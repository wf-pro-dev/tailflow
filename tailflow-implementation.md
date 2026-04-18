# Tailflow Backend — Implementation Guide

---

## Naming Conventions

Established upfront. Every type, function, and package follows these rules without exception.

**Packages** — single lowercase noun. `collector`, `resolver`, `store`, `api`, `parser`, `scheduler`.

**Types** — `<Entity>` for domain nouns, `<Entity>Request` / `<Entity>Response` for API boundaries, `<Entity>Event` for anything that flows through a channel or SSE stream.

**Functions** — `Get<Entity>` for single fetch, `List<Entity>` for collections, `Watch<Entity>` for streaming, `Resolve<Entity>` for derivation logic, `Apply<Event>` for state mutations.

**Events** — `<domain>.<action>` in snake case for SSE event names (`node.connected`, `port.bound`, `edge.resolved`). Go type names use `<Domain><Action>Event` (`NodeConnectedEvent`, `PortBoundEvent`).

**IDs** — all stored entities use `ULID` (lexicographically sortable, embeds timestamp). Never UUID v4.

---

## Shared Package — `internal/core`

Identified before the entities to avoid duplication. Any type or function referenced by two or more entities lives here.

### Types

```go
// A ULID-based identifier used on every stored entity
type ID = string

// NodeName is the Tailscale peer name — the stable key across all entities
type NodeName = string

// Timestamp wraps time.Time with consistent JSON marshalling (RFC3339Nano)
type Timestamp = time.Time

// Result carries a value and an error together —
// used for fan-out operations that return per-node outcomes
type Result[T any] struct {
    Node  NodeName
    Value T
    Err   error
}

// Event is the envelope for all SSE events emitted by tailflow's own API.
// Mirrors the tailkitd SSE envelope so clients have one mental model.
type Event[T any] struct {
    Name string    `json:"event"`
    TS   Timestamp `json:"ts"`
    Data T         `json:"data"`
}
```

### Interfaces

```go
// Watcher is implemented by any component that produces a stream of events.
// The api layer calls Watch() and forwards to SSE clients.
type Watcher[T any] interface {
    Watch(ctx context.Context) (<-chan T, error)
}

// Repository is the base interface every store entity implements.
type Repository[T any] interface {
    Get(ctx context.Context, id ID) (T, error)
    List(ctx context.Context, filter Filter) ([]T, error)
    Save(ctx context.Context, entity T) error
    Delete(ctx context.Context, id ID) error
}

// Filter is a generic query parameter bag passed to List().
// Entity-specific filters embed this.
type Filter struct {
    NodeName  NodeName
    Since     *Timestamp
    Limit     int
}
```

### Functions

```go
// NewID returns a new ULID string
func NewID() ID

// Must panics if err != nil — used in init() and test setup only
func Must[T any](v T, err error) T

// MergeErrors collects per-node errors from a fan-out into one error
// if any node failed. Returns nil if all succeeded.
func MergeErrors(errs map[NodeName]error) error

// BroadcastEvent writes an Event to all registered SSE subscribers.
// Lives here because both the collector and the resolver emit events
// consumed by the same API layer.
func BroadcastEvent[T any](bus *EventBus, name string, data T)
```

### `EventBus`

The central pub/sub primitive. Every entity that produces real-time data publishes to the bus. The API layer subscribes and forwards to SSE clients. One bus, one mental model.

```go
type Topic string

const (
    TopicNode     Topic = "node"
    TopicSnapshot Topic = "snapshot"
    TopicEdge     Topic = "edge"
    TopicPort     Topic = "port"
    TopicProxy    Topic = "proxy"
)

type EventBus struct { /* fan-out to N subscribers per topic */ }

func NewEventBus() *EventBus
func (b *EventBus) Publish(topic Topic, event any)
func (b *EventBus) Subscribe(ctx context.Context, topic Topic) <-chan any
```

Similar systems: this is the same pattern as Watermill (Go), EventEmitter (Node.js), and Guava EventBus (Java). For tailflow's scale (tens of nodes, not millions of events) a simple in-process bus with a fan-out goroutine per topic is sufficient — no Kafka, no Redis.

