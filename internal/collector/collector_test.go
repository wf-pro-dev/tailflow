package collector

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/swarm"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/store"
	"github.com/wf-pro-dev/tailkit"
	tailkittypes "github.com/wf-pro-dev/tailkit/types"
	integrationsTypes "github.com/wf-pro-dev/tailkit/types/integrations"
	"tailscale.com/ipn/ipnstate"
)

func TestCollectorBootstrap(t *testing.T) {
	t.Parallel()

	c := NewCollector(nil, nil, core.NewEventBus(), parser.NewRegistry())
	c.discoverPeers = func(context.Context, *tailkit.Server) ([]tailkittypes.Peer, error) {
		return []tailkittypes.Peer{
			testPeer("node-a", true, "100.64.0.1"),
			testPeer("node-b", true, "100.64.0.2"),
		}, nil
	}
	c.newNode = func(host string) nodeClient {
		switch host {
		case "node-a":
			return fakeNodeClient{
				metrics: fakeMetricsClient{ports: []tailkittypes.Port{{Port: 80, Process: "nginx"}}},
			}
		case "node-b":
			return fakeNodeClient{
				metrics: fakeMetricsClient{err: errors.New("metrics unavailable")},
			}
		default:
			return nil
		}
	}

	if err := c.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	snapshots := c.Snapshots()
	if len(snapshots) != 2 {
		t.Fatalf("len(Snapshots()) = %d, want 2", len(snapshots))
	}
	statuses, err := c.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("len(statuses) = %d, want 2", len(statuses))
	}
	if !statuses[0].Online || !statuses[1].Online {
		t.Fatalf("statuses = %#v, want both nodes online", statuses)
	}
	if statuses[1].LastError == "" {
		t.Fatalf("statuses[1] = %#v, want partial collection error recorded", statuses[1])
	}
}

func TestCollectorWatchNode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	initial := store.NodeSnapshot{
		ID:          "snap-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NewTimestamp(time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)),
		Ports: []store.ListenPort{{
			Addr:    "0.0.0.0",
			Port:    80,
			Proto:   "tcp",
			PID:     123,
			Process: "nginx",
		}},
	}
	if err := db.Snapshots().Save(ctx, initial); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	c := NewCollector(nil, nil, core.NewEventBus(), parser.NewRegistry())
	c.setSnapshot(initial)
	c.newNode = func(string) nodeClient {
		return fakeNodeClient{
			metrics: fakeMetricsClient{
				stream: []tailkittypes.Event[tailkittypes.PortUpdate]{
					{
						Name: tailkittypes.EventPortBound,
						Data: tailkittypes.PortUpdate{
							Kind: "bound",
							Port: tailkittypes.Port{Addr: "0.0.0.0", Port: 443, Proto: "tcp", PID: 123, Process: "nginx"},
						},
					},
					{
						Name: tailkittypes.EventPortReleased,
						Data: tailkittypes.PortUpdate{
							Kind: "released",
							Port: tailkittypes.Port{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 123, Process: "nginx"},
						},
					},
				},
			},
		}
	}

	if err := c.WatchNode(ctx, "node-a"); err != nil {
		t.Fatalf("WatchNode() error = %v", err)
	}

	got, ok := c.LatestSnapshot("node-a")
	if !ok {
		t.Fatal("LatestSnapshot() missing node-a")
	}
	if len(got.Ports) != 1 || got.Ports[0].Port != 443 {
		t.Fatalf("patched ports = %#v, want only 443", got.Ports)
	}
}

