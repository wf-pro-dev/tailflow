// State types for Tailflow. These never import tailkit; the collector maps
// tailkit types onto them at the boundary (design §16.2, §16.3).
//
// All fields are plain Go types (no embedded gopsutil/docker SDK types) so
// the package compiles without those dependencies and serialises cleanly to
// JSON for the API layer.

package state

import (
        "time"
)

// Status is the derived host status (design §5.2).
type Status string

const (
        StatusOffline  Status = "offline"
        StatusNoAgent  Status = "no-agent"
        StatusStale    Status = "stale"
        StatusDegraded Status = "degraded"
        StatusHealthy  Status = "healthy"
)

// CapReason is the reason a capability is unavailable.
type CapReason string

const (
        CapReasonNotConfigured    CapReason = "not-configured"
        CapReasonPermissionDenied CapReason = "permission-denied"
        CapReasonUnavailable      CapReason = "unavailable"
)

// Capabilities describes what integrations a host exposes. Mirrors the
// OpenAPI Capabilities schema.
type Capabilities struct {
        Metrics bool              `json:"metrics"`
        Docker  bool              `json:"docker"`
        Swarm   bool              `json:"swarm"`
        Compose bool              `json:"compose"`
        Reasons map[string]string `json:"reasons,omitempty"`
}

// SetFalse marks a capability as unavailable with a reason. Safe to call on
// a zero-value Capabilities (Reasons is lazily allocated).
func (c *Capabilities) SetFalse(name string, reason CapReason) {
        if c.Reasons == nil {
                c.Reasons = make(map[string]string)
        }
        switch name {
        case "metrics":
                c.Metrics = false
        case "docker":
                c.Docker = false
        case "swarm":
                c.Swarm = false
        case "compose":
                c.Compose = false
        }
        c.Reasons[name] = string(reason)
}

// Host is Tailflow's view of one tailnet host. Mirrors the OpenAPI Host
// schema. The collector builds this from client.Host (design §16.3 mapping
// table).
type Host struct {
        Name           string        `json:"name"`
        Role           string        `json:"role,omitempty"`
        Environment    string        `json:"environment,omitempty"`
        Tags           []string      `json:"tags,omitempty"`
        TSIPs          []string      `json:"ts_ips,omitempty"`
        DNSName        string        `json:"ts_dns_name,omitempty"`
        OS             string        `json:"os,omitempty"`
        Online         bool          `json:"online"`
        AgentOnline    bool          `json:"-"` // tailkitd peer online (from discovery); used by status loop
        Status         Status        `json:"status"`
        Caps           Capabilities  `json:"capabilities"`
        LastSampleAt   *time.Time    `json:"last_sample_at,omitempty"`
        StreamInterval time.Duration `json:"-"` // 2s until U1; surfaced as stream_interval_s by the API
        UptimeSeconds  uint64        `json:"uptime_seconds,omitempty"`
}

// StreamIntervalSeconds returns StreamInterval as an integer number of
// seconds for the API's stream_interval_s field. Defaults to 2 per design D2.
func (h Host) StreamIntervalSeconds() int {
        if h.StreamInterval <= 0 {
                return 2
        }
        return int(h.StreamInterval.Seconds())
}

// Sample is one aggregated metric bucket (design §16.2). The aggregator
// reduces raw client.Metrics events into one Sample per HISTORY_STEP.
type Sample struct {
        T                time.Time `json:"t"`
        CPUTotal         float64   `json:"cpu_total"`               // percent, 0-100
        CPUPerCore       []float64 `json:"cpu_per_core,omitempty"`  // percent per core
        MemUsedPct       float64   `json:"mem_used_pct"`
        SwapUsedPct      float64   `json:"swap_used_pct,omitempty"`
        MemUsedBytes     uint64    `json:"mem_used_bytes,omitempty"`
        MemTotalBytes    uint64    `json:"mem_total_bytes,omitempty"`
        NetRxBps         float64   `json:"net_rx_bps"` // summed over reported interfaces, bytes/s
        NetTxBps         float64   `json:"net_tx_bps"`
}

// DiskUsage is one mount point's latest usage snapshot.
type DiskUsage struct {
        Path     string  `json:"path"`
        Device   string  `json:"device,omitempty"`
        FSType   string  `json:"fstype,omitempty"`
        Total    uint64  `json:"total"`
        Used     uint64  `json:"used"`
        Free     uint64  `json:"free,omitempty"`
        UsedPct  float64 `json:"used_pct"`
}

