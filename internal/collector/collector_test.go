package collector

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/store"
	"github.com/wf-pro-dev/tailkit"
	tailkittypes "github.com/wf-pro-dev/tailkit/types"
	integrationsTypes "github.com/wf-pro-dev/tailkit/types/integrations"
	"tailscale.com/ipn/ipnstate"
)

func TestCollectorRunOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	configStore := db.ProxyConfigs()
	snapshotStore := db.Snapshots()
	runStore := db.Runs()

	if err := configStore.Save(ctx, parser.ProxyConfigInput{
		ID:         "cfg-1",
		NodeName:   "node-a",
		Kind:       "nginx",
		ConfigPath: "/etc/nginx/nginx.conf",
		UpdatedAt:  core.NowTimestamp(),
	}); err != nil {
		t.Fatalf("save proxy config: %v", err)
	}
	if err := configStore.Save(ctx, parser.ProxyConfigInput{
		ID:         "cfg-2",
		NodeName:   "node-a",
		Kind:       "nginx",
		ConfigPath: "/etc/nginx/conf.d/extra.conf",
		UpdatedAt:  core.NowTimestamp(),
	}); err != nil {
		t.Fatalf("save second proxy config: %v", err)
	}

	c := NewCollector(nil, runStore, snapshotStore, configStore, core.NewEventBus(), parser.NewRegistry())
	c.discoverPeers = func(_ context.Context, _ *tailkit.Server) ([]tailkittypes.Peer, error) {
		return []tailkittypes.Peer{
			testPeer("node-a", true, "100.64.0.1"),
			testPeer("node-b", true, "100.64.0.2"),
		}, nil
	}
	c.newNode = func(hostname string) nodeClient {
		switch hostname {
		case "node-a":
			return fakeNodeClient{
				metrics: fakeMetricsClient{
					ports: []tailkittypes.Port{{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 123, Process: "nginx"}},
				},
				docker: fakeDockerClient{
					config: integrationsTypes.DockerConfig{
						Enabled: true,
						Swarm: integrationsTypes.DockerSectionConfig{
							Enabled: true,
							Allow:   []string{"read"},
						},
					},
					containers: []container.Summary{{
						ID:    "container-1",
						Names: []string{"/web"},
						Ports: []container.Port{{PrivatePort: 3000, PublicPort: 8080, Type: "tcp"}},
					}},
					services: []swarm.Service{{
						ID: "service-1",
						Spec: swarm.ServiceSpec{
							Annotations: swarm.Annotations{Name: "swarm-api"},
							EndpointSpec: &swarm.EndpointSpec{
								Ports: []swarm.PortConfig{{
									Protocol:      swarm.PortConfigProtocolTCP,
									TargetPort:    3000,
									PublishedPort: 3000,
									PublishMode:   swarm.PortConfigPublishModeIngress,
								}},
							},
						},
					}},
				},
				files: fakeFilesClient{
					contentByPath: map[string]string{
						"/etc/nginx/nginx.conf": `
include /etc/nginx/conf.d/*.conf;
server {
    listen 80;
    server_name app.example.com;
    location / {
        proxy_pass http://localhost:3000;
    }
}`,
						"/etc/nginx/conf.d/app.conf": `
upstream dashboard_backend {
    server 127.0.0.1:9000;
}

server {
    listen 8080;
    server_name dashboard.example.com;
    location / {
        proxy_pass http://dashboard_backend;
    }
}`,
						"/etc/nginx/conf.d/extra.conf": `
server {
    listen 8443;
    server_name extra.example.com;
    location / {
        proxy_pass http://127.0.0.1:9443;
    }
}`,
					},
					entriesByDir: map[string][]tailkittypes.DirEntry{
						"/etc/nginx/conf.d": {
							{Name: "app.conf"},
							{Name: "extra.conf"},
						},
					},
				},
			}
		case "node-b":
			return fakeNodeClient{
				metrics: fakeMetricsClient{
					err: errors.New("metrics unavailable"),
				},
				docker: fakeDockerClient{},
				files:  fakeFilesClient{},
			}
		default:
			return nil
		}
	}

	run, err := c.RunOnce(ctx)
	if err == nil {
		t.Fatal("RunOnce() error = nil, want partial failure")
	}
	if run.NodeCount != 2 {
		t.Fatalf("RunOnce().NodeCount = %d, want 2", run.NodeCount)
	}
	if run.ErrorCount != 1 {
		t.Fatalf("RunOnce().ErrorCount = %d, want 1", run.ErrorCount)
	}

	snapshot, err := snapshotStore.LatestByNode(ctx, "node-a")
	if err != nil {
		t.Fatalf("LatestByNode(node-a): %v", err)
	}
	if len(snapshot.Ports) != 1 || snapshot.Ports[0].Process != "nginx" {
		t.Fatalf("snapshot.Ports = %#v, want nginx port", snapshot.Ports)
	}
	if len(snapshot.Containers) != 1 || snapshot.Containers[0].ContainerName != "web" {
		t.Fatalf("snapshot.Containers = %#v, want web container", snapshot.Containers)
	}
	if len(snapshot.Containers[0].PublishedPorts) != 1 || snapshot.Containers[0].PublishedPorts[0].HostPort != 8080 {
		t.Fatalf("snapshot.Containers[0].PublishedPorts = %#v, want direct container publish", snapshot.Containers[0].PublishedPorts)
	}
	if len(snapshot.Services) != 1 || snapshot.Services[0].ServiceName != "swarm-api" {
		t.Fatalf("snapshot.Services = %#v, want swarm-api service", snapshot.Services)
	}
	if len(snapshot.Forwards) != 3 {
		t.Fatalf("snapshot.Forwards = %#v, want 3 aggregated forward actions", snapshot.Forwards)
	}

	statuses, err := c.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus(): %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("len(statuses) = %d, want 2", len(statuses))
	}
}

