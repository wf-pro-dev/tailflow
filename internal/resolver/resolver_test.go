package resolver

import (
	"encoding/json"
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
		{NodeName: "api", Ports: []store.ListenPort{{Port: 8080, Process: "api"}}},
		{NodeName: "node-b", Ports: []store.ListenPort{{Port: 8080, Process: "worker"}}},
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
	if len(diff.Changed) != 1 || len(diff.Added) != 1 || len(diff.Removed) != 0 {
		t.Fatalf("diff = %#v", diff)
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

func TestResolveEdges(t *testing.T) {
	t.Parallel()

	edges := ResolveEdges("live", []store.NodeSnapshot{
		{
			ID:          "snap-a",
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
			NodeName:    "node-b",
			TailscaleIP: "100.64.0.2",
			Ports:       []store.ListenPort{{Port: 8080, Process: "app"}},
		},
	})

	if len(edges) != 2 {
		t.Fatalf("len(edges) = %d, want 2", len(edges))
	}
}

func TestResolveEdgesAnnotatesServiceAndRuntimeMetadata(t *testing.T) {
	t.Parallel()

	edges := ResolveEdges("live", []store.NodeSnapshot{
		{
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
	})

	if len(edges) != 1 {
		t.Fatalf("len(edges) = %d, want 1", len(edges))
	}
	edge := edges[0]
	if !edge.Resolved || edge.ToNode != "unipilot-2" || edge.ToService != "unipilot_sse" {
		t.Fatalf("edge = %#v", edge)
	}
	if edge.ToContainer != "unipilot_sse.1.xyz" || edge.ToRuntimeNode != "unipilot-2" || edge.ToRuntimeContainer != "unipilot_sse.1.xyz" {
		t.Fatalf("edge runtime = %#v", edge)
	}
}

func TestResolveEdgesResolvesAliasesWithoutAdvertisedPorts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		targetHost string
		targetPort uint16
		targetNode core.NodeName
		ip         string
	}{
		{name: "hostname", targetHost: "unipilot-2.lab", targetPort: 3001, targetNode: "unipilot-2", ip: "100.74.111.75"},
		{name: "tailscale ip", targetHost: "100.104.22.121", targetPort: 5000, targetNode: "newsroom-api-1", ip: "100.104.22.121"},
		{name: "short t alias", targetHost: "warehouse-13-1-t", targetPort: 80, targetNode: "warehouse-13-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := store.NodeSnapshot{NodeName: tt.targetNode, TailscaleIP: tt.ip}
			if tt.name == "short t alias" {
				target.Ports = []store.ListenPort{{Port: 80, Process: "nginx"}}
			}
			edges := ResolveEdges("live", []store.NodeSnapshot{
				{
					NodeName: "source",
					Ports:    []store.ListenPort{{Port: 80, Process: "nginx"}},
					Forwards: []parser.ForwardAction{{
						Listener: parser.Listener{Port: 80},
						Target: parser.ForwardTarget{
							Raw:  tt.targetHost,
							Kind: parser.TargetKindAddress,
							Host: tt.targetHost,
							Port: tt.targetPort,
						},
					}},
				},
				target,
			})
			if len(edges) != 1 || edges[0].ToNode != tt.targetNode || edges[0].ToPort != tt.targetPort || !edges[0].Resolved {
				t.Fatalf("edges = %#v", edges)
			}
		})
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

	edges := resolveContainerEdges("live", snapshot)
	if len(edges) != 1 {
		t.Fatalf("len(resolveContainerEdges(...)) = %d, want 1; got %#v", len(edges), edges)
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

func TestBuildTopologyDataBuildsRouteCentricSnapshot(t *testing.T) {
	t.Parallel()

	data := BuildTopologyData([]store.NodeSnapshot{
		{
			NodeName:    "wwwill-1",
			TailscaleIP: "100.64.0.1",
			CollectedAt: core.NowTimestamp(),
			Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
			Forwards: []parser.ForwardAction{{
				Listener: parser.Listener{Port: 80},
				Target: parser.ForwardTarget{
					Raw:  "http://warehouse-13-1:3000",
					Kind: parser.TargetKindAddress,
					Host: "warehouse-13-1",
					Port: 3000,
				},
			}},
		},
		{
			NodeName:    "warehouse-13-1",
			TailscaleIP: "100.64.0.2",
			CollectedAt: core.NowTimestamp(),
			Containers: []store.Container{{
				ContainerName: "tailflow-ui",
				Image:         "tailflow/ui:latest",
				State:         "running",
				PublishedPorts: []store.ContainerPublishedPort{{
					HostPort:   3000,
					TargetPort: 3000,
					Proto:      "tcp",
					Source:     "container",
				}},
			}},
		},
	})

	if len(data.Routes) != 1 || !data.Routes[0].Resolved {
		t.Fatalf("data.Routes = %#v", data.Routes)
	}
	if len(data.RouteHops) != 3 {
		t.Fatalf("len(data.RouteHops) = %d, want 3", len(data.RouteHops))
	}
	if data.Summary.RouteCount != 1 || data.Summary.UnresolvedRouteCount != 0 {
		t.Fatalf("summary = %#v", data.Summary)
	}
}
