package resolver

import (
	"context"
	"encoding/json"
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
			Containers: []store.Container{{
				ContainerName: "api",
				PublishedPorts: []store.ContainerPublishedPort{{
					HostPort:   8080,
					TargetPort: 8080,
					Proto:      "tcp",
					Source:     "container",
				}},
			}},
			Services: []store.SwarmServicePort{{HostPort: 3000, ServiceName: "unipilot_api"}},
		},
		{
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			Ports:       []store.ListenPort{{Addr: "192.168.1.20", Port: 9090, Process: "worker"}},
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
		{"lan ip", parser.ForwardTarget{Kind: parser.TargetKindAddress, Host: "192.168.1.20", Port: 9090}, "node-b", 9090, true},
		{"container name", parser.ForwardTarget{Kind: parser.TargetKindAddress, Host: "api", Port: 8080}, "node-a", 8080, true},
		{"service name", parser.ForwardTarget{Kind: parser.TargetKindAddress, Host: "unipilot_api", Port: 3000}, "node-a", 3000, true},
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

func TestResolveTargetKnownNodeRequiresAdvertisedPortWhenInventoryExists(t *testing.T) {
	t.Parallel()

	index := BuildIndex([]store.NodeSnapshot{{
		NodeName:    "node-b",
		TailscaleIP: "100.64.0.2",
		Ports:       []store.ListenPort{{Port: 8080, Process: "worker"}},
	}})

	gotNode, gotPort, gotOK := ResolveTarget(parser.ForwardTarget{
		Kind: parser.TargetKindAddress,
		Host: "100.64.0.2",
		Port: 9090,
	}, index)
	if gotNode != "" || gotPort != 9090 || gotOK {
		t.Fatalf("ResolveTarget() = (%q,%d,%t), want unresolved target port 9090", gotNode, gotPort, gotOK)
	}
}

func TestResolveTargetDoesNotUseGenericDottedHostnameAsAlias(t *testing.T) {
	t.Parallel()

	index := BuildIndex([]store.NodeSnapshot{
		{
			NodeName: "api",
			Ports:    []store.ListenPort{{Port: 8080, Process: "api"}},
		},
		{
			NodeName: "node-b",
			Ports:    []store.ListenPort{{Port: 8080, Process: "worker"}},
		},
	})

	gotNode, gotPort, gotOK := ResolveTarget(parser.ForwardTarget{
		Kind: parser.TargetKindAddress,
		Host: "api.example.com",
		Port: 8080,
	}, index)
	if gotNode != "" || gotPort != 8080 || gotOK {
		t.Fatalf("ResolveTarget() = (%q,%d,%t), want unresolved dotted hostname", gotNode, gotPort, gotOK)
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
	if diff.Removed == nil {
		t.Fatal("diff.Removed = nil, want empty slice")
	}

	body, err := json.Marshal(diff)
	if err != nil {
		t.Fatalf("json.Marshal(diff) error = %v", err)
	}
	want := `{"added":[{"id":"","run_id":"","from_node":"node-a","from_port":443,"from_process":"","from_container":"","to_node":"node-d","to_port":8443,"to_process":"","to_container":"","to_service":"","kind":"proxy_pass","resolved":true,"raw_upstream":"https://100.64.0.4:8443"}],"removed":[],"changed":[{"id":"","run_id":"","from_node":"node-a","from_port":80,"from_process":"","from_container":"","to_node":"node-c","to_port":8081,"to_process":"","to_container":"","to_service":"","kind":"proxy_pass","resolved":true,"raw_upstream":"http://100.64.0.2:8080"}]}`
	if string(body) != want {
		t.Fatalf("json.Marshal(diff) = %s, want %s", body, want)
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
			Containers: []store.Container{{
				ContainerName: "api",
				PublishedPorts: []store.ContainerPublishedPort{{
					HostPort:   3000,
					TargetPort: 3000,
					Proto:      "tcp",
					Source:     "container",
				}},
			}},
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

func TestResolveRunAnnotatesProxyEdgesWithResolvedService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	r := NewResolver(db.Edges(), db.Snapshots(), nil)

	run := store.CollectionRun{ID: "run-1"}
	snapshots := []store.NodeSnapshot{
		{
			ID:          "snap-a",
			RunID:       "run-1",
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://100.64.0.2:3000",
					Kind: parser.TargetKindAddress,
					Host: "100.64.0.2",
					Port: 3000,
				},
			}},
		},
		{
			ID:          "snap-b",
			RunID:       "run-1",
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			Services: []store.SwarmServicePort{{
				ServiceID:   "svc-1",
				ServiceName: "unipilot_api",
				HostPort:    3000,
				TargetPort:  3000,
				Proto:       "tcp",
				Mode:        "ingress",
			}},
		},
	}

	edges, err := r.ResolveRun(ctx, run, snapshots)
	if err != nil {
		t.Fatalf("ResolveRun(): %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("len(edges) = %d, want 1", len(edges))
	}

	proxyEdge := &edges[0]
	if proxyEdge == nil || !proxyEdge.Resolved || proxyEdge.ToNode != "node-b" || proxyEdge.ToService != "unipilot_api" {
		t.Fatalf("proxy edge = %#v, want resolved proxy edge to node-b service unipilot_api", proxyEdge)
	}
}

func TestResolveRunAnnotatesProxyEdgesWithServiceDerivedWorkerTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	r := NewResolver(db.Edges(), db.Snapshots(), nil)

	run := store.CollectionRun{ID: "run-1"}
	snapshots := []store.NodeSnapshot{
		{
			ID:       "snap-source",
			RunID:    "run-1",
			NodeName: "wwwill-1",
			Ports:    []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://unipilot-2.lab:3002",
					Kind: parser.TargetKindAddress,
					Host: "unipilot-2.lab",
					Port: 3002,
				},
			}},
		},
		{
			ID:       "snap-target",
			RunID:    "run-1",
			NodeName: "unipilot-2",
			Ports:    []store.ListenPort{{Port: 3002}},
			Containers: []store.Container{{
				ContainerName: "unipilot_sse.1.xyz",
				ServiceName:   "unipilot_sse",
				State:         "running",
				PublishedPorts: []store.ContainerPublishedPort{{
					HostPort:   3002,
					TargetPort: 3002,
					Proto:      "tcp",
					Source:     "service",
					Mode:       "ingress",
				}},
			}},
		},
	}

	edges, err := r.ResolveRun(ctx, run, snapshots)
	if err != nil {
		t.Fatalf("ResolveRun(): %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("len(edges) = %d, want 1", len(edges))
	}

	proxyEdge := edges[0]
	if !proxyEdge.Resolved || proxyEdge.ToNode != "unipilot-2" || proxyEdge.ToService != "unipilot_sse" {
		t.Fatalf("proxy edge = %#v, want resolved edge to worker service unipilot_sse", proxyEdge)
	}
	if proxyEdge.ToContainer != "unipilot_sse.1.xyz" || proxyEdge.ToRuntimeNode != "unipilot-2" || proxyEdge.ToRuntimeContainer != "unipilot_sse.1.xyz" {
		t.Fatalf("proxy edge runtime = %#v, want local worker task metadata", proxyEdge)
	}
}