**Library:** `github.com/mustafaturan/bus/v3` or a hand-rolled 50-line implementation. Hand-rolled is preferable here — the surface area is tiny and adds zero dependencies.

---

## Entity 1 — Collector

**What it does:** fans out to all online tailnet nodes via tailkit, assembles a `NodeSnapshot` per node, publishes results to the event bus. The heartbeat of tailflow.

### System Design Concepts
- **Fan-out / scatter-gather** — concurrent requests to N nodes, results collected with a deadline
- **Circuit breaker** — a node that fails repeatedly is marked degraded and skipped until it recovers, preventing one bad node from slowing every collection cycle
- **Incremental update** — the streaming `StreamPorts` subscription patches the stored snapshot between full collection cycles rather than waiting for the next poll

### Similar Systems
- Prometheus scrape manager (fan-out to N targets with per-target timeouts)
- Consul health checker (periodic checks with circuit-break on repeated failure)

### Libraries
- `golang.org/x/sync/errgroup` — bounded fan-out with context propagation
- `github.com/sony/gobreaker` — circuit breaker
- `github.com/wf-pro-dev/tailkit` — node clients

### Types

```go
// NodeSnapshot is the complete collected state for one node at one point in time.
// Stored in the database and used as input to the Resolver.
type NodeSnapshot struct {
    ID          core.ID          `json:"id"          db:"id"`
    RunID       core.ID          `json:"run_id"      db:"run_id"`
    NodeName    core.NodeName    `json:"node_name"   db:"node_name"`
    TailscaleIP string           `json:"tailscale_ip" db:"tailscale_ip"`
    CollectedAt core.Timestamp   `json:"collected_at" db:"collected_at"`
    Ports       []ListenPort     `json:"ports"`
    Containers  []ContainerPort  `json:"containers"`
    ProxyRules  []ProxyRule      `json:"proxy_rules"`
    Error       string           `json:"error,omitempty" db:"error"`
}

// CollectionRun groups all snapshots collected in one cycle.
type CollectionRun struct {
    ID          core.ID        `json:"id"          db:"id"`
    StartedAt   core.Timestamp `json:"started_at"  db:"started_at"`
    FinishedAt  core.Timestamp `json:"finished_at" db:"finished_at"`
    NodeCount   int            `json:"node_count"  db:"node_count"`
    ErrorCount  int            `json:"error_count" db:"error_count"`
}

// NodeStatus tracks liveness per node between collection cycles.
type NodeStatus struct {
    NodeName    core.NodeName  `json:"node_name"`
    Online      bool           `json:"online"`
    Degraded    bool           `json:"degraded"`   // circuit breaker open
    LastSeenAt  core.Timestamp `json:"last_seen_at"`
    LastError   string         `json:"last_error,omitempty"`
}

// SnapshotEvent is published to TopicSnapshot after each successful node collection.
type SnapshotEvent struct {
    RunID    core.ID       `json:"run_id"`
    Snapshot NodeSnapshot  `json:"snapshot"`
}

// NodeStatusEvent is published to TopicNode when a node's online/degraded state changes.
type NodeStatusEvent struct {
    Previous NodeStatus `json:"previous"`
    Current  NodeStatus `json:"current"`
}
```

### Functions

```go
type Collector struct {
    srv      *tailkit.Server
    store    SnapshotStore
    bus      *core.EventBus
    parsers  parser.Registry
    breakers map[core.NodeName]*gobreaker.CircuitBreaker
}

func NewCollector(srv *tailkit.Server, store SnapshotStore, bus *core.EventBus, parsers parser.Registry) *Collector

// RunOnce executes one full collection cycle across all online peers.
// Called by the Scheduler on each tick and available as an API trigger.
func (c *Collector) RunOnce(ctx context.Context) (CollectionRun, error)

// WatchNode opens a persistent StreamPorts subscription to one node.
// Called at startup for each online node. Patches the stored snapshot
// on each port change event without waiting for the next full cycle.
func (c *Collector) WatchNode(ctx context.Context, nodeName core.NodeName) error

// collectNode collects the full snapshot for a single node.
// Called concurrently from RunOnce via errgroup.
func (c *Collector) collectNode(ctx context.Context, peer types.Peer, runID core.ID) (NodeSnapshot, error)

// GetStatus returns the current NodeStatus for all known nodes.
func (c *Collector) GetStatus(ctx context.Context) ([]NodeStatus, error)
```

