package topology

import (
	"testing"
	"time"

	"github.com/wf-pro-dev/tailflow/internal/collector"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/store"
)

func TestManagerResetAndApplyPatch(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	initialSnapshots := []store.NodeSnapshot{
		{
			ID:          "snap-a1",
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			CollectedAt: core.NowTimestamp(),
			Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://100.64.0.2:8080",
					Kind: parser.TargetKindAddress,
					Host: "100.64.0.2",
					Port: 8080,
				},
			}},
		},
		{
			ID:          "snap-b1",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			CollectedAt: core.NowTimestamp(),
			Ports:       []store.ListenPort{{Port: 8080, Process: "app"}},
		},
	}
	statuses := []collector.NodeStatus{
		{NodeName: "node-a", Online: true, LastSeenAt: core.NowTimestamp()},
		{NodeName: "node-b", Online: true, LastSeenAt: core.NowTimestamp()},
	}

	reset := manager.Reset(initialSnapshots, statuses, "bootstrap")
	if reset.Snapshot.Version != 1 {
		t.Fatalf("reset version = %d, want 1", reset.Snapshot.Version)
	}
	if len(reset.Snapshot.Routes) != 1 {
		t.Fatalf("len(reset.Snapshot.Routes) = %d, want 1", len(reset.Snapshot.Routes))
	}

	updatedSnapshots := []store.NodeSnapshot{
		initialSnapshots[0],
		{
			ID:          "snap-b2",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			CollectedAt: core.NowTimestamp(),
			Ports:       []store.ListenPort{{Port: 9090, Process: "app"}},
		},
	}
	patch, changed := manager.Apply(updatedSnapshots, statuses)
	if !changed {
		t.Fatal("Apply() changed = false, want true")
	}
	if patch.Version != 2 {
		t.Fatalf("patch version = %d, want 2", patch.Version)
	}
	if len(patch.ChangedNodes) == 0 || patch.ChangedNodes[0] != "node-b" {
		t.Fatalf("patch.ChangedNodes = %#v, want node-b", patch.ChangedNodes)
	}
	if len(patch.RoutesUpserted) != 1 || patch.RoutesUpserted[0].Resolved {
		t.Fatalf("patch.RoutesUpserted = %#v, want one unresolved updated route", patch.RoutesUpserted)
	}
}

func TestManagerApplyNoChange(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	snapshots := []store.NodeSnapshot{{
		ID:          "snap-a1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
	}}
	statuses := []collector.NodeStatus{{NodeName: "node-a", Online: true, LastSeenAt: core.NowTimestamp()}}

	manager.Reset(snapshots, statuses, "bootstrap")
	if patch, changed := manager.Apply(snapshots, statuses); changed || !patch.isEmpty() {
		t.Fatalf("Apply() = (%#v, %t), want empty false", patch, changed)
	}
}

func TestManagerApplyIgnoresTimestampOnlyRuntimeChanges(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	baseTime := core.NewTimestamp(time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC))
	nextTime := core.NewTimestamp(time.Date(2026, 5, 1, 3, 0, 5, 0, time.UTC))

	snapshots := []store.NodeSnapshot{{
		ID:          "snap-a1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: baseTime,
		Ports:       []store.ListenPort{{Port: 80, Process: "nginx", PID: 123}},
	}}
	statuses := []collector.NodeStatus{{NodeName: "node-a", Online: true, LastSeenAt: baseTime}}

	manager.Reset(snapshots, statuses, "bootstrap")

	updatedSnapshots := []store.NodeSnapshot{{
		ID:          "snap-a2",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: nextTime,
		Ports:       []store.ListenPort{{Port: 80, Process: "nginx", PID: 123}},
	}}
	updatedStatuses := []collector.NodeStatus{{NodeName: "node-a", Online: true, LastSeenAt: nextTime}}

	if patch, changed := manager.Apply(updatedSnapshots, updatedStatuses); changed || !patch.isEmpty() {
		t.Fatalf("Apply() = (%#v, %t), want empty false for timestamp-only runtime change", patch, changed)
	}
}