func TestCollectorRunOnceCountsProxyConfigStoreErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	c := NewCollector(nil, db.Runs(), db.Snapshots(), fakeProxyConfigStore{listByNodeErr: errors.New("store timeout")}, core.NewEventBus(), parser.NewRegistry())
	c.discoverPeers = func(_ context.Context, _ *tailkit.Server) ([]tailkittypes.Peer, error) {
		return []tailkittypes.Peer{testPeer("node-a", true, "100.64.0.1")}, nil
	}
	c.newNode = func(string) nodeClient {
		return fakeNodeClient{
			metrics: fakeMetricsClient{
				ports: []tailkittypes.Port{{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 123, Process: "nginx"}},
			},
			docker: fakeDockerClient{},
			files:  fakeFilesClient{},
		}
	}

	run, err := c.RunOnce(ctx)
	if err == nil {
		t.Fatal("RunOnce() error = nil, want partial failure")
	}
	if run.ErrorCount != 1 {
		t.Fatalf("RunOnce().ErrorCount = %d, want 1", run.ErrorCount)
	}

	snapshot, snapErr := db.Snapshots().LatestByNode(ctx, "node-a")
	if snapErr != nil {
		t.Fatalf("LatestByNode(): %v", snapErr)
	}
	if !strings.Contains(snapshot.Error, "proxy config store: store timeout") {
		t.Fatalf("snapshot.Error = %q, want proxy config store error", snapshot.Error)
	}
}

