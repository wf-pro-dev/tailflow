package core

import "sync"

type BootstrapPhase string

const (
	BootstrapPhaseStarting      BootstrapPhase = "starting"
	BootstrapPhaseBootstrapping BootstrapPhase = "bootstrapping"
	BootstrapPhaseReady         BootstrapPhase = "ready"
	BootstrapPhaseDegraded      BootstrapPhase = "degraded"
)

type LiveNodeStatus struct {
	Online            bool      `json:"online"`
	CollectorDegraded bool      `json:"collector_degraded"`
	CollectorError    string    `json:"collector_error,omitempty"`
	LastSeenAt        Timestamp `json:"last_seen_at"`
}

type LivePort struct {
	Addr    string `json:"addr,omitempty"`
	Port    uint16 `json:"port"`
	Proto   string `json:"proto,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Process string `json:"process,omitempty"`
}

type LiveContainerPort struct {
	HostPort   uint16 `json:"host_port"`
	TargetPort uint16 `json:"target_port"`
	Proto      string `json:"proto,omitempty"`
	Source     string `json:"source,omitempty"`
	Mode       string `json:"mode,omitempty"`
}

type LiveContainer struct {
	ContainerID    string              `json:"container_id,omitempty"`
	ContainerName  string              `json:"container_name,omitempty"`
	Image          string              `json:"image,omitempty"`
	State          string              `json:"state,omitempty"`
	Status         string              `json:"status,omitempty"`
	ServiceName    string              `json:"service_name,omitempty"`
	PublishedPorts []LiveContainerPort `json:"published_ports,omitempty"`
}

type LiveServicePort struct {
	ServiceID   string `json:"service_id,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
	HostPort    uint16 `json:"host_port"`
	TargetPort  uint16 `json:"target_port"`
	Proto       string `json:"proto,omitempty"`
	Mode        string `json:"mode,omitempty"`
}

type LiveForwardTarget struct {
	Raw    string `json:"raw,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Host   string `json:"host,omitempty"`
	Port   uint16 `json:"port,omitempty"`
	Socket string `json:"socket,omitempty"`
}

type LiveForward struct {
	ListenerAddr string            `json:"listener_addr,omitempty"`
	ListenerPort uint16            `json:"listener_port"`
	Target       LiveForwardTarget `json:"target"`
	Hostnames    []string          `json:"hostnames,omitempty"`
}

type LiveNode struct {
	NodeName    NodeName          `json:"node_name"`
	TailscaleIP string            `json:"tailscale_ip,omitempty"`
	DNSName     string            `json:"dns_name,omitempty"`
	CollectedAt Timestamp         `json:"collected_at"`
	Status      LiveNodeStatus    `json:"status"`
	Ports       []LivePort        `json:"ports,omitempty"`
	Containers  []LiveContainer   `json:"containers,omitempty"`
	Services    []LiveServicePort `json:"services,omitempty"`
	Forwards    []LiveForward     `json:"forwards,omitempty"`
}

type GlobalState struct {
	mu                   sync.RWMutex
	Nodes                map[NodeName]*LiveNode `json:"nodes"`
	Version              uint64                 `json:"version"`
	TopologyVersion      uint64                 `json:"topology_version"`
	Ready                bool                   `json:"ready"`
	Phase                BootstrapPhase         `json:"phase"`
	Degraded             bool                   `json:"degraded"`
	StatusMessage        string                 `json:"status_message,omitempty"`
	TopologyNodeCount    int                    `json:"topology_node_count"`
	TopologyRouteCount   int                    `json:"topology_route_count"`
	TopologyServiceCount int                    `json:"topology_service_count"`
	UpdatedAt            Timestamp              `json:"updated_at"`
}

func NewGlobalState() *GlobalState {
	return &GlobalState{
		Nodes: make(map[NodeName]*LiveNode),
		Phase: BootstrapPhaseStarting,
	}
}

func (s *GlobalState) BeginBootstrap(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Ready = false
	s.Phase = BootstrapPhaseBootstrapping
	s.Degraded = false
	s.StatusMessage = message
	s.UpdatedAt = NowTimestamp()
}

func (s *GlobalState) Reset(nodes []LiveNode, topologyVersion uint64, ready bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := make(map[NodeName]*LiveNode, len(nodes))
	for _, node := range nodes {
		cloned := cloneLiveNode(node)
		next[node.NodeName] = &cloned
	}
	s.Nodes = next
	s.Version++
	if s.Version == 0 {
		s.Version = 1
	}
	s.TopologyVersion = topologyVersion
	s.Ready = ready
	if ready {
		s.Phase = BootstrapPhaseReady
		s.Degraded = false
		s.StatusMessage = ""
	}
	s.UpdatedAt = NowTimestamp()
}

func (s *GlobalState) UpsertNode(node LiveNode) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cloned := cloneLiveNode(node)
	s.Nodes[node.NodeName] = &cloned
	s.Version++
	if s.Version == 0 {
		s.Version = 1
	}
	s.UpdatedAt = NowTimestamp()
}

func (s *GlobalState) SetTopologyVersion(version uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TopologyVersion = version
	s.UpdatedAt = NowTimestamp()
}

func (s *GlobalState) SetTopologySummary(nodeCount, serviceCount, routeCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TopologyNodeCount = nodeCount
	s.TopologyServiceCount = serviceCount
	s.TopologyRouteCount = routeCount
	s.UpdatedAt = NowTimestamp()
}

func (s *GlobalState) SetDegraded(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Ready = false
	s.Phase = BootstrapPhaseDegraded
	s.Degraded = true
	s.StatusMessage = message
	s.UpdatedAt = NowTimestamp()
}

func (s *GlobalState) Snapshot() []LiveNode {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]LiveNode, 0, len(s.Nodes))
	for _, node := range s.Nodes {
		out = append(out, cloneLiveNode(*node))
	}
	return out
}

func cloneLiveNode(node LiveNode) LiveNode {
	cloned := node
	cloned.Ports = append([]LivePort(nil), node.Ports...)
	cloned.Containers = append([]LiveContainer(nil), node.Containers...)
	for i := range cloned.Containers {
		cloned.Containers[i].PublishedPorts = append([]LiveContainerPort(nil), node.Containers[i].PublishedPorts...)
	}
	cloned.Services = append([]LiveServicePort(nil), node.Services...)
	cloned.Forwards = append([]LiveForward(nil), node.Forwards...)
	for i := range cloned.Forwards {
		cloned.Forwards[i].Hostnames = append([]string(nil), node.Forwards[i].Hostnames...)
	}
	return cloned
}
