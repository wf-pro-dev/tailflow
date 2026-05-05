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

// ContainerPublishedPort describes one host-visible port associated with a
// local Docker container. Source distinguishes direct container publishes from
// publishes inherited from a Swarm service definition.
type ContainerPublishedPort struct {
	HostPort   uint16 `json:"host_port"`
	TargetPort uint16 `json:"target_port"`
	Proto      string `json:"proto"`
	Source     string `json:"source"`
	Mode       string `json:"mode,omitempty"`
}

// Container describes one local Docker container observed on a node.
type Container struct {
	ID             core.ID                  `json:"id,omitempty"`
	ContainerID    string                   `json:"container_id"`
	ContainerName  string                   `json:"container_name"`
	Image          string                   `json:"image"`
	State          string                   `json:"state"`
	Status         string                   `json:"status"`
	ServiceName    string                   `json:"service_name,omitempty"`
	PublishedPorts []ContainerPublishedPort `json:"published_ports"`
}

// SwarmServicePort describes one published Docker Swarm service port mapping.
type SwarmServicePort struct {
	ID          core.ID `json:"id,omitempty"`
	ServiceID   string  `json:"service_id"`
	ServiceName string  `json:"service_name"`
	HostPort    uint16  `json:"host_port"`
	TargetPort  uint16  `json:"target_port"`
	Proto       string  `json:"proto"`
	Mode        string  `json:"mode,omitempty"`
}

type TopologyHealth string

const (
	TopologyHealthHealthy    TopologyHealth = "healthy"
	TopologyHealthDegraded   TopologyHealth = "degraded"
	TopologyHealthUnresolved TopologyHealth = "unresolved"
	TopologyHealthUnknown    TopologyHealth = "unknown"
)

// Service is the logical workload the user cares about.
type Service struct {
	ID          core.ID        `json:"id" db:"id"`
	Name        string         `json:"name" db:"name"`
	Kind        string         `json:"kind" db:"kind"`
	Role        string         `json:"role,omitempty" db:"role"`
	PrimaryNode core.NodeName  `json:"primary_node,omitempty" db:"primary_node"`
	RuntimeIDs  []core.ID      `json:"runtime_ids,omitempty"`
	ExposureIDs []core.ID      `json:"exposure_ids,omitempty"`
	Health      TopologyHealth `json:"health,omitempty" db:"health"`
	Tags        []string       `json:"tags,omitempty"`
	Description string         `json:"description,omitempty" db:"description"`
}

// Runtime is one concrete process or container realizing a service.
type Runtime struct {
	ID             core.ID        `json:"id" db:"id"`
	ServiceID      core.ID        `json:"service_id" db:"service_id"`
	NodeID         core.NodeName  `json:"node_id" db:"node_id"`
	RuntimeKind    string         `json:"runtime_kind" db:"runtime_kind"`
	RuntimeName    string         `json:"runtime_name" db:"runtime_name"`
	PID            int            `json:"pid,omitempty" db:"pid"`
	ContainerID    string         `json:"container_id,omitempty" db:"container_id"`
	Image          string         `json:"image,omitempty" db:"image"`
	State          string         `json:"state,omitempty" db:"state"`
	Ports          []uint16       `json:"ports,omitempty"`
	NetworkNames   []string       `json:"network_names,omitempty"`
	NetworkAliases []string       `json:"network_aliases,omitempty"`
	Health         TopologyHealth `json:"health,omitempty" db:"health"`
	CollectedAt    core.Timestamp `json:"collected_at,omitempty" db:"collected_at"`
}

// Exposure is one discovered way a service can be reached from outside its runtime boundary.
type Exposure struct {
	ID               core.ID        `json:"id" db:"id"`
	ServiceID        core.ID        `json:"service_id" db:"service_id"`
	RuntimeID        core.ID        `json:"runtime_id,omitempty" db:"runtime_id"`
	NodeID           core.NodeName  `json:"node_id,omitempty" db:"node_id"`
	Kind             string         `json:"kind" db:"kind"`
	Protocol         string         `json:"protocol,omitempty" db:"protocol"`
	Hostname         string         `json:"hostname,omitempty" db:"hostname"`
	PathPrefix       string         `json:"path_prefix,omitempty" db:"path_prefix"`
	Port             uint16         `json:"port,omitempty" db:"port"`
	URL              string         `json:"url,omitempty" db:"url"`
	IsPrimary        bool           `json:"is_primary" db:"is_primary"`
	Visibility       string         `json:"visibility,omitempty" db:"visibility"`
	Source           string         `json:"source,omitempty" db:"source"`
	GatewayServiceID core.ID        `json:"gateway_service_id,omitempty" db:"gateway_service_id"`
	Health           TopologyHealth `json:"health,omitempty" db:"health"`
	ResolutionStatus string         `json:"resolution_status,omitempty" db:"resolution_status"`
}