func TestCollectorRunOnceIgnoresSwarmServiceCollectionFailuresForNodeState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	c := NewCollector(nil, db.Runs(), db.Snapshots(), nil, core.NewEventBus(), parser.NewRegistry())
	c.discoverPeers = func(_ context.Context, _ *tailkit.Server) ([]tailkittypes.Peer, error) {
		return []tailkittypes.Peer{testPeer("node-a", true, "100.64.0.1")}, nil
	}
	c.newNode = func(string) nodeClient {
		return fakeNodeClient{
			metrics: fakeMetricsClient{
				ports: []tailkittypes.Port{{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 123, Process: "nginx"}},
			},
			docker: fakeDockerClient{
				config: integrationsTypes.DockerConfig{
					Enabled: true,
					Swarm: integrationsTypes.DockerSectionConfig{
						Enabled: true,
						Allow:   []string{"read"},
					},
				},
				swarmErr: errors.New("swarm unavailable"),
			},
			files: fakeFilesClient{},
		}
	}

	run, err := c.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}
	if run.ErrorCount != 0 {
		t.Fatalf("RunOnce().ErrorCount = %d, want 0", run.ErrorCount)
	}

	statuses, statusErr := c.GetStatus(ctx)
	if statusErr != nil {
		t.Fatalf("GetStatus(): %v", statusErr)
	}
	if len(statuses) != 1 || statuses[0].Degraded {
		t.Fatalf("statuses = %#v, want one non-degraded node", statuses)
	}

	snapshot, snapErr := db.Snapshots().LatestByNode(ctx, "node-a")
	if snapErr != nil {
		t.Fatalf("LatestByNode(): %v", snapErr)
	}
	if snapshot.Error != "" {
		t.Fatalf("snapshot.Error = %q, want empty", snapshot.Error)
	}
}

func TestCollectorRunOnceUsesLocalStoreContextForProxyConfigAndPersistence(t *testing.T) {
	t.Parallel()

	db := openTestStore(t)
	c := NewCollector(nil, db.Runs(), db.Snapshots(), fakeProxyConfigStore{
		listByNodeFunc: func(ctx context.Context, nodeName core.NodeName) ([]parser.ProxyConfigInput, error) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if nodeName != "node-a" {
				t.Fatalf("ListByNode() nodeName = %q, want node-a", nodeName)
			}
			return nil, nil
		},
	}, core.NewEventBus(), parser.NewRegistry())
	c.discoverPeers = func(_ context.Context, _ *tailkit.Server) ([]tailkittypes.Peer, error) {
		return []tailkittypes.Peer{testPeer("node-a", true, "100.64.0.1")}, nil
	}
	c.newNode = func(string) nodeClient {
		return fakeNodeClient{
			metrics: fakeMetricsClient{
				ports: []tailkittypes.Port{{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 123, Process: "nginx"}},
			},
			docker: fakeDockerClient{},
			files:  fakeFilesClient{},
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	run, err := c.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}
	if run.ErrorCount != 0 {
		t.Fatalf("RunOnce().ErrorCount = %d, want 0", run.ErrorCount)
	}

	snapshot, snapErr := db.Snapshots().LatestByNode(context.Background(), "node-a")
	if snapErr != nil {
		t.Fatalf("LatestByNode(): %v", snapErr)
	}
	if len(snapshot.Ports) != 1 || snapshot.Ports[0].Port != 80 {
		t.Fatalf("snapshot.Ports = %#v, want saved nginx port", snapshot.Ports)
	}

	latestRun, runErr := db.Runs().Latest(context.Background())
	if runErr != nil {
		t.Fatalf("Latest() error = %v", runErr)
	}
	if latestRun.ID != run.ID {
		t.Fatalf("Latest().ID = %s, want %s", latestRun.ID, run.ID)
	}
}

