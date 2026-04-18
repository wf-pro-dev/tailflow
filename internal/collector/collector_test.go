package collector

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/store"
	"github.com/wf-pro-dev/tailkit"
	tailkittypes "github.com/wf-pro-dev/tailkit/types"
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
					containers: []container.Summary{{
						ID:    "container-1",
						Names: []string{"/web"},
						Ports: []container.Port{{PrivatePort: 3000, PublicPort: 8080, Type: "tcp"}},
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

	got := mapContainers(containers)
	if len(got) != 1 {
		t.Fatalf("len(mapContainers(...)) = %d, want 1; got %#v", len(got), got)
	}
	if got[0].ContainerName != "devbox-ui" || got[0].HostPort != 80 || got[0].ContainerPort != 80 {
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
	containers []container.Summary
	err        error
}

func (f fakeDockerClient) Containers(context.Context) ([]container.Summary, error) {
	return f.containers, f.err
}

type fakeFilesClient struct {
	contentByPath map[string]string
	entriesByDir  map[string][]tailkittypes.DirEntry
	err           error
}

func (f fakeFilesClient) Read(_ context.Context, path string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	content, ok := f.contentByPath[path]
	if !ok {
		return "", errors.New("file not found")
	}
	return content, nil
}

func (f fakeFilesClient) List(_ context.Context, path string) ([]tailkittypes.DirEntry, error) {
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