---

## Entity 2 — Parser

**What it does:** takes a raw proxy config file (string) and a declared kind (`nginx` | `caddy`) and returns `[]ProxyRule`. Stateless and pure — no I/O, no storage. The config file content is fetched by the Collector via `Files().Read()` before calling the parser.

### System Design Concepts
- **Strategy pattern** — `ProxyParser` interface with `NginxParser` and `CaddyParser` implementations selected at runtime by kind
- **Registry** — a map of kind → parser, consulted by the Collector. Adding a new proxy type means registering a new parser, nothing else changes
- **Fault isolation** — a parse error on one node's config never affects other nodes' snapshots

### Similar Systems
- Prometheus `scrape_config` target parsers
- Traefik's provider system (pluggable config sources)
- Telegraf input plugins

### Libraries
- `github.com/tufanbarisyildirim/gonginx` — nginx config parser
- Standard library `encoding/json` for Caddy JSON config
- `github.com/nicholasgasior/gsfmt` for Caddyfile (text format)

### Types

```go
// ProxyRule is one routing decision extracted from a proxy config.
type ProxyRule struct {
    ListenPort uint16 `json:"listen_port"`
    ServerName string `json:"server_name"` // vhost, empty = catch-all
    Upstream   string `json:"upstream"`    // "localhost:3000", "10.0.0.5:8080"
    Proto      string `json:"proto"`       // "http" | "https" | "grpc"
}

// ProxyConfigInput is the user-declared proxy config for one node.
// Stored in the database, set via the UI.
type ProxyConfigInput struct {
    ID         core.ID        `json:"id"          db:"id"`
    NodeName   core.NodeName  `json:"node_name"   db:"node_name"`
    Kind       string         `json:"kind"`       // "nginx" | "caddy"
    ConfigPath string         `json:"config_path" db:"config_path"`
    UpdatedAt  core.Timestamp `json:"updated_at"  db:"updated_at"`
}

// ParseResult carries the output of one parse attempt.
type ParseResult struct {
    Rules  []ProxyRule `json:"rules"`
    Errors []string    `json:"errors,omitempty"` // non-fatal parse warnings
}
```

### Interfaces and Functions

```go
// ProxyParser is the strategy interface. One implementation per proxy kind.
type ProxyParser interface {
    Kind() string
    Parse(configContent string) (ParseResult, error)
}

// Registry maps kind strings to ProxyParser implementations.
type Registry map[string]ProxyParser

func NewRegistry() Registry
func (r Registry) Register(p ProxyParser)
func (r Registry) Parse(kind string, content string) (ParseResult, error)

// NginxParser implements ProxyParser for nginx configs.
// Handles: server blocks, proxy_pass directives, upstream groups,
// include directives (resolved by the Collector before calling Parse).
type NginxParser struct{}
func (p NginxParser) Kind() string
func (p NginxParser) Parse(content string) (ParseResult, error)

// CaddyParser implements ProxyParser for Caddy configs.
// Handles both Caddyfile and JSON API format (detected by first byte).
type CaddyParser struct{}
func (p CaddyParser) Kind() string
func (p CaddyParser) Parse(content string) (ParseResult, error)
```

---

## Entity 3 — Resolver

**What it does:** takes all `NodeSnapshot`s from one `CollectionRun` and produces `[]TopologyEdge` by joining ports ↔ containers ↔ proxy rules and matching upstream strings against known node IPs. Publishes edges to the event bus.

### System Design Concepts
- **Graph construction** — nodes and edges, adjacency by IP/port
- **Multi-index join** — upstreams matched against three indexes simultaneously: Tailscale IPs, LAN interface IPs, container names
- **Unresolved edge preservation** — edges that don't match any known node are stored as `resolved: false`, never silently dropped
- **Incremental re-resolution** — when a `SnapshotEvent` arrives from the bus, only the edges involving that node are recomputed, not the full graph