func TestCollectorRunOnceAppliesPerOperationTimeoutToDockerReads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	configDeadlineCh := make(chan time.Duration, 1)
	containersDeadlineCh := make(chan time.Duration, 1)
	c := NewCollector(nil, db.Runs(), db.Snapshots(), nil, core.NewEventBus(), parser.NewRegistry())
	c.SetNodeTimeout(25 * time.Millisecond)
	c.discoverPeers = func(_ context.Context, _ *tailkit.Server) ([]tailkittypes.Peer, error) {
		return []tailkittypes.Peer{testPeer("node-a", true, "100.64.0.1")}, nil
	}
	c.newNode = func(string) nodeClient {
		return fakeNodeClient{
			metrics: fakeMetricsClient{
				ports: []tailkittypes.Port{{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 123, Process: "nginx"}},
			},
			docker: fakeDockerClient{
				configFunc: func(ctx context.Context) (integrationsTypes.DockerConfig, error) {
					deadline, ok := ctx.Deadline()
					if !ok {
						t.Fatal("Config() context missing deadline")
					}
					select {
					case configDeadlineCh <- time.Until(deadline):
					default:
					}
					<-ctx.Done()
					return integrationsTypes.DockerConfig{}, ctx.Err()
				},
				containersFunc: func(ctx context.Context) ([]container.Summary, error) {
					deadline, ok := ctx.Deadline()
					if !ok {
						t.Fatal("Containers() context missing deadline")
					}
					select {
					case containersDeadlineCh <- time.Until(deadline):
					default:
					}
					return []container.Summary{{
						ID:    "container-1",
						Names: []string{"/web"},
					}}, nil
				},
			},
			files: fakeFilesClient{},
		}
	}

	parentCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	run, err := c.RunOnce(parentCtx)
	if err == nil {
		t.Fatal("RunOnce() error = nil, want timeout error")
	}
	if run.ErrorCount != 1 {
		t.Fatalf("RunOnce().ErrorCount = %d, want 1", run.ErrorCount)
	}

	select {
	case deadline := <-configDeadlineCh:
		if deadline > 200*time.Millisecond {
			t.Fatalf("Config() deadline = %s, want short per-operation timeout", deadline)
		}
	case <-time.After(time.Second):
		t.Fatal("expected docker config deadline to be observed")
	}

	select {
	case deadline := <-containersDeadlineCh:
		if deadline > 200*time.Millisecond {
			t.Fatalf("Containers() deadline = %s, want short per-operation timeout", deadline)
		}
	case <-time.After(time.Second):
		t.Fatal("expected docker containers deadline to be observed")
	}

	snapshot, snapErr := db.Snapshots().LatestByNode(ctx, "node-a")
	if snapErr != nil {
		t.Fatalf("LatestByNode(): %v", snapErr)
	}
	if len(snapshot.Containers) != 1 || snapshot.Containers[0].ContainerName != "web" {
		t.Fatalf("snapshot.Containers = %#v, want collected containers despite config timeout", snapshot.Containers)
	}
	if !strings.Contains(snapshot.Error, "docker config: context deadline exceeded") {
		t.Fatalf("snapshot.Error = %q, want docker config timeout", snapshot.Error)
	}
}

