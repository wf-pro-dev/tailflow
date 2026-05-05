package runtime

import (
	"testing"

	"github.com/wf-pro-dev/tailflow/internal/collector"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/store"
)

func TestInventoryStateResetBuildsNormalizedNodeState(t *testing.T) {
	t.Parallel()

	inventory := NewInventoryState()
	snapshots := []store.NodeSnapshot{{
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		DNSName:     "node-a.tailnet",
		Ports:       []store.ListenPort{{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 123, Process: "nginx"}},
		Containers:  []store.Container{{ContainerID: "abc123", ContainerName: "api"}},
		Services:    []store.SwarmServicePort{{ServiceID: "svc-1", HostPort: 8080, TargetPort: 80, Proto: "tcp", Mode: "ingress"}},
		Forwards: []parser.ForwardAction{{
			Listener: parser.Listener{Addr: "0.0.0.0", Port: 80},
			Target:   parser.ForwardTarget{Raw: "http://100.64.0.2:8080", Kind: parser.TargetKindAddress, Host: "100.64.0.2", Port: 8080},
		}},
	}}
	statuses := []collector.NodeStatus{{NodeName: "node-a", Online: true, LastError: "degraded upstream"}}

	inventory.Reset(snapshots, statuses)

	node, ok := inventory.SnapshotNode("node-a")
	if !ok {
		t.Fatal("SnapshotNode(node-a) = missing, want present")
	}
	if node.Status.NodeName != "node-a" || !node.Status.Online {
		t.Fatalf("node.Status = %#v", node.Status)
	}
	if len(node.PortsByKey) != 1 {
		t.Fatalf("len(node.PortsByKey) = %d, want 1", len(node.PortsByKey))
	}
	if len(node.ContainersByID) != 1 {
		t.Fatalf("len(node.ContainersByID) = %d, want 1", len(node.ContainersByID))
	}
	if len(node.ServicePortsByKey) != 1 {
		t.Fatalf("len(node.ServicePortsByKey) = %d, want 1", len(node.ServicePortsByKey))
	}
	if len(node.ForwardsByKey) != 1 {
		t.Fatalf("len(node.ForwardsByKey) = %d, want 1", len(node.ForwardsByKey))
	}
}

func TestInventoryStatePortMutationsUpdateOneNode(t *testing.T) {
	t.Parallel()

	inventory := NewInventoryState()
	inventory.Reset([]store.NodeSnapshot{{NodeName: "node-a"}}, nil)

	portOne := store.ListenPort{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 123, Process: "nginx"}
	portTwo := store.ListenPort{Addr: "127.0.0.1", Port: 8080, Proto: "tcp", PID: 456, Process: "api"}

	inventory.UpsertNodePortWithDiff("node-a", portOne)
	inventory.UpsertNodePortWithDiff("node-a", portTwo)
	node, ok := inventory.SnapshotNode("node-a")
	if !ok {
		t.Fatal("SnapshotNode(node-a) = missing, want present")
	}
	if len(node.PortsByKey) != 2 {
		t.Fatalf("len(node.PortsByKey) = %d, want 2", len(node.PortsByKey))
	}

	inventory.RemoveNodePortWithDiff("node-a", portOne)
	node, _ = inventory.SnapshotNode("node-a")
	if len(node.PortsByKey) != 1 {
		t.Fatalf("len(node.PortsByKey) after remove = %d, want 1", len(node.PortsByKey))
	}

	inventory.ReplaceNodePortsWithDiff("node-a", []store.ListenPort{portOne})
	node, _ = inventory.SnapshotNode("node-a")
	if len(node.PortsByKey) != 1 {
		t.Fatalf("len(node.PortsByKey) after replace = %d, want 1", len(node.PortsByKey))
	}
	if _, ok := node.PortsByKey[listenPortKey(portOne)]; !ok {
		t.Fatalf("node.PortsByKey missing replaced port: %#v", node.PortsByKey)
	}
}

func TestInventoryStateReplaceNodeTopologyInputs(t *testing.T) {
	t.Parallel()

	inventory := NewInventoryState()
	inventory.Reset([]store.NodeSnapshot{{NodeName: "node-a"}}, nil)

	containers := []store.Container{{ContainerID: "abc123", ContainerName: "api"}}
	services := []store.SwarmServicePort{{ServiceID: "svc-1", ServiceName: "api", HostPort: 8080, TargetPort: 80, Proto: "tcp"}}
	forwards := []parser.ForwardAction{{
		Listener: parser.Listener{Port: 80},
		Target:   parser.ForwardTarget{Raw: "http://100.64.0.2:8080", Kind: parser.TargetKindAddress, Host: "100.64.0.2", Port: 8080},
	}}

	inventory.ReplaceNodeContainersWithDiff("node-a", containers)
	inventory.ReplaceNodeServicePortsWithDiff("node-a", services)
	inventory.ReplaceNodeForwardsWithDiff("node-a", forwards)

	node, ok := inventory.SnapshotNode("node-a")
	if !ok {
		t.Fatal("SnapshotNode(node-a) = missing, want present")
	}
	if len(node.ContainersByID) != 1 {
		t.Fatalf("len(node.ContainersByID) = %d, want 1", len(node.ContainersByID))
	}
	if len(node.ServicePortsByKey) != 1 {
		t.Fatalf("len(node.ServicePortsByKey) = %d, want 1", len(node.ServicePortsByKey))
	}
	if len(node.ForwardsByKey) != 1 {
		t.Fatalf("len(node.ForwardsByKey) = %d, want 1", len(node.ForwardsByKey))
	}
}

