package runtime

import (
	"testing"

	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/store"
	"github.com/wf-pro-dev/tailflow/internal/topology"
)

func TestProjectionIndexScopeForNode(t *testing.T) {
	t.Parallel()

	index := NewProjectionIndex()
	index.Reset(topology.Snapshot{
		Nodes: []topology.Node{{Name: "node-a"}, {Name: "node-b"}},
		Services: []store.Service{
			{ID: "svc-a", PrimaryNode: "node-a"},
			{ID: "svc-b", PrimaryNode: "node-b"},
		},
		Runtimes: []store.Runtime{
			{ID: "rt-a", NodeID: "node-a"},
		},
		Exposures: []store.Exposure{
			{ID: "exp-a", NodeID: "node-a"},
		},
		Routes: []store.Route{
			{ID: "route-a", SourceServiceID: "svc-a", TargetRuntimeID: "rt-a"},
			{ID: "route-b", SourceServiceID: "svc-b"},
		},
		RouteHops: []store.RouteHop{
			{ID: "hop-a", RouteID: "route-a", EvidenceID: "evidence-a"},
		},
		Evidence: []store.Evidence{
			{ID: "evidence-a"},
		},
	})

	scope := index.ScopeForNode("node-a")
	if _, ok := scope.ServiceIDs["svc-a"]; !ok {
		t.Fatalf("scope.ServiceIDs = %#v, want svc-a", scope.ServiceIDs)
	}
	if _, ok := scope.RuntimeIDs["rt-a"]; !ok {
		t.Fatalf("scope.RuntimeIDs = %#v, want rt-a", scope.RuntimeIDs)
	}
	if _, ok := scope.ExposureIDs["exp-a"]; !ok {
		t.Fatalf("scope.ExposureIDs = %#v, want exp-a", scope.ExposureIDs)
	}
	if _, ok := scope.RouteIDs["route-a"]; !ok {
		t.Fatalf("scope.RouteIDs = %#v, want route-a", scope.RouteIDs)
	}
	if _, ok := scope.HopIDs["hop-a"]; !ok {
		t.Fatalf("scope.HopIDs = %#v, want hop-a", scope.HopIDs)
	}
	if _, ok := scope.EvidenceIDs["evidence-a"]; !ok {
		t.Fatalf("scope.EvidenceIDs = %#v, want evidence-a", scope.EvidenceIDs)
	}
	if _, ok := scope.RouteIDs["route-b"]; ok {
		t.Fatalf("scope.RouteIDs leaked unrelated route: %#v", scope.RouteIDs)
	}
}

func TestProjectionIndexScopeForForwardKeys(t *testing.T) {
	t.Parallel()

	index := NewProjectionIndex()
	index.Reset(topology.Snapshot{
		Nodes: []topology.Node{{Name: "node-a"}},
		Services: []store.Service{
			{ID: "svc-gw", PrimaryNode: "node-a"},
			{ID: "svc-target", PrimaryNode: "node-b"},
		},
		Exposures: []store.Exposure{
			{ID: "exp-a", NodeID: "node-a", Port: 80},
		},
		Routes: []store.Route{
			{
				ID:               "route-a",
				Kind:             "proxy_route",
				SourceServiceID:  "svc-gw",
				SourceExposureID: "exp-a",
				TargetServiceID:  "svc-target",
				DisplayName:      "gw -> target",
				Resolved:         true,
				Hostnames:        []string{"app.example.com"},
				Input:            "http://100.64.0.2:8080",
			},
		},
		RouteHops: []store.RouteHop{
			{ID: "hop-a", RouteID: "route-a", EvidenceID: "evidence-a"},
		},
		Evidence: []store.Evidence{
			{ID: "evidence-a"},
		},
	})

	scope := index.ScopeForForwardKeys("node-a", []string{
		forwardImpactKey("node-a", parser.ForwardAction{
			Listener:  parser.Listener{Port: 80},
			Target:    parser.ForwardTarget{Raw: "http://100.64.0.2:8080"},
			Hostnames: []string{"app.example.com"},
		}),
	})
	if _, ok := scope.RouteIDs["route-a"]; !ok {
		t.Fatalf("scope.RouteIDs = %#v, want route-a", scope.RouteIDs)
	}
	if _, ok := scope.ExposureIDs["exp-a"]; !ok {
		t.Fatalf("scope.ExposureIDs = %#v, want exp-a", scope.ExposureIDs)
	}
	if _, ok := scope.ServiceIDs["svc-target"]; !ok {
		t.Fatalf("scope.ServiceIDs = %#v, want svc-target", scope.ServiceIDs)
	}
	if len(scope.RouteIDs) == 0 {
		t.Fatal("len(scope.RouteIDs) = 0, want non-empty")
	}

	miss := index.ScopeForForwardKeys("node-a", []string{"missing"})
	if len(miss.ServiceIDs) != 0 || len(miss.RuntimeIDs) != 0 || len(miss.ExposureIDs) != 0 || len(miss.RouteIDs) != 0 || len(miss.HopIDs) != 0 || len(miss.EvidenceIDs) != 0 {
		t.Fatalf("missing forward scope = %#v, want empty", miss)
	}
}