### Similar Systems
- Cilium's endpoint map (IP → workload resolution)
- Envoy's EDS (endpoint discovery, resolving upstreams to real addresses)
- AWS VPC Reachability Analyzer

### Libraries
- `github.com/dominikbraun/graph` — typed directed graph with traversal
- Standard library `net` — IP parsing and CIDR matching

### Types

```go
// TopologyEdge is one directed connection in the resolved graph.
type TopologyEdge struct {
    ID            core.ID       `json:"id"             db:"id"`
    RunID         core.ID       `json:"run_id"         db:"run_id"`
    FromNode      core.NodeName `json:"from_node"      db:"from_node"`
    FromPort      uint16        `json:"from_port"      db:"from_port"`
    FromProcess   string        `json:"from_process"   db:"from_process"`
    FromContainer string        `json:"from_container" db:"from_container"`
    ToNode        core.NodeName `json:"to_node"        db:"to_node"`  // empty = unresolved
    ToPort        uint16        `json:"to_port"        db:"to_port"`
    ToProcess     string        `json:"to_process"     db:"to_process"`
    ToContainer   string        `json:"to_container"   db:"to_container"`
    Kind          EdgeKind      `json:"kind"           db:"kind"`
    Resolved      bool          `json:"resolved"       db:"resolved"`
    RawUpstream   string        `json:"raw_upstream"   db:"raw_upstream"`
}

type EdgeKind string
const (
    EdgeKindProxyPass        EdgeKind = "proxy_pass"
    EdgeKindContainerPublish EdgeKind = "container_publish"
    EdgeKindDirect           EdgeKind = "direct"
)

// NodeIndex is the lookup table built from all snapshots in one run.
// Keyed by every IP the resolver might encounter in an upstream string.
type NodeIndex struct {
    ByTailscaleIP map[string]core.NodeName
    ByLANIP       map[string]core.NodeName
    ByPort        map[uint16][]IndexedPort
}

type IndexedPort struct {
    NodeName      core.NodeName
    Port          uint16
    Process       string
    ContainerName string
}

// EdgeEvent is published to TopicEdge after resolution.
type EdgeEvent struct {
    RunID core.ID        `json:"run_id"`
    Edges []TopologyEdge `json:"edges"`
    Diff  EdgeDiff       `json:"diff"`
}

// EdgeDiff describes what changed between the previous and current run.
type EdgeDiff struct {
    Added   []TopologyEdge `json:"added"`
    Removed []TopologyEdge `json:"removed"`
    Changed []TopologyEdge `json:"changed"`
}
```

### Functions

```go
type Resolver struct {
    store  EdgeStore
    bus    *core.EventBus
}

func NewResolver(store EdgeStore, bus *core.EventBus) *Resolver

// ResolveRun builds the full edge set for a completed CollectionRun.
// Called by the Scheduler after RunOnce finishes.
func (r *Resolver) ResolveRun(ctx context.Context, run CollectionRun, snapshots []NodeSnapshot) ([]TopologyEdge, error)

// ResolveSnapshot recomputes only the edges involving one node.
// Called when a SnapshotEvent arrives between full cycles (port stream patch).
func (r *Resolver) ResolveSnapshot(ctx context.Context, snapshot NodeSnapshot) ([]TopologyEdge, error)

// BuildIndex constructs the NodeIndex from a set of snapshots.
// Pure function — no I/O.
func BuildIndex(snapshots []NodeSnapshot) NodeIndex

// ResolveUpstream attempts to match one upstream string against the index.
// Returns (nodeName, port, true) on success, ("", 0, false) on failure.
func ResolveUpstream(upstream string, index NodeIndex) (core.NodeName, uint16, bool)

// DiffEdges compares two edge sets and returns what was added, removed, or changed.
func DiffEdges(previous, current []TopologyEdge) EdgeDiff
```

---

## Entity 4 — Store

**What it does:** persistence layer. GORM models over SQLite. One repository per aggregate. No business logic — raw CRUD and queries only.