func TestInventoryStateApplySnapshotPreservesStatus(t *testing.T) {
	t.Parallel()

	inventory := NewInventoryState()
	inventory.Reset(nil, []collector.NodeStatus{{NodeName: "node-a", Online: true, LastSeenAt: core.NowTimestamp()}})

	inventory.ApplySnapshot(store.NodeSnapshot{
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
	})

	node, ok := inventory.SnapshotNode("node-a")
	if !ok {
		t.Fatal("SnapshotNode(node-a) = missing, want present")
	}
	if !node.Status.Online {
		t.Fatalf("node.Status.Online = %t, want true", node.Status.Online)
	}
	if len(node.PortsByKey) != 1 {
		t.Fatalf("len(node.PortsByKey) = %d, want 1", len(node.PortsByKey))
	}
}

func TestInventoryStateApplySnapshotWithDiffDetectsTopologyDomainChanges(t *testing.T) {
	t.Parallel()

	inventory := NewInventoryState()
	inventory.Reset([]store.NodeSnapshot{{
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		DNSName:     "node-a.tailnet",
		Ports:       []store.ListenPort{{Port: 80, Process: "nginx", Proto: "tcp"}},
		Containers: []store.Container{{
			ContainerID:   "abc123",
			ContainerName: "api",
			State:         "running",
		}},
		Services: []store.SwarmServicePort{{
			ServiceID:   "svc-1",
			ServiceName: "api",
			HostPort:    8080,
			TargetPort:  80,
			Proto:       "tcp",
		}},
		Forwards: []parser.ForwardAction{{
			Listener: parser.Listener{Port: 80},
			Target:   parser.ForwardTarget{Raw: "http://100.64.0.2:8080", Kind: parser.TargetKindAddress, Host: "100.64.0.2", Port: 8080},
		}},
	}}, []collector.NodeStatus{{NodeName: "node-a", Online: true}})

	delta := inventory.ApplySnapshotWithDiff(store.NodeSnapshot{
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.9",
		DNSName:     "node-a-alt.tailnet",
		Ports: []store.ListenPort{
			{Port: 81, Process: "nginx", Proto: "tcp"},
		},
		Containers: []store.Container{{
			ContainerID:   "abc123",
			ContainerName: "api",
			State:         "exited",
		}},
		Services: []store.SwarmServicePort{{
			ServiceID:   "svc-1",
			ServiceName: "api-v2",
			HostPort:    8080,
			TargetPort:  80,
			Proto:       "tcp",
		}},
		Forwards: []parser.ForwardAction{{
			Listener:  parser.Listener{Port: 81},
			Target:    parser.ForwardTarget{Raw: "http://100.64.0.2:9090", Kind: parser.TargetKindAddress, Host: "100.64.0.2", Port: 9090},
			Hostnames: []string{"app.tailflow.test"},
		}},
	})

	if !delta.TopologyMetadataChanged {
		t.Fatal("delta.TopologyMetadataChanged = false, want true")
	}
	if len(delta.PortsChanged) != 2 {
		t.Fatalf("len(delta.PortsChanged) = %d, want 2", len(delta.PortsChanged))
	}
	if len(delta.ContainersChanged) != 1 {
		t.Fatalf("len(delta.ContainersChanged) = %d, want 1", len(delta.ContainersChanged))
	}
	if len(delta.ServicePortsChanged) != 1 {
		t.Fatalf("len(delta.ServicePortsChanged) = %d, want 1", len(delta.ServicePortsChanged))
	}
	if len(delta.ForwardsChanged) != 2 {
		t.Fatalf("len(delta.ForwardsChanged) = %d, want 2", len(delta.ForwardsChanged))
	}
}

func TestInventoryStateSnapshotsRebuildTopologyInputs(t *testing.T) {
	t.Parallel()

	inventory := NewInventoryState()
	inventory.Reset([]store.NodeSnapshot{{
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

	snapshots := inventory.Snapshots()
	if len(snapshots) != 1 {
		t.Fatalf("len(snapshots) = %d, want 1", len(snapshots))
	}
	snapshot := snapshots[0]
	if len(snapshot.Ports) != 1 {
		t.Fatalf("len(snapshot.Ports) = %d, want 1", len(snapshot.Ports))
	}
	if len(snapshot.Containers) != 1 {
		t.Fatalf("len(snapshot.Containers) = %d, want 1", len(snapshot.Containers))
	}
	if len(snapshot.Services) != 1 {
		t.Fatalf("len(snapshot.Services) = %d, want 1", len(snapshot.Services))
	}
	if len(snapshot.Forwards) != 1 {
		t.Fatalf("len(snapshot.Forwards) = %d, want 1", len(snapshot.Forwards))
	}
}