func TestProjectionIndexScopeForServiceKeysAndContainerIDs(t *testing.T) {
	t.Parallel()

	index := NewProjectionIndex()
	index.Reset(topology.Snapshot{
		Nodes: []topology.Node{{Name: "node-a"}},
		Services: []store.Service{
			{ID: "svc-a", PrimaryNode: "node-a", RuntimeIDs: []core.ID{"rt-a"}, ExposureIDs: []core.ID{"exp-a"}},
		},
		Runtimes: []store.Runtime{
			{ID: "rt-a", NodeID: "node-a", ServiceID: "svc-a", ContainerID: "ctr-a"},
		},
		Exposures: []store.Exposure{
			{ID: "exp-a", NodeID: "node-a", ServiceID: "svc-a", RuntimeID: "rt-a", Port: 8080, Protocol: "tcp", Source: "swarm"},
		},
		Routes: []store.Route{
			{ID: "route-a", SourceServiceID: "svc-a", TargetRuntimeID: "rt-a"},
		},
		RouteHops: []store.RouteHop{
			{ID: "hop-a", RouteID: "route-a", EvidenceID: "evidence-a"},
		},
		Evidence: []store.Evidence{
			{ID: "evidence-a"},
		},
	})

	serviceScope := index.ScopeForServiceKeys("node-a", []string{"svc-a|8080|tcp|swarm"})
	if _, ok := serviceScope.ServiceIDs["svc-a"]; !ok {
		t.Fatalf("serviceScope.ServiceIDs = %#v, want svc-a", serviceScope.ServiceIDs)
	}
	if _, ok := serviceScope.ExposureIDs["exp-a"]; !ok {
		t.Fatalf("serviceScope.ExposureIDs = %#v, want exp-a", serviceScope.ExposureIDs)
	}

	containerScope := index.ScopeForContainerIDs("node-a", []string{"ctr-a"})
	if _, ok := containerScope.RuntimeIDs["rt-a"]; !ok {
		t.Fatalf("containerScope.RuntimeIDs = %#v, want rt-a", containerScope.RuntimeIDs)
	}
	if _, ok := containerScope.RouteIDs["route-a"]; !ok {
		t.Fatalf("containerScope.RouteIDs = %#v, want route-a", containerScope.RouteIDs)
	}
}

func TestProjectionIndexScopeForPortKeys(t *testing.T) {
	t.Parallel()

	index := NewProjectionIndex()
	index.Reset(topology.Snapshot{
		Nodes: []topology.Node{{
			Name:  "node-a",
			Ports: []store.ListenPort{{Addr: "0.0.0.0", Port: 8080, Proto: "tcp", PID: 123, Process: "api"}},
		}},
		Services: []store.Service{
			{ID: "svc-a", PrimaryNode: "node-a"},
		},
		Runtimes: []store.Runtime{
			{ID: "rt-a", NodeID: "node-a", ServiceID: "svc-a", Ports: []uint16{8080}},
		},
		Exposures: []store.Exposure{
			{ID: "exp-a", NodeID: "node-a", ServiceID: "svc-a", RuntimeID: "rt-a", Port: 8080},
		},
		Routes: []store.Route{
			{ID: "route-a", SourceExposureID: "exp-a", TargetRuntimeID: "rt-a", TargetServiceID: "svc-a"},
		},
		RouteHops: []store.RouteHop{
			{ID: "hop-a", RouteID: "route-a", EvidenceID: "evidence-a"},
		},
		Evidence: []store.Evidence{
			{ID: "evidence-a"},
		},
	})

	scope := index.ScopeForPortKeys("node-a", []string{listenPortKey(store.ListenPort{
		Addr: "0.0.0.0", Port: 8080, Proto: "tcp", PID: 123, Process: "api",
	})})
	if _, ok := scope.RuntimeIDs["rt-a"]; !ok {
		t.Fatalf("scope.RuntimeIDs = %#v, want rt-a", scope.RuntimeIDs)
	}
	if _, ok := scope.ExposureIDs["exp-a"]; !ok {
		t.Fatalf("scope.ExposureIDs = %#v, want exp-a", scope.ExposureIDs)
	}
	if _, ok := scope.RouteIDs["route-a"]; !ok {
		t.Fatalf("scope.RouteIDs = %#v, want route-a", scope.RouteIDs)
	}
}