func TestManagerApplyNodeScopeScopesPatchToAffectedNode(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	baseTime := core.NewTimestamp(time.Date(2026, 5, 1, 4, 0, 0, 0, time.UTC))
	nextTime := core.NewTimestamp(time.Date(2026, 5, 1, 4, 0, 5, 0, time.UTC))

	initialSnapshots := []store.NodeSnapshot{
		{
			ID:          "snap-a1",
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			CollectedAt: baseTime,
			Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://100.64.0.2:8080",
					Kind: parser.TargetKindAddress,
					Host: "100.64.0.2",
					Port: 8080,
				},
			}},
		},
		{
			ID:          "snap-b1",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			CollectedAt: baseTime,
			Ports:       []store.ListenPort{{Port: 8080, Process: "app"}},
		},
		{
			ID:          "snap-c1",
			NodeName:    "node-c",
			TailscaleIP: "100.64.0.3",
			CollectedAt: baseTime,
			Ports:       []store.ListenPort{{Port: 81, Process: "caddy"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 81},
				Target: parser.ForwardTarget{
					Raw:  "http://100.64.0.4:9090",
					Kind: parser.TargetKindAddress,
					Host: "100.64.0.4",
					Port: 9090,
				},
			}},
		},
		{
			ID:          "snap-d1",
			NodeName:    "node-d",
			TailscaleIP: "100.64.0.4",
			CollectedAt: baseTime,
			Ports:       []store.ListenPort{{Port: 9090, Process: "api"}},
		},
	}
	statuses := []collector.NodeStatus{
		{NodeName: "node-a", Online: true, LastSeenAt: baseTime},
		{NodeName: "node-b", Online: true, LastSeenAt: baseTime},
		{NodeName: "node-c", Online: true, LastSeenAt: baseTime},
		{NodeName: "node-d", Online: true, LastSeenAt: baseTime},
	}

	reset := manager.Reset(initialSnapshots, statuses, "bootstrap")
	if len(reset.Snapshot.Routes) != 2 {
		t.Fatalf("len(reset.Snapshot.Routes) = %d, want 2", len(reset.Snapshot.Routes))
	}

	updatedSnapshots := []store.NodeSnapshot{
		initialSnapshots[0],
		{
			ID:          "snap-b2",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			CollectedAt: nextTime,
			Ports:       []store.ListenPort{{Port: 9091, Process: "app"}},
		},
		initialSnapshots[2],
		initialSnapshots[3],
	}
	updatedStatuses := []collector.NodeStatus{
		{NodeName: "node-a", Online: true, LastSeenAt: nextTime},
		{NodeName: "node-b", Online: true, LastSeenAt: nextTime},
		{NodeName: "node-c", Online: true, LastSeenAt: nextTime},
		{NodeName: "node-d", Online: true, LastSeenAt: nextTime},
	}

	patch, changed := manager.ApplyNodeScope("node-b", ScopedIDs{}, FullNodeScopeMask, updatedSnapshots, updatedStatuses)
	if !changed {
		t.Fatal("ApplyNodeScope() changed = false, want true")
	}
	if patch.Version != 2 {
		t.Fatalf("patch version = %d, want 2", patch.Version)
	}
	if len(patch.ChangedNodes) != 1 || patch.ChangedNodes[0] != "node-b" {
		t.Fatalf("patch.ChangedNodes = %#v, want [node-b]", patch.ChangedNodes)
	}
	if len(patch.NodesUpserted) != 1 || patch.NodesUpserted[0].Name != "node-b" {
		t.Fatalf("patch.NodesUpserted = %#v, want one node-b update", patch.NodesUpserted)
	}
	if len(patch.ServicesUpserted) != 1 {
		t.Fatalf("len(patch.ServicesUpserted) = %d, want 1", len(patch.ServicesUpserted))
	}
	if len(patch.ServicesRemoved) != 0 {
		t.Fatalf("patch.ServicesRemoved = %#v, want none", patch.ServicesRemoved)
	}
	for _, node := range patch.NodesUpserted {
		if node.Name == "node-c" || node.Name == "node-d" {
			t.Fatalf("patch.NodesUpserted leaked unrelated node update: %#v", patch.NodesUpserted)
		}
	}
	for _, service := range patch.ServicesUpserted {
		if service.PrimaryNode == "node-c" || service.PrimaryNode == "node-d" {
			t.Fatalf("patch.ServicesUpserted leaked unrelated service update: %#v", patch.ServicesUpserted)
		}
	}
}