### System Design Concepts
- **Repository pattern** — each entity has its own store interface, implementations are swappable (SQLite today, Postgres later)
- **Optimistic reads** — queries always return the latest run's data by default, historical runs accessible by `run_id`
- **JSON columns for nested data** — `ports`, `containers`, `proxy_rules` stored as JSON in `node_snapshots.raw` alongside normalized columns for querying

### Similar Systems
- Grafana's store layer (SQLite → Postgres migration path)
- Loki's chunk store (time-series with latest-first access pattern)

### Libraries
- `gorm.io/gorm` + `gorm.io/driver/sqlite`
- `github.com/oklog/ulid/v2` — ULID generation for `core.NewID()`

### GORM Models

```go
type CollectionRunModel struct {
    ID          string    `gorm:"primaryKey"`
    StartedAt   time.Time `gorm:"index"`
    FinishedAt  time.Time
    NodeCount   int
    ErrorCount  int
}

type NodeSnapshotModel struct {
    ID          string    `gorm:"primaryKey"`
    RunID       string    `gorm:"index;not null"`
    NodeName    string    `gorm:"index;not null"`
    TailscaleIP string
    CollectedAt time.Time `gorm:"index"`
    RawJSON     string    `gorm:"column:raw_json;type:text"`
    Error       string
}

type ListenPortModel struct {
    ID         string `gorm:"primaryKey"`
    SnapshotID string `gorm:"index;not null"`
    NodeName   string `gorm:"index;not null"`
    Addr       string
    Port       uint16 `gorm:"index"`
    Proto      string
    PID        int
    Process    string
}

type ContainerPortModel struct {
    ID            string `gorm:"primaryKey"`
    SnapshotID    string `gorm:"index;not null"`
    NodeName      string `gorm:"index;not null"`
    ContainerID   string
    ContainerName string `gorm:"index"`
    HostPort      uint16 `gorm:"index"`
    ContainerPort uint16
    Proto         string
}

type TopologyEdgeModel struct {
    ID            string   `gorm:"primaryKey"`
    RunID         string   `gorm:"index;not null"`
    FromNode      string   `gorm:"index"`
    FromPort      uint16
    FromProcess   string
    FromContainer string
    ToNode        string   `gorm:"index"`
    ToPort        uint16
    ToProcess     string
    ToContainer   string
    Kind          string
    Resolved      bool     `gorm:"index"`
    RawUpstream   string
}

type ProxyConfigInputModel struct {
    ID         string    `gorm:"primaryKey"`
    NodeName   string    `gorm:"uniqueIndex;not null"`
    Kind       string
    ConfigPath string
    UpdatedAt  time.Time
}
```

### Store Interfaces

```go
type SnapshotStore interface {
    core.Repository[NodeSnapshot]
    ListByRun(ctx context.Context, runID core.ID) ([]NodeSnapshot, error)
    LatestByNode(ctx context.Context, nodeName core.NodeName) (NodeSnapshot, error)
}

type EdgeStore interface {
    core.Repository[TopologyEdge]
    ListByRun(ctx context.Context, runID core.ID) ([]TopologyEdge, error)
    LatestEdges(ctx context.Context) ([]TopologyEdge, error)
    ListUnresolved(ctx context.Context) ([]TopologyEdge, error)
}

type ProxyConfigStore interface {
    core.Repository[ProxyConfigInput]
    GetByNode(ctx context.Context, nodeName core.NodeName) (ProxyConfigInput, error)
    ListAll(ctx context.Context) ([]ProxyConfigInput, error)
}

type RunStore interface {
    core.Repository[CollectionRun]
    Latest(ctx context.Context) (CollectionRun, error)
}
```

---

## Entity 5 — Scheduler

**What it does:** owns the collection clock. Fires `Collector.RunOnce` on a configurable interval, then hands the result to `Resolver.ResolveRun`. Also manages the persistent `WatchNode` goroutines for streaming port updates.

### System Design Concepts
- **Supervisor tree** — each `WatchNode` goroutine is supervised; if it exits unexpectedly it is restarted with exponential backoff
- **Jitter** — collection interval has a small random jitter to prevent thundering herd if multiple tailflow instances ever run
- **Trigger channel** — exposes a channel the API layer can send to, causing an immediate out-of-cycle collection