func TestCollectorRunOnceAppliesPerConfigTimeoutToProxyReads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	configStore := db.ProxyConfigs()
	for _, cfg := range []parser.ProxyConfigInput{
		{
			ID:         "cfg-1",
			NodeName:   "node-a",
			Kind:       "nginx",
			ConfigPath: "/etc/nginx/slow.conf",
			UpdatedAt:  core.NowTimestamp(),
		},
		{
			ID:         "cfg-2",
			NodeName:   "node-a",
			Kind:       "nginx",
			ConfigPath: "/etc/nginx/fast.conf",
			UpdatedAt:  core.NowTimestamp(),
		},
	} {
		if err := configStore.Save(ctx, cfg); err != nil {
			t.Fatalf("save proxy config %s: %v", cfg.ID, err)
		}
	}

	slowDeadlineCh := make(chan time.Duration, 1)
	fastDeadlineCh := make(chan time.Duration, 1)
	c := NewCollector(nil, db.Runs(), db.Snapshots(), configStore, core.NewEventBus(), parser.NewRegistry())
	c.SetNodeTimeout(25 * time.Millisecond)
	c.discoverPeers = func(_ context.Context, _ *tailkit.Server) ([]tailkittypes.Peer, error) {
		return []tailkittypes.Peer{testPeer("node-a", true, "100.64.0.1")}, nil
	}
	c.newNode = func(string) nodeClient {
		return fakeNodeClient{
			metrics: fakeMetricsClient{
				ports: []tailkittypes.Port{{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 123, Process: "nginx"}},
			},
			docker: fakeDockerClient{},
			files: fakeFilesClient{
				readFunc: func(ctx context.Context, path string) (string, error) {
					deadline, ok := ctx.Deadline()
					if !ok {
						t.Fatal("Read() context missing deadline")
					}
					switch path {
					case "/etc/nginx/slow.conf":
						select {
						case slowDeadlineCh <- time.Until(deadline):
						default:
						}
						<-ctx.Done()
						return "", ctx.Err()
					case "/etc/nginx/fast.conf":
						select {
						case fastDeadlineCh <- time.Until(deadline):
						default:
						}
						if err := ctx.Err(); err != nil {
							return "", err
						}
						return `
server {
    listen 8080;
    location / {
        proxy_pass http://127.0.0.1:9443;
    }
}`, nil
					default:
						return "", errors.New("file not found")
					}
				},
			},
		}
	}

	parentCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	run, err := c.RunOnce(parentCtx)
	if err == nil {
		t.Fatal("RunOnce() error = nil, want timeout error")
	}
	if run.ErrorCount != 1 {
		t.Fatalf("RunOnce().ErrorCount = %d, want 1", run.ErrorCount)
	}

	select {
	case deadline := <-slowDeadlineCh:
		if deadline > 200*time.Millisecond {
			t.Fatalf("slow Read() deadline = %s, want short per-config timeout", deadline)
		}
	case <-time.After(time.Second):
		t.Fatal("expected slow proxy config deadline to be observed")
	}

	select {
	case deadline := <-fastDeadlineCh:
		if deadline > 200*time.Millisecond {
			t.Fatalf("fast Read() deadline = %s, want short per-config timeout", deadline)
		}
	case <-time.After(time.Second):
		t.Fatal("expected fast proxy config deadline to be observed")
	}

	snapshot, snapErr := db.Snapshots().LatestByNode(ctx, "node-a")
	if snapErr != nil {
		t.Fatalf("LatestByNode(): %v", snapErr)
	}
	if len(snapshot.Forwards) != 1 {
		t.Fatalf("snapshot.Forwards = %#v, want fast proxy config to still be parsed", snapshot.Forwards)
	}
	if !strings.Contains(snapshot.Error, "proxy config read: context deadline exceeded") {
		t.Fatalf("snapshot.Error = %q, want proxy config timeout", snapshot.Error)
	}
}