func TestManagerApplyNodeScopeIgnoresTimestampOnlyNodeChanges(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	baseTime := core.NewTimestamp(time.Date(2026, 5, 1, 5, 0, 0, 0, time.UTC))
	nextTime := core.NewTimestamp(time.Date(2026, 5, 1, 5, 0, 5, 0, time.UTC))

	snapshots := []store.NodeSnapshot{
		{
			ID:          "snap-a1",
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			CollectedAt: baseTime,
			Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://100.64.0.2:8080",
					Kind: parser.TargetKindAddress,
					Host: "100.64.0.2",
					Port: 8080,
				},
			}},
		},
		{
			ID:          "snap-b1",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			CollectedAt: baseTime,
			Ports:       []store.ListenPort{{Port: 8080, Process: "app"}},
		},
	}
	statuses := []collector.NodeStatus{
		{NodeName: "node-a", Online: true, LastSeenAt: baseTime},
		{NodeName: "node-b", Online: true, LastSeenAt: baseTime},
	}

	manager.Reset(snapshots, statuses, "bootstrap")

	updatedSnapshots := []store.NodeSnapshot{
		{
			ID:          "snap-a2",
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			CollectedAt: nextTime,
			Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://100.64.0.2:8080",
					Kind: parser.TargetKindAddress,
					Host: "100.64.0.2",
					Port: 8080,
				},
			}},
		},
		{
			ID:          "snap-b2",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			CollectedAt: nextTime,
			Ports:       []store.ListenPort{{Port: 8080, Process: "app"}},
		},
	}
	updatedStatuses := []collector.NodeStatus{
		{NodeName: "node-a", Online: true, LastSeenAt: nextTime},
		{NodeName: "node-b", Online: true, LastSeenAt: nextTime},
	}

	if patch, changed := manager.ApplyNodeScope("node-b", ScopedIDs{}, FullNodeScopeMask, updatedSnapshots, updatedStatuses); changed || !patch.isEmpty() {
		t.Fatalf("ApplyNodeScope() = (%#v, %t), want empty false for timestamp-only node change", patch, changed)
	}
}