func TestResolveRunSeparatesReachableServiceFromRemoteRuntimeTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	r := NewResolver(db.Edges(), db.Snapshots(), nil)

	run := store.CollectionRun{ID: "run-1"}
	snapshots := []store.NodeSnapshot{
		{
			ID:       "snap-source",
			RunID:    "run-1",
			NodeName: "wwwill-1",
			Ports:    []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://unipilot-1.lab:3002",
					Kind: parser.TargetKindAddress,
					Host: "unipilot-1.lab",
					Port: 3002,
				},
			}},
		},
		{
			ID:       "snap-manager",
			RunID:    "run-1",
			NodeName: "unipilot-1",
			Ports:    []store.ListenPort{{Port: 3002}},
			Services: []store.SwarmServicePort{{
				ServiceID:   "svc-sse",
				ServiceName: "unipilot_sse",
				HostPort:    3002,
				TargetPort:  3002,
				Proto:       "tcp",
				Mode:        "ingress",
			}},
		},
		{
			ID:       "snap-worker",
			RunID:    "run-1",
			NodeName: "unipilot-2",
			Ports:    []store.ListenPort{{Port: 3002}},
			Containers: []store.Container{{
				ContainerName: "unipilot_sse.1.xyz",
				ServiceName:   "unipilot_sse",
				State:         "running",
				PublishedPorts: []store.ContainerPublishedPort{{
					HostPort:   3002,
					TargetPort: 3002,
					Proto:      "tcp",
					Source:     "service",
					Mode:       "ingress",
				}},
			}},
		},
	}

	edges, err := r.ResolveRun(ctx, run, snapshots)
	if err != nil {
		t.Fatalf("ResolveRun(): %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("len(edges) = %d, want 1", len(edges))
	}

	proxyEdge := edges[0]
	if !proxyEdge.Resolved || proxyEdge.ToNode != "unipilot-1" || proxyEdge.ToService != "unipilot_sse" {
		t.Fatalf("proxy edge = %#v, want reachable service on unipilot-1", proxyEdge)
	}
	if proxyEdge.ToContainer != "" {
		t.Fatalf("proxy edge ToContainer = %q, want no local task on reachable node", proxyEdge.ToContainer)
	}
	if proxyEdge.ToRuntimeNode != "unipilot-2" || proxyEdge.ToRuntimeContainer != "unipilot_sse.1.xyz" {
		t.Fatalf("proxy edge runtime = (%q,%q), want remote runtime task on unipilot-2", proxyEdge.ToRuntimeNode, proxyEdge.ToRuntimeContainer)
	}
}