func TestCollectorRunOnceEnrichesWorkerSwarmContainersFromManagerServices(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	c := NewCollector(nil, db.Runs(), db.Snapshots(), nil, core.NewEventBus(), parser.NewRegistry())
	c.discoverPeers = func(_ context.Context, _ *tailkit.Server) ([]tailkittypes.Peer, error) {
		return []tailkittypes.Peer{
			testPeer("manager-a", true, "100.64.0.1"),
			testPeer("worker-b", true, "100.64.0.2"),
		}, nil
	}
	c.newNode = func(hostname string) nodeClient {
		switch hostname {
		case "manager-a":
			return fakeNodeClient{
				metrics: fakeMetricsClient{},
				docker: fakeDockerClient{
					config: integrationsTypes.DockerConfig{
						Enabled: true,
						Swarm: integrationsTypes.DockerSectionConfig{
							Enabled: true,
							Allow:   []string{"read"},
						},
					},
					services: []swarm.Service{
						{
							ID: "svc-sse",
							Spec: swarm.ServiceSpec{
								Annotations: swarm.Annotations{Name: "unipilot_sse"},
								EndpointSpec: &swarm.EndpointSpec{
									Ports: []swarm.PortConfig{{
										Protocol:      swarm.PortConfigProtocolTCP,
										TargetPort:    3002,
										PublishedPort: 3002,
										PublishMode:   swarm.PortConfigPublishModeIngress,
									}},
								},
							},
						},
						{
							ID: "svc-redis-insight",
							Spec: swarm.ServiceSpec{
								Annotations: swarm.Annotations{Name: "unipilot_redis-insight"},
								EndpointSpec: &swarm.EndpointSpec{
									Ports: []swarm.PortConfig{{
										Protocol:      swarm.PortConfigProtocolTCP,
										TargetPort:    5540,
										PublishedPort: 5540,
										PublishMode:   swarm.PortConfigPublishModeIngress,
									}},
								},
							},
						},
					},
				},
				files: fakeFilesClient{},
			}
		case "worker-b":
			return fakeNodeClient{
				metrics: fakeMetricsClient{},
				docker: fakeDockerClient{
					containers: []container.Summary{
						{
							ID:     "ctr-sse",
							Names:  []string{"/unipilot_sse.1.xyz"},
							Image:  "example/sse:latest",
							State:  "running",
							Status: "Up 1 hour",
							Labels: map[string]string{"com.docker.swarm.service.name": "unipilot_sse"},
						},
						{
							ID:     "ctr-redis-insight",
							Names:  []string{"/unipilot_redis-insight.1.abc"},
							Image:  "redis/redisinsight:latest",
							State:  "running",
							Status: "Up 1 hour",
							Labels: map[string]string{"com.docker.swarm.service.name": "unipilot_redis-insight"},
						},
					},
				},
				files: fakeFilesClient{},
			}
		default:
			return nil
		}
	}

	run, err := c.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}
	if run.ErrorCount != 0 {
		t.Fatalf("RunOnce().ErrorCount = %d, want 0", run.ErrorCount)
	}

	workerSnapshot, snapErr := db.Snapshots().LatestByNode(ctx, "worker-b")
	if snapErr != nil {
		t.Fatalf("LatestByNode(worker-b): %v", snapErr)
	}
	if len(workerSnapshot.Services) != 0 {
		t.Fatalf("workerSnapshot.Services = %#v, want no local worker services", workerSnapshot.Services)
	}
	if len(workerSnapshot.Containers) != 2 {
		t.Fatalf("workerSnapshot.Containers = %#v, want 2 worker swarm containers", workerSnapshot.Containers)
	}
	for _, container := range workerSnapshot.Containers {
		if len(container.PublishedPorts) != 1 {
			t.Fatalf("container %s PublishedPorts = %#v, want 1 inherited service publish", container.ContainerName, container.PublishedPorts)
		}
		if container.PublishedPorts[0].Source != "service" {
			t.Fatalf("container %s PublishedPorts[0] = %#v, want service-derived publish", container.ContainerName, container.PublishedPorts[0])
		}
	}
}

func TestCollectorWatchNode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	snapshotStore := db.Snapshots()

	initial := store.NodeSnapshot{
		ID:          "snap-1",
		RunID:       "run-1",
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
	if err := snapshotStore.Save(ctx, initial); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	c := NewCollector(nil, nil, snapshotStore, nil, core.NewEventBus(), parser.NewRegistry())
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
		t.Fatalf("WatchNode(): %v", err)
	}

	got, err := snapshotStore.LatestByNode(ctx, "node-a")
	if err != nil {
		t.Fatalf("LatestByNode(): %v", err)
	}
	if len(got.Ports) != 1 || got.Ports[0].Port != 443 {
		t.Fatalf("patched ports = %#v, want only 443", got.Ports)
	}
}

func TestMapContainersDeduplicatesCollapsedBindings(t *testing.T) {
	t.Parallel()

	containers := []container.Summary{
		{
			ID:    "container-1",
			Names: []string{"/devbox-ui"},
			Ports: []container.Port{
				{PrivatePort: 80, PublicPort: 80, Type: "tcp"},
				{PrivatePort: 80, PublicPort: 80, Type: "tcp"},
			},
		},
	}

	got := mapContainers(containers, nil)
	if len(got) != 1 {
		t.Fatalf("len(mapContainers(...)) = %d, want 1; got %#v", len(got), got)
	}
	if got[0].ContainerName != "devbox-ui" || len(got[0].PublishedPorts) != 1 || got[0].PublishedPorts[0].HostPort != 80 || got[0].PublishedPorts[0].TargetPort != 80 {
		t.Fatalf("got[0] = %#v", got[0])
	}
}