func TestCollectorWatchNodeDoesNotEmitSnapshotUpdatedForSteadyStateWatcherChanges(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := core.NewEventBus()
	eventsCh := bus.Subscribe(ctx, core.TopicSnapshot)

	c := NewCollector(nil, nil, bus, parser.NewRegistry())
	c.setSnapshot(store.NodeSnapshot{
		ID:          "snap-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Addr: "0.0.0.0", Port: 80, Proto: "tcp"}},
	})
	c.newNode = func(string) nodeClient {
		return fakeNodeClient{
			metrics: fakeMetricsClient{
				stream: []tailkittypes.Event[tailkittypes.PortUpdate]{
					{
						Name: tailkittypes.EventPortBound,
						Data: tailkittypes.PortUpdate{
							Kind: "bound",
							Port: tailkittypes.Port{Addr: "0.0.0.0", Port: 443, Proto: "tcp"},
						},
					},
				},
			},
		}
	}

	if err := c.WatchNode(context.Background(), "node-a"); err != nil {
		t.Fatalf("WatchNode() error = %v", err)
	}

	select {
	case event := <-eventsCh:
		t.Fatalf("unexpected snapshot event from watcher path: %#v", event)
	default:
	}
}

func TestCollectorWatchNodeRecomputesForwardsFromActiveConfigs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	config := parser.ProxyConfigInput{
		ID:         "cfg-1",
		NodeName:   "node-a",
		Kind:       "nginx",
		ConfigPath: "/etc/nginx/nginx.conf",
		Content:    "server {}",
		UpdatedAt:  core.NowTimestamp(),
	}
	if err := db.ProxyConfigs().Save(ctx, config); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	initial := store.NodeSnapshot{
		ID:          "snap-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NewTimestamp(time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)),
		Ports: []store.ListenPort{{
			Addr:    "0.0.0.0",
			Port:    80,
			Proto:   "tcp",
			PID:     123,
			Process: "nginx",
		}},
		Forwards: []parser.ForwardAction{{
			Listener:  parser.Listener{Port: 80},
			Target:    parser.NormalizeTarget("stale-node:9999", 0),
			Hostnames: []string{"stale.example.com"},
		}},
	}

	c := NewCollector(nil, db.ProxyConfigs(), core.NewEventBus(), parser.NewRegistry())
	c.setSnapshot(initial)
	c.newNode = func(string) nodeClient {
		return fakeNodeClient{
			metrics: fakeMetricsClient{
				stream: []tailkittypes.Event[tailkittypes.PortUpdate]{
					{
						Name: "snapshot",
						Data: tailkittypes.PortUpdate{
							Kind:  "snapshot",
							Ports: []tailkittypes.Port{{Addr: "0.0.0.0", Port: 443, Proto: "tcp", PID: 123, Process: "nginx"}},
						},
					},
				},
			},
			files: fakeFilesClient{
				contentByPath: map[string]string{
					"/etc/nginx/nginx.conf": `
server {
    listen 443;
    server_name live.example.com;
    location / {
        proxy_pass http://127.0.0.1:3000;
    }
}`,
				},
			},
		}
	}

	if err := c.WatchNode(ctx, "node-a"); err != nil {
		t.Fatalf("WatchNode() error = %v", err)
	}

	got, ok := c.LatestSnapshot("node-a")
	if !ok {
		t.Fatal("LatestSnapshot() missing node-a")
	}
	if len(got.Forwards) != 1 {
		t.Fatalf("forwards = %#v, want one active forward", got.Forwards)
	}
	if got.Forwards[0].Target.Raw != "http://127.0.0.1:3000" {
		t.Fatalf("forward target = %#v, want active config target", got.Forwards[0].Target)
	}
	if len(got.Forwards[0].Hostnames) != 1 || got.Forwards[0].Hostnames[0] != "live.example.com" {
		t.Fatalf("forward hostnames = %#v, want active config hostname", got.Forwards[0].Hostnames)
	}
}

