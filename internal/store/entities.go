package store

import (
	"context"

	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
)

// ListenPort describes one host-level listening socket observed on a node.
type ListenPort struct {
	ID      core.ID `json:"id,omitempty"`
	Addr    string  `json:"addr"`
	Port    uint16  `json:"port"`
	Proto   string  `json:"proto"`
	PID     int     `json:"pid"`
	Process string  `json:"process"`
}

// ContainerPort describes one published container port mapping.
type ContainerPort struct {
	ID            core.ID `json:"id,omitempty"`
	ContainerID   string  `json:"container_id"`
	ContainerName string  `json:"container_name"`
	HostPort      uint16  `json:"host_port"`
	ContainerPort uint16  `json:"container_port"`
	Proto         string  `json:"proto"`
}

// CollectionRun groups all snapshots collected in one cycle.
type CollectionRun struct {
	ID         core.ID        `json:"id" db:"id"`
	StartedAt  core.Timestamp `json:"started_at" db:"started_at"`
	FinishedAt core.Timestamp `json:"finished_at" db:"finished_at"`
	NodeCount  int            `json:"node_count" db:"node_count"`
	ErrorCount int            `json:"error_count" db:"error_count"`
}

// NodeSnapshot is the collected state for one node at one point in time.
type NodeSnapshot struct {
	ID          core.ID                `json:"id" db:"id"`
	RunID       core.ID                `json:"run_id" db:"run_id"`
	NodeName    core.NodeName          `json:"node_name" db:"node_name"`
	TailscaleIP string                 `json:"tailscale_ip" db:"tailscale_ip"`
	CollectedAt core.Timestamp         `json:"collected_at" db:"collected_at"`
	Ports       []ListenPort           `json:"ports"`
	Containers  []ContainerPort        `json:"containers"`
	Forwards    []parser.ForwardAction `json:"forwards"`
	Error       string                 `json:"error,omitempty" db:"error"`
}

// EdgeKind identifies the source of a topology edge.
type EdgeKind string

const (
	EdgeKindProxyPass        EdgeKind = "proxy_pass"
	EdgeKindContainerPublish EdgeKind = "container_publish"
	EdgeKindDirect           EdgeKind = "direct"
)

// TopologyEdge is one directed connection in the resolved graph.
type TopologyEdge struct {
	ID            core.ID       `json:"id" db:"id"`
	RunID         core.ID       `json:"run_id" db:"run_id"`
	FromNode      core.NodeName `json:"from_node" db:"from_node"`
	FromPort      uint16        `json:"from_port" db:"from_port"`
	FromProcess   string        `json:"from_process" db:"from_process"`
	FromContainer string        `json:"from_container" db:"from_container"`
	ToNode        core.NodeName `json:"to_node" db:"to_node"`
	ToPort        uint16        `json:"to_port" db:"to_port"`
	ToProcess     string        `json:"to_process" db:"to_process"`
	ToContainer   string        `json:"to_container" db:"to_container"`
	Kind          EdgeKind      `json:"kind" db:"kind"`
	Resolved      bool          `json:"resolved" db:"resolved"`
	RawUpstream   string        `json:"raw_upstream" db:"raw_upstream"`
}

// SnapshotStore provides CRUD and query operations for node snapshots.
type SnapshotStore interface {
	core.Repository[NodeSnapshot]
	ListByRun(ctx context.Context, runID core.ID) ([]NodeSnapshot, error)
	LatestByNode(ctx context.Context, nodeName core.NodeName) (NodeSnapshot, error)
}

// EdgeStore provides CRUD and query operations for topology edges.
type EdgeStore interface {
	core.Repository[TopologyEdge]
	ListByRun(ctx context.Context, runID core.ID) ([]TopologyEdge, error)
	LatestEdges(ctx context.Context) ([]TopologyEdge, error)
	ListUnresolved(ctx context.Context) ([]TopologyEdge, error)
}

// ProxyConfigStore provides CRUD and query operations for node proxy config input.
type ProxyConfigStore interface {
	core.Repository[parser.ProxyConfigInput]
	GetByNodeAndPath(ctx context.Context, nodeName core.NodeName, configPath string) (parser.ProxyConfigInput, error)
	ListByNode(ctx context.Context, nodeName core.NodeName) ([]parser.ProxyConfigInput, error)
	ListAll(ctx context.Context) ([]parser.ProxyConfigInput, error)
}

// RunStore provides CRUD and query operations for collection runs.
type RunStore interface {
	core.Repository[CollectionRun]
	Latest(ctx context.Context) (CollectionRun, error)
}
