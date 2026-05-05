package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wf-pro-dev/tailflow/internal/collector"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/store"
	"github.com/wf-pro-dev/tailflow/internal/topology"
)

func TestShouldMarkWatcherFailureDegraded(t *testing.T) {
	t.Parallel()

	if shouldMarkWatcherFailureDegraded(context.DeadlineExceeded) {
		t.Fatal("deadline exceeded should not degrade watcher health")
	}
	if !shouldMarkWatcherFailureDegraded(errors.New("tailkit node client unavailable: node-a")) {
		t.Fatal("non-timeout watcher failures should still degrade")
	}
}

func TestRefreshNowStartsWatchersForEligibleNodesOnly(t *testing.T) {
	t.Parallel()

	fake := &fakeCollector{
		statuses: []collector.NodeStatus{
			{NodeName: "node-a", Online: true},
			{NodeName: "node-b", Online: true, LastError: "tailkit node client unavailable: node-b"},
		},
	}
	manager := New(Config{}, fake, topology.NewManager(), core.NewEventBus())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := manager.RefreshNow(ctx); err != nil {
		t.Fatalf("RefreshNow(): %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if fake.watchCalls["node-a"] == 0 {
		t.Fatal("expected watcher for eligible node-a")
	}
	if fake.watchCalls["node-b"] != 0 {
		t.Fatal("did not expect watcher for no-tailkit node-b")
	}
}

func TestResetGlobalStateBuildsLiveNodes(t *testing.T) {
	t.Parallel()

	manager := New(Config{}, &fakeCollector{}, topology.NewManager(), core.NewEventBus())
	manager.inventory.Reset([]store.NodeSnapshot{{
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		DNSName:     "node-a.tailnet",
		Ports:       []store.ListenPort{{Port: 80, Process: "nginx", Proto: "tcp"}},
		Containers:  []store.Container{{ContainerID: "abc123", ContainerName: "api"}},
		Services:    []store.SwarmServicePort{{ServiceID: "svc-1", ServiceName: "api", HostPort: 8080, TargetPort: 80, Proto: "tcp"}},
		Forwards: []parser.ForwardAction{{
			Listener: parser.Listener{Port: 80},
			Target:   parser.ForwardTarget{Raw: "http://100.64.0.2:8080", Kind: parser.TargetKindAddress, Host: "100.64.0.2", Port: 8080},
		}},
	}}, []collector.NodeStatus{{NodeName: "node-a", Online: true}})

	manager.resetGlobalState(topology.Snapshot{
		Version:  42,
		Nodes:    []topology.Node{{Name: "node-a"}},
		Services: []store.Service{{ID: "svc-1"}},
		Routes:   []store.Route{{ID: "route-1"}},
	})

	nodes := manager.state.Snapshot()
	if len(nodes) != 1 {
		t.Fatalf("len(nodes) = %d, want 1", len(nodes))
	}
	if nodes[0].NodeName != "node-a" {
		t.Fatalf("nodes[0].NodeName = %q, want node-a", nodes[0].NodeName)
	}
	if len(nodes[0].Ports) != 1 || len(nodes[0].Containers) != 1 || len(nodes[0].Services) != 1 || len(nodes[0].Forwards) != 1 {
		t.Fatalf("live node not fully hydrated = %#v", nodes[0])
	}
}

type fakeCollector struct {
	statuses   []collector.NodeStatus
	snapshots  []store.NodeSnapshot
	watchCalls map[core.NodeName]int
}

func (f *fakeCollector) Bootstrap(context.Context) error { return nil }

func (f *fakeCollector) RefreshPeers(context.Context) ([]core.NodeName, error) {
	names := make([]core.NodeName, 0, len(f.statuses))
	for _, status := range f.statuses {
		names = append(names, status.NodeName)
	}
	return names, nil
}

func (f *fakeCollector) WatchNode(_ context.Context, nodeName core.NodeName) error {
	if f.watchCalls == nil {
		f.watchCalls = make(map[core.NodeName]int)
	}
	f.watchCalls[nodeName]++
	return nil
}

func (f *fakeCollector) GetStatus(context.Context) ([]collector.NodeStatus, error) {
	return append([]collector.NodeStatus(nil), f.statuses...), nil
}

func (f *fakeCollector) Snapshots() []store.NodeSnapshot {
	return append([]store.NodeSnapshot(nil), f.snapshots...)
}

func (f *fakeCollector) MarkNodeDegraded(core.NodeName, error) {}

func (f *fakeCollector) LocalTailscaleIP(context.Context) (string, error) {
	return "100.64.0.1", nil
}