func TestManagerApplyNodeScopeRespectsMask(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	baseTime := core.NewTimestamp(time.Date(2026, 5, 1, 6, 0, 0, 0, time.UTC))
	nextTime := core.NewTimestamp(time.Date(2026, 5, 1, 6, 0, 5, 0, time.UTC))

	initialSnapshots := []store.NodeSnapshot{
		{
			ID:          "snap-a1",
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			CollectedAt: baseTime,
			Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://100.64.0.2:8080",
					Kind: parser.TargetKindAddress,
					Host: "100.64.0.2",
					Port: 8080,
				},
			}},
		},
		{
			ID:          "snap-b1",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			CollectedAt: baseTime,
			Ports:       []store.ListenPort{{Port: 8080, Process: "app"}},
		},
	}
	statuses := []collector.NodeStatus{
		{NodeName: "node-a", Online: true, LastSeenAt: baseTime},
		{NodeName: "node-b", Online: true, LastSeenAt: baseTime},
	}

	manager.Reset(initialSnapshots, statuses, "bootstrap")

	updatedSnapshots := []store.NodeSnapshot{
		{
			ID:          "snap-a2",
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			CollectedAt: nextTime,
			Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://100.64.0.2:9090",
					Kind: parser.TargetKindAddress,
					Host: "100.64.0.2",
					Port: 9090,
				},
			}},
		},
		{
			ID:          "snap-b2",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			CollectedAt: nextTime,
			Ports:       []store.ListenPort{{Port: 9090, Process: "app"}},
		},
	}
	updatedStatuses := []collector.NodeStatus{
		{NodeName: "node-a", Online: true, LastSeenAt: nextTime},
		{NodeName: "node-b", Online: true, LastSeenAt: nextTime},
	}

	patch, changed := manager.ApplyNodeScope("node-a", ScopedIDs{}, ScopeMask{
		Services:  true,
		Runtimes:  true,
		Exposures: true,
		Routes:    true,
		RouteHops: true,
		Evidence:  true,
	}, updatedSnapshots, updatedStatuses)
	if !changed {
		t.Fatal("ApplyNodeScope() changed = false, want true")
	}
	if len(patch.ChangedNodes) != 0 {
		t.Fatalf("patch.ChangedNodes = %#v, want none when node domain masked out", patch.ChangedNodes)
	}
	if len(patch.NodesUpserted) != 0 {
		t.Fatalf("patch.NodesUpserted = %#v, want none when node domain masked out", patch.NodesUpserted)
	}
	if len(patch.RoutesUpserted) == 0 && len(patch.RoutesRemoved) == 0 {
		t.Fatalf("patch routes unchanged = %#v, want route-domain activity", patch)
	}
}

func TestManagerApplyNodeScopeForwardRouteMaskSuppressesOtherDomains(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	baseTime := core.NewTimestamp(time.Date(2026, 5, 1, 7, 0, 0, 0, time.UTC))
	nextTime := core.NewTimestamp(time.Date(2026, 5, 1, 7, 0, 5, 0, time.UTC))

	initialSnapshots := []store.NodeSnapshot{
		{
			ID:          "snap-a1",
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			CollectedAt: baseTime,
			Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://100.64.0.2:8080",
					Kind: parser.TargetKindAddress,
					Host: "100.64.0.2",
					Port: 8080,
				},
			}},
		},
		{
			ID:          "snap-b1",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			CollectedAt: baseTime,
			Ports:       []store.ListenPort{{Port: 8080, Process: "app"}},
		},
	}
	statuses := []collector.NodeStatus{
		{NodeName: "node-a", Online: true, LastSeenAt: baseTime},
		{NodeName: "node-b", Online: true, LastSeenAt: baseTime},
	}

	manager.Reset(initialSnapshots, statuses, "bootstrap")

	updatedSnapshots := []store.NodeSnapshot{
		{
			ID:          "snap-a2",
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			CollectedAt: nextTime,
			Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://100.64.0.2:9090",
					Kind: parser.TargetKindAddress,
					Host: "100.64.0.2",
					Port: 9090,
				},
			}},
		},
		{
			ID:          "snap-b2",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			CollectedAt: nextTime,
			Ports:       []store.ListenPort{{Port: 9090, Process: "app"}},
		},
	}
	updatedStatuses := []collector.NodeStatus{
		{NodeName: "node-a", Online: true, LastSeenAt: nextTime},
		{NodeName: "node-b", Online: true, LastSeenAt: nextTime},
	}

	patch, changed := manager.ApplyNodeScope("node-a", ScopedIDs{}, ForwardRouteScopeMask, updatedSnapshots, updatedStatuses)
	if !changed {
		t.Fatal("ApplyNodeScope() changed = false, want true")
	}
	if len(patch.ServicesUpserted) != 0 || len(patch.RuntimesUpserted) != 0 || len(patch.ExposuresUpserted) != 0 {
		t.Fatalf("patch leaked non-route domains = %#v", patch)
	}
	if len(patch.RoutesUpserted) == 0 && len(patch.RoutesRemoved) == 0 {
		t.Fatalf("patch route domain unchanged = %#v, want route activity", patch)
	}
}