func TestCollectorWatchNodeRefreshesDockerInventoryFromContainerStream(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	initial := store.NodeSnapshot{
		ID:          "snap-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NewTimestamp(time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)),
		Containers: []store.Container{{
			ContainerID:   "container-1",
			ContainerName: "api-old",
			Image:         "example/api:v1",
			State:         "running",
			Status:        "Up 1 hour",
		}},
	}

	c := NewCollector(nil, nil, core.NewEventBus(), parser.NewRegistry())
	c.setSnapshot(initial)

	var refreshCalls int32
	c.newNode = func(string) nodeClient {
		return fakeNodeClient{
			metrics: fakeMetricsClient{},
			docker: fakeDockerClient{
				config: integrationsTypes.DockerConfig{
					Enabled: true,
					Swarm:   integrationsTypes.DockerSectionConfig{Enabled: true, Allow: []string{"read"}},
				},
				containerStream: []tailkittypes.Event[tailkittypes.DockerEvent]{
					{
						Name: tailkittypes.EventDockerContainer,
						Data: tailkittypes.DockerEvent{
							Type:   events.ContainerEventType,
							Action: events.ActionStart,
							Actor:  events.Actor{ID: "container-2"},
						},
					},
				},
				containersFunc: func(context.Context) ([]container.Summary, error) {
					atomic.AddInt32(&refreshCalls, 1)
					return []container.Summary{{
						ID:     "container-2",
						Names:  []string{"/api"},
						Image:  "example/api:v2",
						State:  "running",
						Status: "Up 5 seconds",
						Ports: []container.Port{
							{PrivatePort: 3000, PublicPort: 3000, Type: "tcp"},
						},
					}}, nil
				},
				servicesFunc: func(context.Context) ([]swarm.Service, error) {
					return []swarm.Service{{
						ID: "service-1",
						Spec: swarm.ServiceSpec{
							Annotations: swarm.Annotations{Name: "api"},
							EndpointSpec: &swarm.EndpointSpec{
								Ports: []swarm.PortConfig{
									{
										PublishedPort: 3000,
										TargetPort:    3000,
										Protocol:      swarm.PortConfigProtocolTCP,
										PublishMode:   swarm.PortConfigPublishModeIngress,
									},
								},
							},
						},
					}}, nil
				},
			},
		}
	}

	if err := c.WatchNode(ctx, "node-a"); err != nil {
		t.Fatalf("WatchNode() error = %v", err)
	}

	if got := atomic.LoadInt32(&refreshCalls); got != 1 {
		t.Fatalf("docker refresh calls = %d, want 1", got)
	}

	got, ok := c.LatestSnapshot("node-a")
	if !ok {
		t.Fatal("LatestSnapshot() missing node-a")
	}
	if len(got.Containers) != 1 || got.Containers[0].ContainerName != "api" {
		t.Fatalf("containers = %#v, want refreshed docker inventory", got.Containers)
	}
	if len(got.Services) != 1 || got.Services[0].ServiceName != "api" {
		t.Fatalf("services = %#v, want refreshed swarm inventory", got.Services)
	}
}

func TestPublishStatusEventSuppressesTimestampOnlyChanges(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := core.NewEventBus()
	eventsCh := bus.Subscribe(ctx, core.TopicNode)
	c := NewCollector(nil, nil, bus, parser.NewRegistry())

	previous := NodeStatus{
		NodeName:   "node-a",
		Online:     true,
		Degraded:   false,
		LastSeenAt: core.NewTimestamp(time.Date(2026, 5, 5, 1, 0, 0, 0, time.UTC)),
	}
	current := previous
	current.LastSeenAt = core.NewTimestamp(time.Date(2026, 5, 5, 1, 0, 5, 0, time.UTC))

	c.publishStatusEvent(previous, current)

	select {
	case event := <-eventsCh:
		t.Fatalf("unexpected node status event for timestamp-only change: %#v", event)
	default:
	}
}