func TestResolveRunResolvesNodeHostnameAliasWithoutTargetPorts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	r := NewResolver(db.Edges(), db.Snapshots(), nil)

	run := store.CollectionRun{ID: "run-1"}
	snapshots := []store.NodeSnapshot{
		{
			ID:       "snap-source",
			RunID:    "run-1",
			NodeName: "wwwill-1",
			Ports:    []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "unipilot-2.lab:3001",
					Kind: parser.TargetKindAddress,
					Host: "unipilot-2.lab",
					Port: 3001,
				},
			}},
		},
		{
			ID:          "snap-target",
			RunID:       "run-1",
			NodeName:    "unipilot-2",
			TailscaleIP: "100.74.111.75",
		},
	}

	edges, err := r.ResolveRun(ctx, run, snapshots)
	if err != nil {
		t.Fatalf("ResolveRun(): %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("len(edges) = %d, want 1", len(edges))
	}
	if edges[0].ToNode != "unipilot-2" || edges[0].ToPort != 3001 || !edges[0].Resolved {
		t.Fatalf("edge = %#v, want resolved edge to unipilot-2:3001", edges[0])
	}
}

func TestResolveRunResolvesTailscaleIPWithoutTargetPorts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	r := NewResolver(db.Edges(), db.Snapshots(), nil)

	run := store.CollectionRun{ID: "run-1"}
	snapshots := []store.NodeSnapshot{
		{
			ID:       "snap-source",
			RunID:    "run-1",
			NodeName: "wwwill-1",
			Ports:    []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://100.104.22.121:5000/",
					Kind: parser.TargetKindAddress,
					Host: "100.104.22.121",
					Port: 5000,
				},
			}},
		},
		{
			ID:          "snap-target",
			RunID:       "run-1",
			NodeName:    "newsroom-api-1",
			TailscaleIP: "100.104.22.121",
		},
	}

	edges, err := r.ResolveRun(ctx, run, snapshots)
	if err != nil {
		t.Fatalf("ResolveRun(): %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("len(edges) = %d, want 1", len(edges))
	}
	if edges[0].ToNode != "newsroom-api-1" || edges[0].ToPort != 5000 || !edges[0].Resolved {
		t.Fatalf("edge = %#v, want resolved edge to newsroom-api-1:5000", edges[0])
	}
}

func TestResolveRunResolvesShortTHostAlias(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	r := NewResolver(db.Edges(), db.Snapshots(), nil)

	run := store.CollectionRun{ID: "run-1"}
	snapshots := []store.NodeSnapshot{
		{
			ID:       "snap-source",
			RunID:    "run-1",
			NodeName: "wwwill-1",
			Ports:    []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://warehouse-13-1-t",
					Kind: parser.TargetKindAddress,
					Host: "warehouse-13-1-t",
					Port: 80,
				},
			}},
		},
		{
			ID:       "snap-target",
			RunID:    "run-1",
			NodeName: "warehouse-13-1",
			Ports:    []store.ListenPort{{Port: 80, Process: "nginx"}},
		},
	}

	edges, err := r.ResolveRun(ctx, run, snapshots)
	if err != nil {
		t.Fatalf("ResolveRun(): %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("len(edges) = %d, want 1", len(edges))
	}
	if edges[0].ToNode != "warehouse-13-1" || edges[0].ToPort != 80 || !edges[0].Resolved {
		t.Fatalf("edge = %#v, want resolved edge to warehouse-13-1:80", edges[0])
	}
}