### Similar Systems
- Kubernetes controller reconcile loop (periodic + event-triggered)
- Prometheus scrape manager (interval + jitter + per-target goroutine)

### Libraries
- `golang.org/x/sync/errgroup`
- Standard library `time` — ticker with jitter
- `github.com/cenkalti/backoff/v4` — exponential backoff for watcher restarts

### Types and Functions

```go
type SchedulerConfig struct {
    CollectInterval time.Duration // default: 30s
    CollectJitter   time.Duration // default: 5s
    NodeTimeout     time.Duration // per-node context timeout, default: 10s
}

type Scheduler struct {
    config    SchedulerConfig
    collector *collector.Collector
    resolver  *resolver.Resolver
    trigger   chan struct{}
}

func NewScheduler(cfg SchedulerConfig, c *collector.Collector, r *resolver.Resolver) *Scheduler

// Run starts the collection loop and all WatchNode goroutines.
// Blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) error

// Trigger causes an immediate collection cycle outside the normal interval.
// Non-blocking — if a cycle is already running, this is a no-op.
func (s *Scheduler) Trigger()

// superviseWatcher starts WatchNode for nodeName and restarts it on failure
// using exponential backoff. Exits when ctx is cancelled.
func (s *Scheduler) superviseWatcher(ctx context.Context, nodeName core.NodeName)
```

---

## Entity 6 — API

**What it does:** HTTP server exposing REST endpoints for the UI and SSE streams for real-time updates. Reads from Store for historical data, subscribes to the EventBus for live data. Writes `ProxyConfigInput` to Store.

### System Design Concepts
- **CQRS at the handler level** — reads go to Store, writes go to Store + trigger Collector
- **SSE fan-out** — same `SSEWriter` pattern from tailkitd, reused here for the tailflow API. The `core.EventBus` is the source; the handler subscribes and forwards
- **Consistent error shape** — every error response is `{"error": "...", "hint": "..."}`, matching tailkitd's convention

### Similar Systems
- Grafana HTTP API (mix of REST + SSE for alerting streams)
- Portainer API (Docker management with SSE for container events)

### Libraries
- Standard library `net/http` — no framework; routes registered with `http.ServeMux` (Go 1.22 pattern matching)
- `github.com/rs/cors` — CORS for the UI
- `encoding/json` — standard JSON
- Shared `internal/sse` package from tailkitd reimplemented in tailflow (same `SSEWriter`, same `StreamHandler`)

### Route Table

```
# Nodes
GET  /api/v1/nodes                         ListNodes
GET  /api/v1/nodes/{name}                  GetNode
GET  /api/v1/nodes/{name}/snapshot         GetLatestSnapshot
GET  /api/v1/nodes/stream                  WatchNodes          (SSE)

# Topology
GET  /api/v1/topology                      GetTopology
GET  /api/v1/topology/edges                ListEdges
GET  /api/v1/topology/edges/unresolved     ListUnresolvedEdges
GET  /api/v1/topology/stream               WatchTopology       (SSE)

# Collection runs
GET  /api/v1/runs                          ListRuns
GET  /api/v1/runs/{id}                     GetRun
GET  /api/v1/runs/{id}/snapshots           ListRunSnapshots
POST /api/v1/runs                          TriggerRun

# Proxy config
GET  /api/v1/proxy-configs                 ListProxyConfigs
GET  /api/v1/proxy-configs/{node}          GetProxyConfig
PUT  /api/v1/proxy-configs/{node}          SetProxyConfig
DELETE /api/v1/proxy-configs/{node}        DeleteProxyConfig

# Health
GET  /api/v1/health                        Health
```

### Request / Response Types