func TestCollectorWatchNodeRefreshesDockerInventoryFromContainerStreamWithoutDockerType(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := NewCollector(nil, nil, core.NewEventBus(), parser.NewRegistry())
	c.setSnapshot(store.NodeSnapshot{
		ID:          "snap-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Containers: []store.Container{{
			ContainerID:   "container-1",
			ContainerName: "api-old",
			Image:         "example/api:v1",
			State:         "running",
			Status:        "Up 1 hour",
		}},
	})

	var refreshCalls int32
	c.newNode = func(string) nodeClient {
		return fakeNodeClient{
			metrics: fakeMetricsClient{},
			docker: fakeDockerClient{
				config: integrationsTypes.DockerConfig{Enabled: true},
				containerStream: []tailkittypes.Event[tailkittypes.DockerEvent]{
					{
						Name: tailkittypes.EventDockerContainer,
						Data: tailkittypes.DockerEvent{
							Action: events.ActionStart,
							Actor:  events.Actor{ID: "container-2"},
						},
					},
				},
				containersFunc: func(context.Context) ([]container.Summary, error) {
					atomic.AddInt32(&refreshCalls, 1)
					return []container.Summary{{
						ID:     "container-2",
						Names:  []string{"/api"},
						Image:  "example/api:v2",
						State:  "running",
						Status: "Up 5 seconds",
					}}, nil
				},
			},
		}
	}

	if err := c.WatchNode(ctx, "node-a"); err != nil {
		t.Fatalf("WatchNode() error = %v", err)
	}

	if got := atomic.LoadInt32(&refreshCalls); got != 1 {
		t.Fatalf("docker refresh calls = %d, want 1", got)
	}

	got, ok := c.LatestSnapshot("node-a")
	if !ok {
		t.Fatal("LatestSnapshot() missing node-a")
	}
	if len(got.Containers) != 1 || got.Containers[0].ContainerName != "api" {
		t.Fatalf("containers = %#v, want refreshed docker inventory", got.Containers)
	}
}

func TestCollectorWatchNodeRefreshesContainersWhenDockerSwarmServicesFail(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := NewCollector(nil, nil, core.NewEventBus(), parser.NewRegistry())
	c.setSnapshot(store.NodeSnapshot{
		ID:          "snap-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Containers: []store.Container{{
			ContainerID:   "container-1",
			ContainerName: "api-old",
			Image:         "example/api:v1",
			State:         "running",
			Status:        "Up 1 hour",
		}},
	})

	var refreshCalls int32
	c.newNode = func(string) nodeClient {
		return fakeNodeClient{
			metrics: fakeMetricsClient{},
			docker: fakeDockerClient{
				config: integrationsTypes.DockerConfig{
					Enabled: true,
					Swarm:   integrationsTypes.DockerSectionConfig{Enabled: true, Allow: []string{"read"}},
				},
				containerStream: []tailkittypes.Event[tailkittypes.DockerEvent]{
					{
						Name: tailkittypes.EventDockerContainer,
						Data: tailkittypes.DockerEvent{
							Action: events.ActionStart,
							Actor:  events.Actor{ID: "container-2"},
						},
					},
				},
				servicesFunc: func(context.Context) ([]swarm.Service, error) {
					return nil, errors.New("tailkit: HTTP 500 from /integrations/docker/swarm/services: failed to list swarm services")
				},
				containersFunc: func(context.Context) ([]container.Summary, error) {
					atomic.AddInt32(&refreshCalls, 1)
					return []container.Summary{{
						ID:     "container-2",
						Names:  []string{"/api"},
						Image:  "example/api:v2",
						State:  "running",
						Status: "Up 5 seconds",
					}}, nil
				},
			},
		}
	}

	if err := c.WatchNode(ctx, "node-a"); err != nil {
		t.Fatalf("WatchNode() error = %v", err)
	}

	if got := atomic.LoadInt32(&refreshCalls); got != 1 {
		t.Fatalf("docker refresh calls = %d, want 1", got)
	}

	got, ok := c.LatestSnapshot("node-a")
	if !ok {
		t.Fatal("LatestSnapshot() missing node-a")
	}
	if len(got.Containers) != 1 || got.Containers[0].ContainerName != "api" {
		t.Fatalf("containers = %#v, want refreshed docker inventory", got.Containers)
	}
}