// NetInterfaceCounters is the latest per-interface counter snapshot plus
// the computed rate.
type NetInterfaceCounters struct {
        Name      string  `json:"name"`
        BytesRecv uint64  `json:"bytes_recv"`
        BytesSent uint64  `json:"bytes_sent"`
        RxBps     float64 `json:"rx_bps"`
        TxBps     float64 `json:"tx_bps"`
}

// Latest is the most recent observation bundle for a host. Returned by
// Store.Latest(). Disks and NetInterfaces are the latest snapshot (not a
// time series); Processes are limited by tailkitd (default 20, max 100,
// CPU-sorted — design D5).
type Latest struct {
        Processes     []Process           `json:"processes,omitempty"`
        Ports         []Port              `json:"ports,omitempty"`
        Disks         []DiskUsage         `json:"disks,omitempty"`
        NetInterfaces []NetInterfaceCounters `json:"net_interfaces,omitempty"`
}

// Process is one row in the per-host process table. Mirrors client.Process.
type Process struct {
        PID        int32   `json:"pid"`
        Name       string  `json:"name"`
        Status     string  `json:"status"`
        CPUPercent float64 `json:"cpu_percent"`
        MemoryRSS  uint64  `json:"memory_rss_bytes"`
        Cmdline    string  `json:"cmdline,omitempty"`
}

// Port is one listening socket. Mirrors client.Port (note Port is uint16).
type Port struct {
        Addr    string `json:"addr"`
        Port    uint16 `json:"port"`
        Proto   string `json:"proto"`
        PID     int    `json:"pid"`
        Process string `json:"process,omitempty"`
}

// PortKey is the tuple that uniquely identifies a listening socket for the
// snapshot/bound/released replay logic (design §F5). Used as a map key.
type PortKey struct {
        Addr  string
        Port  uint16
        Proto string
        PID   int
}

// Key returns the PortKey for a Port.
func (p Port) Key() PortKey {
        return PortKey{Addr: p.Addr, Port: p.Port, Proto: p.Proto, PID: p.PID}
}

// Container is Tailflow's view of one Docker container. Mirrors the OpenAPI
// Container schema (subset of docker/container.Summary).
type Container struct {
        ID        string         `json:"id"`
        Name      string         `json:"name"`
        Image     string         `json:"image"`
        ImageID   string         `json:"image_id,omitempty"`
        State     string         `json:"state"` // created|running|paused|restarting|exited|dead
        Status    string         `json:"status,omitempty"`
        Health    string         `json:"health,omitempty"`
        Ports     []ContainerPort `json:"ports,omitempty"`
        CreatedAt time.Time      `json:"created_at,omitempty"`
        Command   string         `json:"command,omitempty"`
        Labels    map[string]string `json:"labels,omitempty"`
}

// ContainerPort is one published/private port pair.
type ContainerPort struct {
        IP       string `json:"ip,omitempty"`
        Private  uint16 `json:"private"`
        Public   uint16 `json:"public,omitempty"`
        Proto    string `json:"proto,omitempty"`
}

// SwarmService is Tailflow's view of one Docker Swarm service.
type SwarmService struct {
        ID       string           `json:"id"`
        Name     string           `json:"name"`
        Image    string           `json:"image"`
        Mode     string           `json:"mode,omitempty"`         // replicated|global
        Replicas *int32           `json:"replicas,omitempty"`     // nil for global
        Ports    []SwarmPort      `json:"ports,omitempty"`
}

// SwarmPort is one published port on a swarm service.
type SwarmPort struct {
        Published uint32 `json:"published,omitempty"`
        Target    uint32 `json:"target"`
        Proto     string `json:"proto,omitempty"`
}

// ComposeProject is one row from `docker compose ls`.
type ComposeProject struct {
        Name        string `json:"name"`
        Status      string `json:"status,omitempty"`
        ConfigFiles string `json:"config_files,omitempty"`
}

// DockerImage is one Docker image on a host.
type DockerImage struct {
        ID        string   `json:"id"`
        Tags      []string `json:"tags,omitempty"`
        Size      int64    `json:"size,omitempty"`
        CreatedAt int64    `json:"created_at,omitempty"`
}

// SystemdUnit is one systemd unit on a host.
type SystemdUnit struct {
        Name        string `json:"name"`
        LoadState   string `json:"load_state"`
        ActiveState string `json:"active_state"`
        SubState    string `json:"sub_state"`
        Description string `json:"description,omitempty"`
}

// ContainerInventory is the latest container/swarm/compose state for a host.
type ContainerInventory struct {
        Containers      []Container      `json:"containers,omitempty"`
        SwarmServices   []SwarmService   `json:"swarm_services,omitempty"`
        ComposeProjects []ComposeProject `json:"compose_projects,omitempty"`
        Images          []DockerImage    `json:"images,omitempty"`
}