func TestResolveContainerEdgesDeduplicatesDuplicateContainerMappings(t *testing.T) {
	t.Parallel()

	snapshot := store.NodeSnapshot{
		NodeName: "warehouse-13-1",
		Containers: []store.Container{
			{
				ContainerID:   "c1",
				ContainerName: "devbox-ui",
				PublishedPorts: []store.ContainerPublishedPort{{
					HostPort:   80,
					TargetPort: 80,
					Proto:      "tcp",
					Source:     "container",
				}},
			},
			{
				ContainerID:   "c1",
				ContainerName: "devbox-ui",
				PublishedPorts: []store.ContainerPublishedPort{{
					HostPort:   80,
					TargetPort: 80,
					Proto:      "tcp",
					Source:     "container",
				}},
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
			Raw:  "http://100.64.0.2:8080",
			Kind: parser.TargetKindAddress,
			Host: "100.64.0.2",
			Port: 8080,
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
	if edges[0].ToNode != "node-b" || edges[0].ToPort != 8080 || !edges[0].Resolved {
		t.Fatalf("edge = %#v, want resolved edge to node-b:8080", edges[0])
	}
}

func TestResolveSnapshotUsesSameRunSnapshotsOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	r := NewResolver(db.Edges(), db.Snapshots(), nil)

	run1Source := store.NodeSnapshot{
		ID:          "snap-a1",
		RunID:       "run-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
		Forwards: []parser.ForwardAction{{
			Listener: parser.Listener{Port: 80},
			Target: parser.ForwardTarget{
				Raw:  "http://service.internal:9090",
				Kind: parser.TargetKindAddress,
				Host: "service.internal",
				Port: 9090,
			},
		}},
	}
	run1Peer := store.NodeSnapshot{
		ID:          "snap-b1",
		RunID:       "run-1",
		NodeName:    "node-b",
		TailscaleIP: "100.64.0.2",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 8080, Process: "app"}},
	}
	run2Peer := store.NodeSnapshot{
		ID:          "snap-c1",
		RunID:       "run-2",
		NodeName:    "node-c",
		TailscaleIP: "100.64.0.3",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 9090, Process: "app"}},
	}

	for _, snapshot := range []store.NodeSnapshot{run1Source, run1Peer, run2Peer} {
		if err := db.Snapshots().Save(ctx, snapshot); err != nil {
			t.Fatalf("save snapshot: %v", err)
		}
	}
	if _, err := r.ResolveRun(ctx, store.CollectionRun{ID: "run-1"}, []store.NodeSnapshot{run1Source, run1Peer}); err != nil {
		t.Fatalf("ResolveRun(): %v", err)
	}

	updatedSource := run1Source
	updatedSource.ID = "snap-a2"
	updatedSource.CollectedAt = core.NowTimestamp()
	if err := db.Snapshots().Save(ctx, updatedSource); err != nil {
		t.Fatalf("save updated snapshot: %v", err)
	}

	edges, err := r.ResolveSnapshot(ctx, updatedSource)
	if err != nil {
		t.Fatalf("ResolveSnapshot(): %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("len(edges) = %d, want 1", len(edges))
	}
	if edges[0].Resolved || edges[0].ToNode != "" || edges[0].ToPort != 9090 {
		t.Fatalf("edge = %#v, want unresolved edge that ignores run-2 snapshots", edges[0])
	}
}

func TestTargetMetadataPrefersContainer(t *testing.T) {
	t.Parallel()

	index := BuildIndex([]store.NodeSnapshot{{
		NodeName: "node-a",
		Ports:    []store.ListenPort{{Port: 8080, Process: "nginx"}},
		Containers: []store.Container{{
			ContainerName: "api",
			PublishedPorts: []store.ContainerPublishedPort{{
				HostPort:   8080,
				TargetPort: 3000,
				Proto:      "tcp",
				Source:     "container",
			}},
		}},
	}})

	details := targetMetadata(index, "node-a", 8080)
	if details.Process != "" || details.Container != "api" || details.Service != "" {
		t.Fatalf("targetMetadata(...) = %#v, want container-preferred metadata", details)
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