func TestMapContainersAttachesServicePublishesToRunningSwarmTasks(t *testing.T) {
	t.Parallel()

	containers := []container.Summary{
		{
			ID:     "container-1",
			Names:  []string{"/unipilot_api.1.x"},
			Image:  "example/api:latest",
			State:  "running",
			Status: "Up 2 hours",
			Labels: map[string]string{"com.docker.swarm.service.name": "unipilot_api"},
		},
	}
	services := []store.SwarmServicePort{{
		ServiceID:   "svc-1",
		ServiceName: "unipilot_api",
		HostPort:    3000,
		TargetPort:  3000,
		Proto:       "tcp",
		Mode:        "ingress",
	}}

	got := mapContainers(containers, services)
	if len(got) != 1 {
		t.Fatalf("len(mapContainers(...)) = %d, want 1", len(got))
	}
	if got[0].ServiceName != "unipilot_api" || len(got[0].PublishedPorts) != 1 {
		t.Fatalf("got[0] = %#v, want running swarm task with one service publish", got[0])
	}
	if got[0].PublishedPorts[0].Source != "service" || got[0].PublishedPorts[0].HostPort != 3000 {
		t.Fatalf("got[0].PublishedPorts[0] = %#v, want service publish on port 3000", got[0].PublishedPorts[0])
	}
}

func TestMapContainersSkipsExitedContainers(t *testing.T) {
	t.Parallel()

	containers := []container.Summary{
		{
			ID:     "container-1",
			Names:  []string{"/running"},
			State:  "running",
			Status: "Up 1 hour",
			Ports:  []container.Port{{PrivatePort: 80, PublicPort: 8080, Type: "tcp"}},
		},
		{
			ID:     "container-2",
			Names:  []string{"/exited"},
			State:  "exited",
			Status: "Exited (0) 10 minutes ago",
			Ports:  []container.Port{{PrivatePort: 81, PublicPort: 8081, Type: "tcp"}},
		},
	}

	got := mapContainers(containers, nil)
	if len(got) != 1 {
		t.Fatalf("len(mapContainers(...)) = %d, want 1", len(got))
	}
	if got[0].ContainerName != "running" {
		t.Fatalf("got[0].ContainerName = %q, want running", got[0].ContainerName)
	}
}

func TestMapSwarmServicesDeduplicatesPublishedPorts(t *testing.T) {
	t.Parallel()

	services := []swarm.Service{
		{
			ID: "service-1",
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "unipilot_api"},
				EndpointSpec: &swarm.EndpointSpec{
					Ports: []swarm.PortConfig{
						{Protocol: swarm.PortConfigProtocolTCP, TargetPort: 3000, PublishedPort: 3000, PublishMode: swarm.PortConfigPublishModeIngress},
						{Protocol: swarm.PortConfigProtocolTCP, TargetPort: 3000, PublishedPort: 3000, PublishMode: swarm.PortConfigPublishModeIngress},
					},
				},
			},
		},
	}

	got := mapSwarmServices(services)
	if len(got) != 1 {
		t.Fatalf("len(mapSwarmServices(...)) = %d, want 1; got %#v", len(got), got)
	}
	if got[0].ServiceName != "unipilot_api" || got[0].HostPort != 3000 || got[0].TargetPort != 3000 {
		t.Fatalf("got[0] = %#v", got[0])
	}
}

