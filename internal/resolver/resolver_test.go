package resolver

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/store"
)

func TestBuildIndexAndResolveTarget(t *testing.T) {
	t.Parallel()

	snapshots := []store.NodeSnapshot{
		{
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			Ports:       []store.ListenPort{{Port: 8080, Process: "api"}},
			Containers:  []store.ContainerPort{{HostPort: 8080, ContainerName: "api"}},
		},
		{
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			Ports:       []store.ListenPort{{Port: 9090, Process: "worker"}},
		},
	}
	index := BuildIndex(snapshots)

	tests := []struct {
		name     string
		target   parser.ForwardTarget
		wantNode core.NodeName
		wantPort uint16
		wantOK   bool
	}{
		{"tailscale ip", parser.ForwardTarget{Kind: parser.TargetKindAddress, Host: "100.64.0.2", Port: 9090}, "node-b", 9090, true},
		{"container name", parser.ForwardTarget{Kind: parser.TargetKindAddress, Host: "api", Port: 8080}, "node-a", 8080, true},
		{"unknown", parser.ForwardTarget{Kind: parser.TargetKindAddress, Host: "unknown", Port: 8080}, "", 8080, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNode, gotPort, gotOK := ResolveTarget(tt.target, index)
			if gotNode != tt.wantNode || gotPort != tt.wantPort || gotOK != tt.wantOK {
				t.Fatalf("ResolveTarget(%#v) = (%q,%d,%t), want (%q,%d,%t)", tt.target, gotNode, gotPort, gotOK, tt.wantNode, tt.wantPort, tt.wantOK)
			}
		})
	}
}

func TestDiffEdges(t *testing.T) {
	t.Parallel()

	prev := []store.TopologyEdge{{
		FromNode:    "node-a",
		FromPort:    80,
		ToNode:      "node-b",
		ToPort:      8080,
		Kind:        store.EdgeKindProxyPass,
		Resolved:    true,
		RawUpstream: "http://100.64.0.2:8080",
	}}
	cur := []store.TopologyEdge{
		{
			FromNode:    "node-a",
			FromPort:    80,
			ToNode:      "node-c",
			ToPort:      8081,
			Kind:        store.EdgeKindProxyPass,
			Resolved:    true,
			RawUpstream: "http://100.64.0.2:8080",
		},
		{
			FromNode:    "node-a",
			FromPort:    443,
			ToNode:      "node-d",
			ToPort:      8443,
			Kind:        store.EdgeKindProxyPass,
			Resolved:    true,
			RawUpstream: "https://100.64.0.4:8443",
		},
	}

	diff := DiffEdges(prev, cur)
	if len(diff.Changed) != 1 {
		t.Fatalf("len(diff.Changed) = %d, want 1", len(diff.Changed))
	}
	if len(diff.Added) != 1 {
		t.Fatalf("len(diff.Added) = %d, want 1", len(diff.Added))
	}
	if len(diff.Removed) != 0 {
		t.Fatalf("len(diff.Removed) = %d, want 0", len(diff.Removed))
	}
}

func TestResolveRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	bus := core.NewEventBus()
	r := NewResolver(db.Edges(), db.Snapshots(), bus)

	run := store.CollectionRun{ID: "run-1"}
	snapshots := []store.NodeSnapshot{
		{
			ID:          "snap-a",
			RunID:       "run-1",
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
			Containers:  []store.ContainerPort{{HostPort: 3000, ContainerPort: 3000, ContainerName: "api"}},
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
			ID:          "snap-b",
			RunID:       "run-1",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			Ports:       []store.ListenPort{{Port: 8080, Process: "app"}},
		},
	}

	edges, err := r.ResolveRun(ctx, run, snapshots)
	if err != nil {
		t.Fatalf("ResolveRun(): %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("len(edges) = %d, want 2", len(edges))
	}

	stored, err := db.Edges().ListByRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListByRun(): %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("len(stored) = %d, want 2", len(stored))
	}

	var proxyEdge *store.TopologyEdge
	for i := range edges {
		if edges[i].Kind == store.EdgeKindProxyPass {
			proxyEdge = &edges[i]
			break
		}
	}
	if proxyEdge == nil || proxyEdge.ToNode != "node-b" || !proxyEdge.Resolved {
		t.Fatalf("proxy edge = %#v, want resolved edge to node-b", proxyEdge)
	}
}

func TestResolveContainerEdgesDeduplicatesDuplicateContainerMappings(t *testing.T) {
	t.Parallel()

	snapshot := store.NodeSnapshot{
		NodeName: "warehouse-13-1",
		Containers: []store.ContainerPort{
			{
				ContainerID:   "c1",
				ContainerName: "devbox-ui",
				HostPort:      80,
				ContainerPort: 80,
				Proto:         "tcp",
			},
			{
				ContainerID:   "c1",
				ContainerName: "devbox-ui",
				HostPort:      80,
				ContainerPort: 80,
				Proto:         "tcp",
			},
		},
	}

	edges := resolveContainerEdges("run-1", snapshot)
	if len(edges) != 1 {
		t.Fatalf("len(resolveContainerEdges(...)) = %d, want 1; got %#v", len(edges), edges)
	}
	if edges[0].FromPort != 80 || edges[0].ToPort != 80 || edges[0].ToContainer != "devbox-ui" {
		t.Fatalf("edge = %#v", edges[0])
	}
}

func TestResolveSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	r := NewResolver(db.Edges(), db.Snapshots(), nil)

	initialA := store.NodeSnapshot{
		ID:          "snap-a1",
		RunID:       "run-1",
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
	}
	initialB := store.NodeSnapshot{
		ID:          "snap-b1",
		RunID:       "run-1",
		NodeName:    "node-b",
		TailscaleIP: "100.64.0.2",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 8080, Process: "app"}},
	}
	for _, snapshot := range []store.NodeSnapshot{initialA, initialB} {
		if err := db.Snapshots().Save(ctx, snapshot); err != nil {
			t.Fatalf("save snapshot: %v", err)
		}
	}
	if _, err := r.ResolveRun(ctx, store.CollectionRun{ID: "run-1"}, []store.NodeSnapshot{initialA, initialB}); err != nil {
		t.Fatalf("ResolveRun(): %v", err)
	}

	updatedA := initialA
	updatedA.ID = "snap-a2"
	updatedA.CollectedAt = core.NowTimestamp()
	updatedA.Forwards = []parser.ForwardAction{{
		Listener: parser.Listener{Port: 80},
		Target: parser.ForwardTarget{
			Raw:  "http://100.64.0.2:9090",
			Kind: parser.TargetKindAddress,
			Host: "100.64.0.2",
			Port: 9090,
		},
	}}
	if err := db.Snapshots().Save(ctx, updatedA); err != nil {
		t.Fatalf("save updated snapshot: %v", err)
	}

	edges, err := r.ResolveSnapshot(ctx, updatedA)
	if err != nil {
		t.Fatalf("ResolveSnapshot(): %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("len(edges) = %d, want 1", len(edges))
	}
	if edges[0].ToPort != 9090 || edges[0].Resolved {
		t.Fatalf("edge = %#v, want unresolved edge to port 9090", edges[0])
	}
}

func openTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()

	path := filepath.Join(t.TempDir(), "tailflow-resolver.sqlite")
	db, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite(): %v", err)
	}
	return db
}