func TestManagerApplyForwardRoutesUpdatesRouteFamilyOnly(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	baseTime := core.NewTimestamp(time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC))
	nextTime := core.NewTimestamp(time.Date(2026, 5, 1, 8, 0, 5, 0, time.UTC))

	initialSnapshots := []store.NodeSnapshot{
		{
			ID:          "snap-a1",
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			CollectedAt: baseTime,
			Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://100.64.0.2:8080",
					Kind: parser.TargetKindAddress,
					Host: "100.64.0.2",
					Port: 8080,
				},
			}},
		},
		{
			ID:          "snap-b1",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			CollectedAt: baseTime,
			Ports: []store.ListenPort{
				{Port: 8080, Process: "app"},
				{Port: 9090, Process: "app"},
			},
		},
	}
	statuses := []collector.NodeStatus{
		{NodeName: "node-a", Online: true, LastSeenAt: baseTime},
		{NodeName: "node-b", Online: true, LastSeenAt: baseTime},
	}

	reset := manager.Reset(initialSnapshots, statuses, "bootstrap")
	if len(reset.Snapshot.Routes) != 1 {
		t.Fatalf("len(reset.Snapshot.Routes) = %d, want 1", len(reset.Snapshot.Routes))
	}
	initialRoute := reset.Snapshot.Routes[0]

	updatedSnapshots := []store.NodeSnapshot{
		{
			ID:          "snap-a2",
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			CollectedAt: nextTime,
			Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://100.64.0.2:9090",
					Kind: parser.TargetKindAddress,
					Host: "100.64.0.2",
					Port: 9090,
				},
			}},
		},
		{
			ID:          "snap-b2",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			CollectedAt: nextTime,
			Ports: []store.ListenPort{
				{Port: 8080, Process: "app"},
				{Port: 9090, Process: "app"},
			},
		},
	}
	updatedStatuses := []collector.NodeStatus{
		{NodeName: "node-a", Online: true, LastSeenAt: nextTime},
		{NodeName: "node-b", Online: true, LastSeenAt: nextTime},
	}

	patch, changed := manager.ApplyForwardRoutes("node-a", ScopedIDs{}, updatedSnapshots, updatedStatuses)
	if !changed {
		t.Fatal("ApplyForwardRoutes() changed = false, want true")
	}
	if patch.Version != 2 {
		t.Fatalf("patch version = %d, want 2", patch.Version)
	}
	if len(patch.ChangedNodes) != 0 || len(patch.NodesUpserted) != 0 {
		t.Fatalf("patch leaked node updates = %#v", patch)
	}
	if len(patch.ServicesUpserted) != 0 || len(patch.RuntimesUpserted) != 0 || len(patch.ExposuresUpserted) != 0 {
		t.Fatalf("patch leaked non-route topology updates = %#v", patch)
	}
	if len(patch.RoutesUpserted) != 1 {
		t.Fatalf("len(patch.RoutesUpserted) = %d, want 1", len(patch.RoutesUpserted))
	}
	if len(patch.RoutesRemoved) != 1 || patch.RoutesRemoved[0] != initialRoute.ID {
		t.Fatalf("patch.RoutesRemoved = %#v, want removal of prior route %q", patch.RoutesRemoved, initialRoute.ID)
	}
	if patch.RoutesUpserted[0].ID == initialRoute.ID {
		t.Fatalf("patch.RoutesUpserted[0].ID = %q, want a replacement route id", patch.RoutesUpserted[0].ID)
	}
	if patch.RoutesUpserted[0].Input != "http://100.64.0.2:9090" {
		t.Fatalf("patch.RoutesUpserted[0].Input = %q, want %q", patch.RoutesUpserted[0].Input, "http://100.64.0.2:9090")
	}
	if len(patch.RouteHopsUpserted) == 0 {
		t.Fatalf("patch.RouteHopsUpserted = %#v, want route hop updates", patch.RouteHopsUpserted)
	}
	if len(patch.EvidenceUpserted) == 0 {
		t.Fatalf("patch.EvidenceUpserted = %#v, want evidence updates", patch.EvidenceUpserted)
	}
}