func TestCollectorMarksOfflineNodes(t *testing.T) {
	t.Parallel()

	c := NewCollector(nil, nil, nil, nil, core.NewEventBus(), parser.NewRegistry())
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

func TestCollectorGetStatusIncludesDiscoveredNodes(t *testing.T) {
	t.Parallel()

	c := NewCollector(nil, nil, nil, nil, core.NewEventBus(), parser.NewRegistry())
	c.srv = &tailkit.Server{}
	c.discoverPeers = func(context.Context, *tailkit.Server) ([]tailkittypes.Peer, error) {
		return []tailkittypes.Peer{
			testPeer("node-a", true, "100.64.0.1"),
			testPeer("node-b", true, "100.64.0.2"),
		}, nil
	}
	c.statuses["node-b"] = NodeStatus{
		NodeName:   "node-b",
		Online:     false,
		Degraded:   true,
		LastError:  "previous error",
		LastSeenAt: core.NowTimestamp(),
	}

	statuses, err := c.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus(): %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("len(statuses) = %d, want 2", len(statuses))
	}
	if statuses[0].NodeName != "node-a" || !statuses[0].Online {
		t.Fatalf("statuses[0] = %#v, want discovered online node-a", statuses[0])
	}
	if statuses[1].NodeName != "node-b" || !statuses[1].Online || !statuses[1].Degraded {
		t.Fatalf("statuses[1] = %#v, want merged discovered node-b with degraded state", statuses[1])
	}
}

type fakeNodeClient struct {
	metrics fakeMetricsClient
	docker  fakeDockerClient
	files   fakeFilesClient
}

type fakeProxyConfigStore struct {
	listByNodeErr  error
	listByNodeFunc func(context.Context, core.NodeName) ([]parser.ProxyConfigInput, error)
}

func (f fakeProxyConfigStore) Get(context.Context, core.ID) (parser.ProxyConfigInput, error) {
	return parser.ProxyConfigInput{}, errors.New("not implemented")
}

func (f fakeProxyConfigStore) List(context.Context, core.Filter) ([]parser.ProxyConfigInput, error) {
	return nil, errors.New("not implemented")
}

func (f fakeProxyConfigStore) Save(context.Context, parser.ProxyConfigInput) error {
	return errors.New("not implemented")
}

func (f fakeProxyConfigStore) Delete(context.Context, core.ID) error {
	return errors.New("not implemented")
}

func (f fakeProxyConfigStore) GetByNodeAndPath(context.Context, core.NodeName, string) (parser.ProxyConfigInput, error) {
	return parser.ProxyConfigInput{}, errors.New("not implemented")
}

func (f fakeProxyConfigStore) ListByNode(ctx context.Context, nodeName core.NodeName) ([]parser.ProxyConfigInput, error) {
	if f.listByNodeFunc != nil {
		return f.listByNodeFunc(ctx, nodeName)
	}
	return nil, f.listByNodeErr
}

func (f fakeProxyConfigStore) ListAll(context.Context) ([]parser.ProxyConfigInput, error) {
	return nil, errors.New("not implemented")
}

func (f fakeNodeClient) Metrics() metricsClient { return f.metrics }
func (f fakeNodeClient) Docker() dockerClient   { return f.docker }
func (f fakeNodeClient) Files() filesClient     { return f.files }

type fakeMetricsClient struct {
	ports  []tailkittypes.Port
	err    error
	stream []tailkittypes.Event[tailkittypes.PortUpdate]
}

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

type fakeDockerClient struct {
	config         integrationsTypes.DockerConfig
	containers     []container.Summary
	services       []swarm.Service
	err            error
	configErr      error
	swarmErr       error
	dockerErr      error
	configFunc     func(context.Context) (integrationsTypes.DockerConfig, error)
	containersFunc func(context.Context) ([]container.Summary, error)
	servicesFunc   func(context.Context) ([]swarm.Service, error)
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

func (f fakeDockerClient) SwarmServices(ctx context.Context) ([]swarm.Service, error) {
	if f.servicesFunc != nil {
		return f.servicesFunc(ctx)
	}
	if f.swarmErr != nil {
		return f.services, f.swarmErr
	}
	return f.services, f.err
}

type fakeFilesClient struct {
	contentByPath map[string]string
	entriesByDir  map[string][]tailkittypes.DirEntry
	err           error
	readFunc      func(context.Context, string) (string, error)
	listFunc      func(context.Context, string) ([]tailkittypes.DirEntry, error)
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