func TestCollectorWatchNodeIgnoresDockerActionsThatDoNotAffectTopology(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := NewCollector(nil, nil, core.NewEventBus(), parser.NewRegistry())
	c.setSnapshot(store.NodeSnapshot{
		ID:          "snap-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
	})

	var refreshCalls int32
	c.newNode = func(string) nodeClient {
		return fakeNodeClient{
			metrics: fakeMetricsClient{},
			docker: fakeDockerClient{
				config: integrationsTypes.DockerConfig{Enabled: true},
				containerStream: []tailkittypes.Event[tailkittypes.DockerEvent]{
					{
						Name: tailkittypes.EventDockerContainer,
						Data: tailkittypes.DockerEvent{
							Type:   events.ContainerEventType,
							Action: events.ActionAttach,
							Actor:  events.Actor{ID: "container-1"},
						},
					},
				},
				containersFunc: func(context.Context) ([]container.Summary, error) {
					atomic.AddInt32(&refreshCalls, 1)
					return nil, nil
				},
			},
		}
	}

	if err := c.WatchNode(ctx, "node-a"); err != nil {
		t.Fatalf("WatchNode() error = %v", err)
	}

	if got := atomic.LoadInt32(&refreshCalls); got != 0 {
		t.Fatalf("docker refresh calls = %d, want 0", got)
	}
}

func TestCollectorPreviewProxyConfig(t *testing.T) {
	t.Parallel()

	c := NewCollector(nil, nil, core.NewEventBus(), parser.NewRegistry())
	c.newNode = func(string) nodeClient {
		return fakeNodeClient{
			files: fakeFilesClient{
				contentByPath: map[string]string{
					"/etc/nginx/nginx.conf": `
server {
    listen 80;
    location / {
        proxy_pass http://127.0.0.1:3000;
    }
}`,
				},
			},
		}
	}

	content, bundle, parsed, err := c.PreviewProxyConfig(context.Background(), "node-a", "nginx", "/etc/nginx/nginx.conf")
	if err != nil {
		t.Fatalf("PreviewProxyConfig() error = %v", err)
	}
	if content == "" || len(bundle) != 1 || len(parsed.Forwards) != 1 {
		t.Fatalf("preview = (%q, %#v, %#v)", content, bundle, parsed)
	}
}

func TestMapContainersDeduplicatesCollapsedBindings(t *testing.T) {
	t.Parallel()

	containers := []container.Summary{{
		ID:    "container-1",
		Names: []string{"/devbox-ui"},
		Ports: []container.Port{
			{PrivatePort: 80, PublicPort: 80, Type: "tcp"},
			{PrivatePort: 80, PublicPort: 80, Type: "tcp"},
		},
	}}

	got := mapContainers(containers, nil)
	if len(got) != 1 || len(got[0].PublishedPorts) != 1 {
		t.Fatalf("mapContainers(...) = %#v", got)
	}
}

func TestMapContainersAttachesServicePublishesToRunningSwarmTasks(t *testing.T) {
	t.Parallel()

	containers := []container.Summary{{
		ID:     "container-1",
		Names:  []string{"/unipilot_api.1.x"},
		Image:  "example/api:latest",
		State:  "running",
		Status: "Up 2 hours",
		Labels: map[string]string{"com.docker.swarm.service.name": "unipilot_api"},
	}}
	services := []store.SwarmServicePort{{
		ServiceID:   "svc-1",
		ServiceName: "unipilot_api",
		HostPort:    3000,
		TargetPort:  3000,
		Proto:       "tcp",
		Mode:        "ingress",
	}}

	got := mapContainers(containers, services)
	if len(got) != 1 || len(got[0].PublishedPorts) != 1 || got[0].PublishedPorts[0].Source != "service" {
		t.Fatalf("mapContainers(...) = %#v", got)
	}
}