```go
// GET /api/v1/nodes
type ListNodesResponse struct {
    Nodes []NodeResponse `json:"nodes"`
}

type NodeResponse struct {
    Name        core.NodeName    `json:"name"`
    TailscaleIP string           `json:"tailscale_ip"`
    Online      bool             `json:"online"`
    Degraded    bool             `json:"degraded"`
    LastSeenAt  core.Timestamp   `json:"last_seen_at"`
    Snapshot    *SnapshotSummary `json:"snapshot,omitempty"`
}

type SnapshotSummary struct {
    CollectedAt  core.Timestamp `json:"collected_at"`
    PortCount    int            `json:"port_count"`
    ContainerCount int          `json:"container_count"`
    ProxyRuleCount int          `json:"proxy_rule_count"`
}

// GET /api/v1/topology
type TopologyResponse struct {
    RunID     core.ID          `json:"run_id"`
    Nodes     []TopologyNode   `json:"nodes"`
    Edges     []TopologyEdge   `json:"edges"`
    UpdatedAt core.Timestamp   `json:"updated_at"`
}

type TopologyNode struct {
    Name        core.NodeName  `json:"name"`
    TailscaleIP string         `json:"tailscale_ip"`
    Online      bool           `json:"online"`
    Ports       []ListenPort   `json:"ports"`
    Containers  []ContainerPort `json:"containers"`
}

// PUT /api/v1/proxy-configs/{node}
type SetProxyConfigRequest struct {
    Kind       string `json:"kind"`        // "nginx" | "caddy"
    ConfigPath string `json:"config_path"`
}

type SetProxyConfigResponse struct {
    Config  ProxyConfigInput `json:"config"`
    Preview ParseResult      `json:"preview"` // parsed immediately on set
}

// POST /api/v1/runs
type TriggerRunResponse struct {
    RunID     core.ID        `json:"run_id"`
    StartedAt core.Timestamp `json:"started_at"`
}

// GET /api/v1/health
type HealthResponse struct {
    Status     string         `json:"status"`  // "ok" | "degraded"
    NodeCount  int            `json:"node_count"`
    LastRunAt  core.Timestamp `json:"last_run_at"`
    TailnetIP  string         `json:"tailnet_ip"`
}
```

### SSE Event Catalogue (tailflow API)

All events use `core.Event[T]` as the envelope.

```
Topic: node
  node.connected        NodeStatusEvent   — node came online
  node.disconnected     NodeStatusEvent   — node went offline
  node.degraded         NodeStatusEvent   — circuit breaker opened
  node.snapshot_updated SnapshotEvent     — new snapshot stored for node

Topic: topology
  topology.edge_added   EdgeEvent         — new edge resolved
  topology.edge_removed EdgeEvent         — edge no longer present
  topology.edge_changed EdgeEvent         — edge endpoint changed
  topology.run_completed CollectionRun    — full cycle finished

Topic: port (forwarded from tailkitd via WatchNode)
  port.bound            PortBoundEvent    — port appeared on a node
  port.released         PortReleasedEvent — port disappeared from a node
```

```go
// WatchNodes SSE — subscribes to TopicNode
// WatchTopology SSE — subscribes to TopicEdge + TopicSnapshot

type PortBoundEvent struct {
    NodeName core.NodeName    `json:"node_name"`
    Port     types.ListenPort `json:"port"`
}

type PortReleasedEvent struct {
    NodeName core.NodeName    `json:"node_name"`
    Port     types.ListenPort `json:"port"`
}
```

---

## Dependency Graph

```
core
  └── (no dependencies on other internal packages)

store
  └── core

parser
  └── core

collector
  ├── core
  ├── store
  └── parser

resolver
  ├── core
  └── store

scheduler
  ├── collector
  └── resolver

api
  ├── core
  ├── store
  ├── collector  (Trigger only)
  └── scheduler  (Trigger only)
```

No cycles. `core` is the only package every other package imports.

---

## Directory Layout

```
tailflow/
  cmd/
    tailflow/
      main.go          — wire everything together, start tsnet + HTTP
  internal/
    core/              — ID, Event, EventBus, Watcher, Repository, Filter
    collector/         — Collector, NodeSnapshot, CollectionRun, NodeStatus
    parser/            — ProxyParser, Registry, NginxParser, CaddyParser
    resolver/          — Resolver, TopologyEdge, NodeIndex, EdgeDiff
    store/             — GORM models, repository implementations
    scheduler/         — Scheduler, SchedulerConfig
    api/               — HTTP handlers, SSE, request/response types
    sse/               — SSEWriter, StreamHandler (same pattern as tailkitd)
  docker-compose.yml
  Dockerfile
```