// Route is the user-meaningful request path from one reachable endpoint to another service.
type Route struct {
	ID               core.ID        `json:"id" db:"id"`
	Kind             string         `json:"kind" db:"kind"`
	SourceServiceID  core.ID        `json:"source_service_id,omitempty" db:"source_service_id"`
	SourceExposureID core.ID        `json:"source_exposure_id,omitempty" db:"source_exposure_id"`
	TargetServiceID  core.ID        `json:"target_service_id,omitempty" db:"target_service_id"`
	TargetRuntimeID  core.ID        `json:"target_runtime_id,omitempty" db:"target_runtime_id"`
	DisplayName      string         `json:"display_name" db:"display_name"`
	Resolved         bool           `json:"resolved" db:"resolved"`
	Health           TopologyHealth `json:"health,omitempty" db:"health"`
	HopIDs           []core.ID      `json:"hop_ids,omitempty"`
	Hostnames        []string       `json:"hostnames,omitempty"`
	Input            string         `json:"input,omitempty" db:"input"`
}

// RouteHop is one hop in a route.
type RouteHop struct {
	ID         core.ID        `json:"id" db:"id"`
	RouteID    core.ID        `json:"route_id" db:"route_id"`
	Order      int            `json:"order" db:"order"`
	Kind       string         `json:"kind" db:"kind"`
	From       string         `json:"from,omitempty" db:"from_ref"`
	To         string         `json:"to,omitempty" db:"to_ref"`
	Resolved   bool           `json:"resolved" db:"resolved"`
	Health     TopologyHealth `json:"health,omitempty" db:"health"`
	EvidenceID core.ID        `json:"evidence_id,omitempty" db:"evidence_id"`
}

// Evidence explains why a route resolved to a given target.
type Evidence struct {
	ID         core.ID  `json:"id" db:"id"`
	MatchedBy  string   `json:"matched_by" db:"matched_by"`
	Confidence string   `json:"confidence" db:"confidence"`
	Reason     string   `json:"reason,omitempty" db:"reason"`
	RawValue   string   `json:"raw_value,omitempty" db:"raw_value"`
	Warnings   []string `json:"warnings,omitempty"`
}

// TopologySummary carries high-level counts for one topology snapshot.
type TopologySummary struct {
	NodeCount            int `json:"node_count"`
	ServiceCount         int `json:"service_count"`
	RuntimeCount         int `json:"runtime_count"`
	ExposureCount        int `json:"exposure_count"`
	RouteCount           int `json:"route_count"`
	UnresolvedRouteCount int `json:"unresolved_route_count"`
}

// NodeSnapshot is the collected state for one node at one point in time.
type NodeSnapshot struct {
	ID          core.ID                `json:"id" db:"id"`
	RunID       core.ID                `json:"run_id" db:"run_id"`
	NodeName    core.NodeName          `json:"node_name" db:"node_name"`
	TailscaleIP string                 `json:"tailscale_ip" db:"tailscale_ip"`
	DNSName     string                 `json:"dns_name" db:"dns_name"`
	CollectedAt core.Timestamp         `json:"collected_at" db:"collected_at"`
	Ports       []ListenPort           `json:"ports"`
	Containers  []Container            `json:"containers"`
	Services    []SwarmServicePort     `json:"services"`
	Forwards    []parser.ForwardAction `json:"forwards"`
	Error       string                 `json:"error,omitempty" db:"error"`
}

// EdgeKind identifies the source of a topology edge.
type EdgeKind string

const (
	EdgeKindProxyPass        EdgeKind = "proxy_pass"
	EdgeKindContainerPublish EdgeKind = "container_publish"
	EdgeKindServicePublish   EdgeKind = "service_publish"
	EdgeKindDirect           EdgeKind = "direct"
)

// TopologyEdge is one directed connection in the resolved graph.
type TopologyEdge struct {
	ID                 core.ID       `json:"id" db:"id"`
	RunID              core.ID       `json:"run_id" db:"run_id"`
	FromNode           core.NodeName `json:"from_node" db:"from_node"`
	FromPort           uint16        `json:"from_port" db:"from_port"`
	FromProcess        string        `json:"from_process" db:"from_process"`
	FromContainer      string        `json:"from_container" db:"from_container"`
	ToNode             core.NodeName `json:"to_node" db:"to_node"`
	ToPort             uint16        `json:"to_port" db:"to_port"`
	ToProcess          string        `json:"to_process" db:"to_process"`
	ToContainer        string        `json:"to_container" db:"to_container"`
	ToService          string        `json:"to_service" db:"to_service"`
	ToRuntimeNode      core.NodeName `json:"to_runtime_node,omitempty" db:"to_runtime_node"`
	ToRuntimeContainer string        `json:"to_runtime_container,omitempty" db:"to_runtime_container"`
	Kind               EdgeKind      `json:"kind" db:"kind"`
	Resolved           bool          `json:"resolved" db:"resolved"`
	RawUpstream        string        `json:"raw_upstream" db:"raw_upstream"`
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