func TestMapSwarmServicesDeduplicatesPublishedPorts(t *testing.T) {
	t.Parallel()

	services := []swarm.Service{{
		ID: "service-1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "unipilot_api"},
			EndpointSpec: &swarm.EndpointSpec{
				Ports: []swarm.PortConfig{
					{PublishedPort: 3000, TargetPort: 3000, Protocol: swarm.PortConfigProtocolTCP, PublishMode: swarm.PortConfigPublishModeIngress},
					{PublishedPort: 3000, TargetPort: 3000, Protocol: swarm.PortConfigProtocolTCP, PublishMode: swarm.PortConfigPublishModeIngress},
				},
			},
		},
	}}

	got := mapSwarmServices(services)
	if len(got) != 1 {
		t.Fatalf("mapSwarmServices(...) = %#v", got)
	}
}

func TestCollectorMarksOfflineNodes(t *testing.T) {
	t.Parallel()

	c := NewCollector(nil, nil, core.NewEventBus(), parser.NewRegistry())
	c.statuses["node-a"] = NodeStatus{
		NodeName:   "node-a",
		Online:     true,
		LastSeenAt: core.NowTimestamp(),
	}

	c.markOfflineNodes(map[core.NodeName]struct{}{})

	statuses, err := c.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus(): %v", err)
	}
	if len(statuses) != 1 || statuses[0].Online {
		t.Fatalf("statuses = %#v, want node-a offline", statuses)
	}
}

func TestCollectorRefreshPeersIncludesDiscoveredNodes(t *testing.T) {
	t.Parallel()

	c := NewCollector(nil, nil, core.NewEventBus(), parser.NewRegistry())
	c.discoverPeers = func(context.Context, *tailkit.Server) ([]tailkittypes.Peer, error) {
		return []tailkittypes.Peer{
			testPeer("node-a", true, "100.64.0.1"),
			testPeer("node-b", true, "100.64.0.2"),
		}, nil
	}
	c.newNode = func(host string) nodeClient {
		return fakeNodeClient{
			metrics: fakeMetricsClient{
				ports: []tailkittypes.Port{{Port: 80, Process: host}},
			},
		}
	}
	c.statuses["node-b"] = NodeStatus{
		NodeName:   "node-b",
		Online:     false,
		Degraded:   true,
		LastError:  "previous error",
		LastSeenAt: core.NowTimestamp(),
	}

	online, err := c.RefreshPeers(context.Background())
	if err != nil {
		t.Fatalf("RefreshPeers(): %v", err)
	}
	if len(online) != 2 {
		t.Fatalf("len(online) = %d, want 2", len(online))
	}

	statuses, err := c.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus(): %v", err)
	}
	if len(statuses) != 2 || !statuses[0].Online || !statuses[1].Online || statuses[1].Degraded {
		t.Fatalf("statuses = %#v", statuses)
	}
	if statuses[1].LastError != "" {
		t.Fatalf("statuses[1].LastError = %q, want cleared", statuses[1].LastError)
	}
}

func TestCollectorRefreshPeersImmediatelyDegradesWhenTailkitUnavailable(t *testing.T) {
	t.Parallel()

	c := NewCollector(nil, nil, core.NewEventBus(), parser.NewRegistry())
	c.discoverPeers = func(context.Context, *tailkit.Server) ([]tailkittypes.Peer, error) {
		return []tailkittypes.Peer{
			testPeer("node-a", true, "100.64.0.1"),
		}, nil
	}
	c.newNode = func(string) nodeClient { return nil }

	if _, err := c.RefreshPeers(context.Background()); err != nil {
		t.Fatalf("RefreshPeers(): %v", err)
	}

	statuses, err := c.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus(): %v", err)
	}
	if len(statuses) != 1 || !statuses[0].Degraded {
		t.Fatalf("statuses = %#v, want node immediately degraded", statuses)
	}
}

type fakeNodeClient struct {
	metrics fakeMetricsClient
	docker  fakeDockerClient
	files   fakeFilesClient
}

type fakeMetricsClient struct {
	ports  []tailkittypes.Port
	err    error
	stream []tailkittypes.Event[tailkittypes.PortUpdate]
}

type fakeDockerClient struct {
	config          integrationsTypes.DockerConfig
	containers      []container.Summary
	containerStream []tailkittypes.Event[tailkittypes.DockerEvent]
	services        []swarm.Service
	err             error
	configErr       error
	swarmErr        error
	dockerErr       error
	configFunc      func(context.Context) (integrationsTypes.DockerConfig, error)
	containersFunc  func(context.Context) ([]container.Summary, error)
	servicesFunc    func(context.Context) ([]swarm.Service, error)
}

type fakeFilesClient struct {
	contentByPath map[string]string
	entriesByDir  map[string][]tailkittypes.DirEntry
	err           error
	readFunc      func(context.Context, string) (string, error)
	listFunc      func(context.Context, string) ([]tailkittypes.DirEntry, error)
}

func (f fakeNodeClient) Metrics() metricsClient { return f.metrics }
func (f fakeNodeClient) Docker() dockerClient   { return f.docker }
func (f fakeNodeClient) Files() filesClient     { return f.files }

func (f fakeMetricsClient) Ports(context.Context) ([]tailkittypes.Port, error) {
	return f.ports, f.err
}

func (f fakeMetricsClient) StreamPorts(ctx context.Context, fn func(tailkittypes.Event[tailkittypes.PortUpdate]) error) error {
	for _, event := range f.stream {
		if err := fn(event); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (f fakeDockerClient) Config(ctx context.Context) (integrationsTypes.DockerConfig, error) {
	if f.configFunc != nil {
		return f.configFunc(ctx)
	}
	if !f.config.Enabled && len(f.config.Containers.Allow) == 0 && len(f.config.Swarm.Allow) == 0 && len(f.config.Compose.Allow) == 0 && len(f.config.Images.Allow) == 0 {
		return integrationsTypes.DockerConfig{Enabled: true}, nil
	}
	if f.configErr != nil {
		return f.config, f.configErr
	}
	return f.config, f.err
}

func (f fakeDockerClient) Containers(ctx context.Context) ([]container.Summary, error) {
	if f.containersFunc != nil {
		return f.containersFunc(ctx)
	}
	if f.dockerErr != nil {
		return f.containers, f.dockerErr
	}
	return f.containers, f.err
}

func (f fakeDockerClient) StreamContainers(ctx context.Context, fn func(tailkittypes.Event[tailkittypes.DockerEvent]) error) error {
	for _, event := range f.containerStream {
		if err := fn(event); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (f fakeDockerClient) SwarmServices(ctx context.Context) ([]swarm.Service, error) {
	if f.servicesFunc != nil {
		return f.servicesFunc(ctx)
	}
	if f.swarmErr != nil {
		return f.services, f.swarmErr
	}
	return f.services, f.err
}

func (f fakeFilesClient) Read(ctx context.Context, path string) (string, error) {
	if f.readFunc != nil {
		return f.readFunc(ctx, path)
	}
	if f.err != nil {
		return "", f.err
	}
	content, ok := f.contentByPath[path]
	if !ok {
		return "", errors.New("file not found")
	}
	return content, nil
}

func (f fakeFilesClient) List(ctx context.Context, path string) ([]tailkittypes.DirEntry, error) {
	if f.listFunc != nil {
		return f.listFunc(ctx, path)
	}
	if f.err != nil {
		return nil, f.err
	}
	entries, ok := f.entriesByDir[path]
	if !ok {
		return nil, errors.New("directory not found")
	}
	return entries, nil
}

func testPeer(host string, online bool, ip string) tailkittypes.Peer {
	return tailkittypes.Peer{
		Status: ipnstate.PeerStatus{
			HostName:     host,
			Online:       online,
			TailscaleIPs: []netip.Addr{netip.MustParseAddr(ip)},
		},
	}
}

func openTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()

	path := filepath.Join(t.TempDir(), "tailflow-collector.sqlite")
	db, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite(): %v", err)
	}
	return db
}
