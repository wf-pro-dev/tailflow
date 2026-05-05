package topology

import (
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/wf-pro-dev/tailflow/internal/collector"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/resolver"
	"github.com/wf-pro-dev/tailflow/internal/store"
)

type Node struct {
	Name              core.NodeName            `json:"name"`
	TailscaleIP       string                   `json:"tailscale_ip"`
	Online            bool                     `json:"online"`
	Degraded          bool                     `json:"degraded"`
	CollectorDegraded bool                     `json:"collector_degraded"`
	WorkloadDegraded  bool                     `json:"workload_degraded"`
	LastSeenAt        core.Timestamp           `json:"last_seen_at"`
	CollectorError    string                   `json:"collector_error,omitempty"`
	WorkloadIssues    []string                 `json:"workload_issues,omitempty"`
	Ports             []store.ListenPort       `json:"ports"`
	Containers        []store.Container        `json:"containers"`
	Services          []store.SwarmServicePort `json:"services"`
}

type Snapshot struct {
	Version   uint64                `json:"version"`
	Nodes     []Node                `json:"nodes"`
	Services  []store.Service       `json:"services"`
	Runtimes  []store.Runtime       `json:"runtimes"`
	Exposures []store.Exposure      `json:"exposures"`
	Routes    []store.Route         `json:"routes"`
	RouteHops []store.RouteHop      `json:"route_hops"`
	Evidence  []store.Evidence      `json:"evidence"`
	Summary   store.TopologySummary `json:"summary"`
	UpdatedAt core.Timestamp        `json:"updated_at"`
}

type Patch struct {
	Version           uint64                `json:"version"`
	UpdatedAt         core.Timestamp        `json:"updated_at"`
	ChangedNodes      []core.NodeName       `json:"changed_nodes"`
	NodesUpserted     []Node                `json:"nodes_upserted"`
	NodesRemoved      []core.NodeName       `json:"nodes_removed"`
	ServicesUpserted  []store.Service       `json:"services_upserted"`
	ServicesRemoved   []core.ID             `json:"services_removed"`
	RuntimesUpserted  []store.Runtime       `json:"runtimes_upserted"`
	RuntimesRemoved   []core.ID             `json:"runtimes_removed"`
	ExposuresUpserted []store.Exposure      `json:"exposures_upserted"`
	ExposuresRemoved  []core.ID             `json:"exposures_removed"`
	RoutesUpserted    []store.Route         `json:"routes_upserted"`
	RoutesRemoved     []core.ID             `json:"routes_removed"`
	RouteHopsUpserted []store.RouteHop      `json:"route_hops_upserted"`
	RouteHopsRemoved  []core.ID             `json:"route_hops_removed"`
	EvidenceUpserted  []store.Evidence      `json:"evidence_upserted"`
	EvidenceRemoved   []core.ID             `json:"evidence_removed"`
	Summary           store.TopologySummary `json:"summary"`
}

type ScopedIDs struct {
	ServiceIDs  map[string]struct{}
	RuntimeIDs  map[string]struct{}
	ExposureIDs map[string]struct{}
	RouteIDs    map[string]struct{}
	HopIDs      map[string]struct{}
	EvidenceIDs map[string]struct{}
}

type ScopeMask struct {
	Nodes     bool
	Services  bool
	Runtimes  bool
	Exposures bool
	Routes    bool
	RouteHops bool
	Evidence  bool
}

var FullNodeScopeMask = ScopeMask{
	Nodes:     true,
	Services:  true,
	Runtimes:  true,
	Exposures: true,
	Routes:    true,
	RouteHops: true,
	Evidence:  true,
}

var ContainerScopeMask = ScopeMask{
	Nodes:     true,
	Services:  true,
	Runtimes:  true,
	Exposures: true,
	Routes:    true,
	RouteHops: true,
	Evidence:  true,
}

var ServiceScopeMask = ScopeMask{
	Nodes:     true,
	Services:  true,
	Exposures: true,
	Routes:    true,
	RouteHops: true,
	Evidence:  true,
}

var ForwardRouteScopeMask = ScopeMask{
	Routes:    true,
	RouteHops: true,
	Evidence:  true,
}

type Reset struct {
	Reason   string   `json:"reason"`
	Snapshot Snapshot `json:"snapshot"`
}

type entityState struct {
	nodesByName   map[core.NodeName]Node
	servicesByID  map[core.ID]store.Service
	runtimesByID  map[core.ID]store.Runtime
	exposuresByID map[core.ID]store.Exposure
	routesByID    map[core.ID]store.Route
	routeHopsByID map[core.ID]store.RouteHop
	evidenceByID  map[core.ID]store.Evidence
}

type Manager struct {
	mu         sync.RWMutex
	snapshot   Snapshot
	entities   entityState
	indexes    snapshotIndexes
	nodeInputs map[core.NodeName]nodeTopologyInput
}

func NewManager() *Manager {
	return &Manager{
		entities:   buildEntityState(Snapshot{}),
		nodeInputs: make(map[core.NodeName]nodeTopologyInput),
	}
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneSnapshot(m.snapshot)
}

func (m *Manager) Reset(snapshots []store.NodeSnapshot, statuses []collector.NodeStatus, reason string) Reset {
	m.mu.Lock()
	defer m.mu.Unlock()

	next, nextIndexes := buildSnapshot(snapshots, statuses)
	nextNodeInputs := buildNodeTopologyInputs(snapshots, statuses)
	next.Version = m.snapshot.Version + 1
	if next.Version == 0 {
		next.Version = 1
	}
	m.snapshot = cloneSnapshot(next)
	m.entities = buildEntityState(next)
	m.indexes = nextIndexes
	m.nodeInputs = nextNodeInputs
	core.Infof("topology: reset reason=%s version=%d nodes=%d services=%d routes=%d", reason, next.Version, len(next.Nodes), len(next.Services), len(next.Routes))
	return Reset{
		Reason:   reason,
		Snapshot: cloneSnapshot(next),
	}
}

func (m *Manager) Apply(snapshots []store.NodeSnapshot, statuses []collector.NodeStatus) (Patch, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	next, nextIndexes := buildSnapshot(snapshots, statuses)
	nextNodeInputs := buildNodeTopologyInputs(snapshots, statuses)
	if snapshotsEquivalent(m.snapshot, next) {
		core.Debugf("topology: apply no-op previous_version=%d", m.snapshot.Version)
		return Patch{}, false
	}

	next.Version = m.snapshot.Version + 1
	if next.Version == 0 {
		next.Version = 1
	}

	patch := diffSnapshots(m.snapshot, next)
	patch.Version = next.Version
	patch.UpdatedAt = next.UpdatedAt
	m.snapshot = cloneSnapshot(next)
	m.entities = buildEntityState(next)
	m.indexes = nextIndexes
	m.nodeInputs = nextNodeInputs
	core.Infof("topology: apply version=%d changed_nodes=%d nodes_upserted=%d nodes_removed=%d services_upserted=%d services_removed=%d routes_upserted=%d routes_removed=%d", patch.Version, len(patch.ChangedNodes), len(patch.NodesUpserted), len(patch.NodesRemoved), len(patch.ServicesUpserted), len(patch.ServicesRemoved), len(patch.RoutesUpserted), len(patch.RoutesRemoved))
	return patch, true
}

func (m *Manager) ApplyForwardRoutes(nodeName core.NodeName, previousScope ScopedIDs, snapshots []store.NodeSnapshot, statuses []collector.NodeStatus) (Patch, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	nextNodeInput, ok := buildNodeTopologyInput(nodeName, snapshots, statuses)
	if !ok {
		core.Warnf("topology: apply forward-routes skipped node=%s missing_node_input=true", nodeName)
		return Patch{}, false
	}
	if currentNodeInput, ok := m.nodeInputs[nodeName]; ok && reflect.DeepEqual(currentNodeInput, nextNodeInput) {
		core.Debugf("topology: apply forward-routes no-op node=%s reason=node_input_unchanged", nodeName)
		return Patch{}, false
	}

	if previousScope.isEmpty() {
		previousScope = m.indexes.scopeForNode(nodeName)
	}

	projected := resolver.BuildRouteProjectionForNode(snapshots, nodeName)
	next := cloneSnapshot(m.snapshot)
	next.Routes = replaceRoutesByScope(next.Routes, previousScope.RouteIDs, projected.Routes)
	next.RouteHops = replaceRouteHopsByScope(next.RouteHops, previousScope.HopIDs, projected.RouteHops)
	next.Evidence = replaceEvidenceByScope(next.Evidence, previousScope.EvidenceIDs, projected.Evidence)
	next.Summary = summarizeSnapshot(next)
	next.UpdatedAt = currentUpdatedAt(snapshots)

	if snapshotsEquivalent(m.snapshot, next) {
		core.Debugf("topology: apply forward-routes no-op node=%s previous_version=%d", nodeName, m.snapshot.Version)
		return Patch{}, false
	}

	next.Version = m.snapshot.Version + 1
	if next.Version == 0 {
		next.Version = 1
	}

	nextIndexes := buildSnapshotIndexes(next)
	patch := diffScopedNodeSnapshot(
		m.snapshot,
		maskScopedIDs(previousScope, ForwardRouteScopeMask),
		next,
		maskScopedIDs(nextIndexes.scopeForNode(nodeName), ForwardRouteScopeMask),
		nodeName,
		ForwardRouteScopeMask,
	)
	patch.Version = next.Version
	patch.UpdatedAt = next.UpdatedAt
	m.snapshot = cloneSnapshot(next)
	m.entities = buildEntityState(next)
	m.indexes = nextIndexes
	m.nodeInputs[nodeName] = nextNodeInput
	if patch.isEmpty() {
		core.Debugf("topology: apply forward-routes empty node=%s version=%d", nodeName, next.Version)
		return Patch{}, false
	}
	core.Infof("topology: apply forward-routes node=%s version=%d routes_upserted=%d routes_removed=%d", nodeName, patch.Version, len(patch.RoutesUpserted), len(patch.RoutesRemoved))
	return patch, true
}

func (m *Manager) ApplyNodeScope(nodeName core.NodeName, previousScope ScopedIDs, mask ScopeMask, snapshots []store.NodeSnapshot, statuses []collector.NodeStatus) (Patch, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	nextNodeInput, ok := buildNodeTopologyInput(nodeName, snapshots, statuses)
	if !ok {
		core.Warnf("topology: apply node-scope skipped node=%s missing_node_input=true", nodeName)
		return Patch{}, false
	}
	if currentNodeInput, ok := m.nodeInputs[nodeName]; ok && reflect.DeepEqual(currentNodeInput, nextNodeInput) {
		core.Debugf("topology: apply node-scope no-op node=%s reason=node_input_unchanged", nodeName)
		return Patch{}, false
	}

	if previousScope.isEmpty() {
		previousScope = m.indexes.scopeForNode(nodeName)
	}

	inventoryProjection := resolver.BuildInventoryProjection(snapshots)
	sourceNodes := collectRouteSourceNodes(m.snapshot, previousScope.RouteIDs)
	if hasNodeForwards(snapshots, nodeName) {
		sourceNodes = appendUniqueNodeNames(sourceNodes, nodeName)
	}
	routeProjection := resolver.BuildRouteProjectionForNodes(snapshots, sourceNodes)

	next := cloneSnapshot(m.snapshot)
	if nextNode, ok := buildNextNode(nodeName, snapshots, statuses); ok {
		next.Nodes = replaceNodeByName(next.Nodes, nextNode)
	}

	nextScope := collectProjectedNodeScope(nodeName, inventoryProjection, routeProjection)
	serviceIDs := unionStringSets(previousScope.ServiceIDs, nextScope.ServiceIDs)
	runtimeIDs := unionStringSets(previousScope.RuntimeIDs, nextScope.RuntimeIDs)
	exposureIDs := unionStringSets(previousScope.ExposureIDs, nextScope.ExposureIDs)
	routeIDs := unionStringSets(previousScope.RouteIDs, nextScope.RouteIDs)
	hopIDs := unionStringSets(previousScope.HopIDs, nextScope.HopIDs)
	evidenceIDs := unionStringSets(previousScope.EvidenceIDs, nextScope.EvidenceIDs)

	next.Services = replaceServicesByIDs(next.Services, serviceIDs, inventoryProjection.Services)
	next.Runtimes = replaceRuntimesByIDs(next.Runtimes, runtimeIDs, inventoryProjection.Runtimes)
	next.Exposures = replaceExposuresByIDs(next.Exposures, exposureIDs, inventoryProjection.Exposures)
	next.Routes = replaceRoutesByScope(next.Routes, routeIDs, routeProjection.Routes)
	next.RouteHops = replaceRouteHopsByScope(next.RouteHops, hopIDs, routeProjection.RouteHops)
	next.Evidence = replaceEvidenceByScope(next.Evidence, evidenceIDs, routeProjection.Evidence)
	next.Summary = summarizeSnapshot(next)
	next.UpdatedAt = currentUpdatedAt(snapshots)

	if snapshotsEquivalent(m.snapshot, next) {
		core.Debugf("topology: apply node-scope no-op node=%s previous_version=%d", nodeName, m.snapshot.Version)
		return Patch{}, false
	}

	next.Version = m.snapshot.Version + 1
	if next.Version == 0 {
		next.Version = 1
	}

	nextIndexes := buildSnapshotIndexes(next)
	nextNodeInputs := m.nodeInputs
	if nextNodeInputs == nil {
		nextNodeInputs = make(map[core.NodeName]nodeTopologyInput)
	}
	nextNodeInputs[nodeName] = nextNodeInput

	patch := diffScopedNodeSnapshot(m.snapshot, maskScopedIDs(previousScope, mask), next, maskScopedIDs(nextScope, mask), nodeName, mask)
	patch.Version = next.Version
	patch.UpdatedAt = next.UpdatedAt
	m.snapshot = cloneSnapshot(next)
	m.entities = buildEntityState(next)
	m.indexes = nextIndexes
	m.nodeInputs = nextNodeInputs
	if patch.isEmpty() {
		core.Debugf("topology: apply node-scope empty node=%s version=%d", nodeName, next.Version)
		return Patch{}, false
	}
	core.Infof("topology: apply node-scope node=%s version=%d changed_nodes=%d services_upserted=%d routes_upserted=%d", nodeName, patch.Version, len(patch.ChangedNodes), len(patch.ServicesUpserted), len(patch.RoutesUpserted))
	return patch, true
}

func buildSnapshot(snapshots []store.NodeSnapshot, statuses []collector.NodeStatus) (Snapshot, snapshotIndexes) {
	topologyData := resolver.BuildTopologyData(snapshots)
	statusByName := make(map[core.NodeName]collector.NodeStatus, len(statuses))
	for _, status := range statuses {
		statusByName[status.NodeName] = status
	}

	nodes := make([]Node, 0, len(snapshots))
	updatedAt := core.Timestamp{}
	for _, snapshot := range snapshots {
		if updatedAt.IsZero() || snapshot.CollectedAt.Time().After(updatedAt.Time()) {
			updatedAt = snapshot.CollectedAt
		}
		nodes = append(nodes, buildNode(snapshot, statusByName[snapshot.NodeName]))
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	if updatedAt.IsZero() {
		updatedAt = core.NowTimestamp()
	}

	snapshot := Snapshot{
		Nodes:     nodes,
		Services:  slices.Clone(topologyData.Services),
		Runtimes:  slices.Clone(topologyData.Runtimes),
		Exposures: slices.Clone(topologyData.Exposures),
		Routes:    slices.Clone(topologyData.Routes),
		RouteHops: slices.Clone(topologyData.RouteHops),
		Evidence:  slices.Clone(topologyData.Evidence),
		Summary:   topologyData.Summary,
		UpdatedAt: updatedAt,
	}
	return snapshot, buildSnapshotIndexes(snapshot)
}

func buildNode(snapshot store.NodeSnapshot, status collector.NodeStatus) Node {
	workload := assessWorkload(snapshot)
	return Node{
		Name:              snapshot.NodeName,
		TailscaleIP:       snapshot.TailscaleIP,
		Online:            status.Online,
		Degraded:          status.Degraded || workload.Degraded,
		CollectorDegraded: status.Degraded,
		WorkloadDegraded:  workload.Degraded,
		LastSeenAt:        status.LastSeenAt,
		CollectorError:    status.LastError,
		WorkloadIssues:    workload.Issues,
		Ports:             slices.Clone(snapshot.Ports),
		Containers:        slices.Clone(snapshot.Containers),
		Services:          slices.Clone(snapshot.Services),
	}
}

func (m *Manager) ApplyNodeStatus(status collector.NodeStatus) (Patch, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.snapshot.Nodes {
		if m.snapshot.Nodes[i].Name != status.NodeName {
			continue
		}

		current := m.snapshot.Nodes[i]
		next := current
		next.Online = status.Online
		next.CollectorDegraded = status.Degraded
		next.Degraded = status.Degraded || next.WorkloadDegraded
		next.LastSeenAt = status.LastSeenAt
		next.CollectorError = status.LastError
		if reflect.DeepEqual(current, next) {
			return Patch{}, false
		}

		m.snapshot.Version++
		if m.snapshot.Version == 0 {
			m.snapshot.Version = 1
		}
		m.snapshot.UpdatedAt = core.NowTimestamp()
		m.snapshot.Nodes[i] = next
		m.entities.nodesByName[next.Name] = next
		nodeInput := m.nodeInputs[status.NodeName]
		nodeInput.Online = status.Online
		nodeInput.CollectorDegraded = status.Degraded
		nodeInput.CollectorError = status.LastError
		m.nodeInputs[status.NodeName] = nodeInput
		return Patch{
			Version:       m.snapshot.Version,
			UpdatedAt:     m.snapshot.UpdatedAt,
			ChangedNodes:  []core.NodeName{status.NodeName},
			NodesUpserted: []Node{next},
			Summary:       m.snapshot.Summary,
		}, true
	}

	return Patch{}, false
}

type workloadAssessment struct {
	Degraded bool
	Issues   []string
}

type nodeTopologyInput struct {
	NodeName          core.NodeName
	TailscaleIP       string
	Online            bool
	CollectorDegraded bool
	CollectorError    string
	Ports             []store.ListenPort
	Containers        []store.Container
	Services          []store.SwarmServicePort
	Forwards          []parser.ForwardAction
}

func assessWorkload(snapshot store.NodeSnapshot) workloadAssessment {
	issues := make([]string, 0)
	for _, container := range snapshot.Containers {
		if issue, ok := assessContainerWorkload(container); ok {
			issues = append(issues, issue)
		}
	}
	return workloadAssessment{Degraded: len(issues) > 0, Issues: issues}
}

func assessContainerWorkload(container store.Container) (string, bool) {
	state := strings.ToLower(strings.TrimSpace(container.State))
	status := strings.ToLower(strings.TrimSpace(container.Status))
	name := container.ContainerName

	switch {
	case strings.Contains(status, "unhealthy"):
		return "container " + name + " is unhealthy", true
	case container.ServiceName != "":
		return "", false
	case len(container.PublishedPorts) == 0:
		return "", false
	case state == "" || state == "running":
		return "", false
	default:
		return "container " + name + " is " + state, true
	}
}

func buildNodeTopologyInputs(snapshots []store.NodeSnapshot, statuses []collector.NodeStatus) map[core.NodeName]nodeTopologyInput {
	inputs := make(map[core.NodeName]nodeTopologyInput, len(snapshots))
	for _, snapshot := range snapshots {
		if input, ok := buildNodeTopologyInput(snapshot.NodeName, snapshots, statuses); ok {
			inputs[snapshot.NodeName] = input
		}
	}
	return inputs
}

func buildNodeTopologyInput(nodeName core.NodeName, snapshots []store.NodeSnapshot, statuses []collector.NodeStatus) (nodeTopologyInput, bool) {
	snapshot, ok := findNodeSnapshot(snapshots, nodeName)
	if !ok {
		return nodeTopologyInput{}, false
	}
	status, _ := findNodeStatus(statuses, nodeName)
	return nodeTopologyInput{
		NodeName:          snapshot.NodeName,
		TailscaleIP:       snapshot.TailscaleIP,
		Online:            status.Online,
		CollectorDegraded: status.Degraded,
		CollectorError:    status.LastError,
		Ports:             slices.Clone(snapshot.Ports),
		Containers:        slices.Clone(snapshot.Containers),
		Services:          slices.Clone(snapshot.Services),
		Forwards:          slices.Clone(snapshot.Forwards),
	}, true
}

func findNodeSnapshot(snapshots []store.NodeSnapshot, nodeName core.NodeName) (store.NodeSnapshot, bool) {
	for _, snapshot := range snapshots {
		if snapshot.NodeName == nodeName {
			return snapshot, true
		}
	}
	return store.NodeSnapshot{}, false
}

func findNodeStatus(statuses []collector.NodeStatus, nodeName core.NodeName) (collector.NodeStatus, bool) {
	for _, status := range statuses {
		if status.NodeName == nodeName {
			return status, true
		}
	}
	return collector.NodeStatus{}, false
}

func snapshotsEquivalent(left, right Snapshot) bool {
	return diffSnapshots(normalizeSnapshotForCompare(left), normalizeSnapshotForCompare(right)).isEmpty()
}

func normalizeSnapshotForCompare(snapshot Snapshot) Snapshot {
	normalized := cloneSnapshot(snapshot)
	normalized.UpdatedAt = core.Timestamp{}
	for i := range normalized.Nodes {
		normalized.Nodes[i].LastSeenAt = core.Timestamp{}
	}
	for i := range normalized.Runtimes {
		normalized.Runtimes[i].CollectedAt = core.Timestamp{}
	}
	return normalized
}

func diffSnapshots(previous, current Snapshot) Patch {
	return Patch{
		ChangedNodes:      diffNodeNames(previous.Nodes, current.Nodes),
		NodesUpserted:     diffUpserted(previous.Nodes, current.Nodes, func(value Node) string { return value.Name }),
		NodesRemoved:      diffRemoved(previous.Nodes, current.Nodes, func(value Node) string { return value.Name }),
		ServicesUpserted:  diffUpserted(previous.Services, current.Services, func(value store.Service) string { return value.ID }),
		ServicesRemoved:   diffRemoved(previous.Services, current.Services, func(value store.Service) string { return value.ID }),
		RuntimesUpserted:  diffUpserted(previous.Runtimes, current.Runtimes, func(value store.Runtime) string { return value.ID }),
		RuntimesRemoved:   diffRemoved(previous.Runtimes, current.Runtimes, func(value store.Runtime) string { return value.ID }),
		ExposuresUpserted: diffUpserted(previous.Exposures, current.Exposures, func(value store.Exposure) string { return value.ID }),
		ExposuresRemoved:  diffRemoved(previous.Exposures, current.Exposures, func(value store.Exposure) string { return value.ID }),
		RoutesUpserted:    diffUpserted(previous.Routes, current.Routes, func(value store.Route) string { return value.ID }),
		RoutesRemoved:     diffRemoved(previous.Routes, current.Routes, func(value store.Route) string { return value.ID }),
		RouteHopsUpserted: diffUpserted(previous.RouteHops, current.RouteHops, func(value store.RouteHop) string { return value.ID }),
		RouteHopsRemoved:  diffRemoved(previous.RouteHops, current.RouteHops, func(value store.RouteHop) string { return value.ID }),
		EvidenceUpserted:  diffUpserted(previous.Evidence, current.Evidence, func(value store.Evidence) string { return value.ID }),
		EvidenceRemoved:   diffRemoved(previous.Evidence, current.Evidence, func(value store.Evidence) string { return value.ID }),
		Summary:           current.Summary,
	}
}

func diffScopedNodeSnapshot(previous Snapshot, previousScope ScopedIDs, current Snapshot, currentScope ScopedIDs, nodeName core.NodeName, mask ScopeMask) Patch {
	changedNodes := make([]core.NodeName, 0, 1)
	if mask.Nodes && nodesEqualByName(previous.Nodes, current.Nodes, nodeName) == false {
		changedNodes = append(changedNodes, nodeName)
	}

	return Patch{
		ChangedNodes:      changedNodes,
		NodesUpserted:     diffScopedNodes(mask, previous.Nodes, current.Nodes, nodeName),
		NodesRemoved:      diffScopedNodeRemovals(mask, previous.Nodes, current.Nodes, nodeName),
		ServicesUpserted:  diffScopedServices(mask, previous.Services, current.Services, previousScope.ServiceIDs, currentScope.ServiceIDs),
		ServicesRemoved:   diffScopedServiceRemovals(mask, previous.Services, current.Services, previousScope.ServiceIDs, currentScope.ServiceIDs),
		RuntimesUpserted:  diffScopedRuntimes(mask, previous.Runtimes, current.Runtimes, previousScope.RuntimeIDs, currentScope.RuntimeIDs),
		RuntimesRemoved:   diffScopedRuntimeRemovals(mask, previous.Runtimes, current.Runtimes, previousScope.RuntimeIDs, currentScope.RuntimeIDs),
		ExposuresUpserted: diffScopedExposures(mask, previous.Exposures, current.Exposures, previousScope.ExposureIDs, currentScope.ExposureIDs),
		ExposuresRemoved:  diffScopedExposureRemovals(mask, previous.Exposures, current.Exposures, previousScope.ExposureIDs, currentScope.ExposureIDs),
		RoutesUpserted:    diffScopedRoutes(mask, previous.Routes, current.Routes, previousScope.RouteIDs, currentScope.RouteIDs),
		RoutesRemoved:     diffScopedRouteRemovals(mask, previous.Routes, current.Routes, previousScope.RouteIDs, currentScope.RouteIDs),
		RouteHopsUpserted: diffScopedRouteHops(mask, previous.RouteHops, current.RouteHops, previousScope.HopIDs, currentScope.HopIDs),
		RouteHopsRemoved:  diffScopedRouteHopRemovals(mask, previous.RouteHops, current.RouteHops, previousScope.HopIDs, currentScope.HopIDs),
		EvidenceUpserted:  diffScopedEvidence(mask, previous.Evidence, current.Evidence, previousScope.EvidenceIDs, currentScope.EvidenceIDs),
		EvidenceRemoved:   diffScopedEvidenceRemovals(mask, previous.Evidence, current.Evidence, previousScope.EvidenceIDs, currentScope.EvidenceIDs),
		Summary:           current.Summary,
	}
}

func (s ScopedIDs) isEmpty() bool {
	return len(s.ServiceIDs) == 0 &&
		len(s.RuntimeIDs) == 0 &&
		len(s.ExposureIDs) == 0 &&
		len(s.RouteIDs) == 0 &&
		len(s.HopIDs) == 0 &&
		len(s.EvidenceIDs) == 0
}

type snapshotIndexes struct {
	serviceIDsByNode   map[core.NodeName]map[string]struct{}
	runtimeIDsByNode   map[core.NodeName]map[string]struct{}
	exposureIDsByNode  map[core.NodeName]map[string]struct{}
	routeIDsByService  map[string]map[string]struct{}
	routeIDsByExposure map[string]map[string]struct{}
	routeIDsByRuntime  map[string]map[string]struct{}
	hopIDsByRoute      map[string]map[string]struct{}
	evidenceIDsByRoute map[string]map[string]struct{}
}

func buildSnapshotIndexes(snapshot Snapshot) snapshotIndexes {
	indexes := snapshotIndexes{
		serviceIDsByNode:   make(map[core.NodeName]map[string]struct{}),
		runtimeIDsByNode:   make(map[core.NodeName]map[string]struct{}),
		exposureIDsByNode:  make(map[core.NodeName]map[string]struct{}),
		routeIDsByService:  make(map[string]map[string]struct{}),
		routeIDsByExposure: make(map[string]map[string]struct{}),
		routeIDsByRuntime:  make(map[string]map[string]struct{}),
		hopIDsByRoute:      make(map[string]map[string]struct{}),
		evidenceIDsByRoute: make(map[string]map[string]struct{}),
	}

	for _, service := range snapshot.Services {
		addStringToNodeIndex(indexes.serviceIDsByNode, service.PrimaryNode, service.ID)
	}
	for _, runtime := range snapshot.Runtimes {
		addStringToNodeIndex(indexes.runtimeIDsByNode, runtime.NodeID, runtime.ID)
	}
	for _, exposure := range snapshot.Exposures {
		addStringToNodeIndex(indexes.exposureIDsByNode, exposure.NodeID, exposure.ID)
	}
	for _, route := range snapshot.Routes {
		addStringToKeyIndex(indexes.routeIDsByService, route.SourceServiceID, route.ID)
		addStringToKeyIndex(indexes.routeIDsByService, route.TargetServiceID, route.ID)
		addStringToKeyIndex(indexes.routeIDsByExposure, route.SourceExposureID, route.ID)
		addStringToKeyIndex(indexes.routeIDsByRuntime, route.TargetRuntimeID, route.ID)
	}
	for _, hop := range snapshot.RouteHops {
		addStringToKeyIndex(indexes.hopIDsByRoute, hop.RouteID, hop.ID)
		if hop.EvidenceID != "" {
			addStringToKeyIndex(indexes.evidenceIDsByRoute, hop.RouteID, hop.EvidenceID)
		}
	}

	return indexes
}

func (indexes snapshotIndexes) scopeForNode(nodeName core.NodeName) ScopedIDs {
	scope := newScopedIDs()

	for serviceID := range indexes.serviceIDsByNode[nodeName] {
		scope.ServiceIDs[serviceID] = struct{}{}
		for routeID := range indexes.routeIDsByService[serviceID] {
			scope.RouteIDs[routeID] = struct{}{}
		}
	}
	for runtimeID := range indexes.runtimeIDsByNode[nodeName] {
		scope.RuntimeIDs[runtimeID] = struct{}{}
		for routeID := range indexes.routeIDsByRuntime[runtimeID] {
			scope.RouteIDs[routeID] = struct{}{}
		}
	}
	for exposureID := range indexes.exposureIDsByNode[nodeName] {
		scope.ExposureIDs[exposureID] = struct{}{}
		for routeID := range indexes.routeIDsByExposure[exposureID] {
			scope.RouteIDs[routeID] = struct{}{}
		}
	}
	for routeID := range scope.RouteIDs {
		for hopID := range indexes.hopIDsByRoute[routeID] {
			scope.HopIDs[hopID] = struct{}{}
		}
		for evidenceID := range indexes.evidenceIDsByRoute[routeID] {
			scope.EvidenceIDs[evidenceID] = struct{}{}
		}
	}

	return scope
}

func newScopedIDs() ScopedIDs {
	return ScopedIDs{
		ServiceIDs:  make(map[string]struct{}),
		RuntimeIDs:  make(map[string]struct{}),
		ExposureIDs: make(map[string]struct{}),
		RouteIDs:    make(map[string]struct{}),
		HopIDs:      make(map[string]struct{}),
		EvidenceIDs: make(map[string]struct{}),
	}
}

func addStringToNodeIndex(index map[core.NodeName]map[string]struct{}, nodeName core.NodeName, value string) {
	if nodeName == "" || value == "" {
		return
	}
	values, ok := index[nodeName]
	if !ok {
		values = make(map[string]struct{})
		index[nodeName] = values
	}
	values[value] = struct{}{}
}

func addStringToKeyIndex(index map[string]map[string]struct{}, key, value string) {
	if key == "" || value == "" {
		return
	}
	values, ok := index[key]
	if !ok {
		values = make(map[string]struct{})
		index[key] = values
	}
	values[value] = struct{}{}
}

func nodesEqualByName(previous, current []Node, nodeName core.NodeName) bool {
	var left *Node
	var right *Node
	for i := range previous {
		if previous[i].Name == nodeName {
			left = &previous[i]
			break
		}
	}
	for i := range current {
		if current[i].Name == nodeName {
			right = &current[i]
			break
		}
	}
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return reflect.DeepEqual(*left, *right)
}

func filterNodesByName(nodes []Node, nodeName core.NodeName) []Node {
	out := make([]Node, 0, 1)
	for _, node := range nodes {
		if node.Name == nodeName {
			out = append(out, node)
		}
	}
	return out
}

func filterServicesByIDs(values []store.Service, ids map[string]struct{}) []store.Service {
	out := make([]store.Service, 0)
	for _, value := range values {
		if _, ok := ids[value.ID]; ok {
			out = append(out, value)
		}
	}
	return out
}

func filterRuntimesByIDs(values []store.Runtime, ids map[string]struct{}) []store.Runtime {
	out := make([]store.Runtime, 0)
	for _, value := range values {
		if _, ok := ids[value.ID]; ok {
			out = append(out, value)
		}
	}
	return out
}

func filterExposuresByIDs(values []store.Exposure, ids map[string]struct{}) []store.Exposure {
	out := make([]store.Exposure, 0)
	for _, value := range values {
		if _, ok := ids[value.ID]; ok {
			out = append(out, value)
		}
	}
	return out
}

func filterRoutesByIDs(values []store.Route, ids map[string]struct{}) []store.Route {
	out := make([]store.Route, 0)
	for _, value := range values {
		if _, ok := ids[value.ID]; ok {
			out = append(out, value)
		}
	}
	return out
}

func filterRouteHopsByIDs(values []store.RouteHop, ids map[string]struct{}) []store.RouteHop {
	out := make([]store.RouteHop, 0)
	for _, value := range values {
		if _, ok := ids[value.ID]; ok {
			out = append(out, value)
		}
	}
	return out
}

func filterEvidenceByIDs(values []store.Evidence, ids map[string]struct{}) []store.Evidence {
	out := make([]store.Evidence, 0)
	for _, value := range values {
		if _, ok := ids[value.ID]; ok {
			out = append(out, value)
		}
	}
	return out
}

func maskScopedIDs(scope ScopedIDs, mask ScopeMask) ScopedIDs {
	masked := newScopedIDs()
	if mask.Services {
		for id := range scope.ServiceIDs {
			masked.ServiceIDs[id] = struct{}{}
		}
	}
	if mask.Runtimes {
		for id := range scope.RuntimeIDs {
			masked.RuntimeIDs[id] = struct{}{}
		}
	}
	if mask.Exposures {
		for id := range scope.ExposureIDs {
			masked.ExposureIDs[id] = struct{}{}
		}
	}
	if mask.Routes {
		for id := range scope.RouteIDs {
			masked.RouteIDs[id] = struct{}{}
		}
	}
	if mask.RouteHops {
		for id := range scope.HopIDs {
			masked.HopIDs[id] = struct{}{}
		}
	}
	if mask.Evidence {
		for id := range scope.EvidenceIDs {
			masked.EvidenceIDs[id] = struct{}{}
		}
	}
	return masked
}

func diffScopedNodes(mask ScopeMask, previous, current []Node, nodeName core.NodeName) []Node {
	if !mask.Nodes {
		return nil
	}
	return diffUpserted(filterNodesByName(previous, nodeName), filterNodesByName(current, nodeName), func(value Node) string { return value.Name })
}

func diffScopedNodeRemovals(mask ScopeMask, previous, current []Node, nodeName core.NodeName) []string {
	if !mask.Nodes {
		return nil
	}
	return diffRemoved(filterNodesByName(previous, nodeName), filterNodesByName(current, nodeName), func(value Node) string { return value.Name })
}

func diffScopedServices(mask ScopeMask, previous, current []store.Service, previousIDs, currentIDs map[string]struct{}) []store.Service {
	if !mask.Services {
		return nil
	}
	return diffUpserted(filterServicesByIDs(previous, currentIDs), filterServicesByIDs(current, currentIDs), func(value store.Service) string { return value.ID })
}

func diffScopedServiceRemovals(mask ScopeMask, previous, current []store.Service, previousIDs, currentIDs map[string]struct{}) []string {
	if !mask.Services {
		return nil
	}
	return diffRemoved(filterServicesByIDs(previous, previousIDs), filterServicesByIDs(current, currentIDs), func(value store.Service) string { return value.ID })
}

func diffScopedRuntimes(mask ScopeMask, previous, current []store.Runtime, previousIDs, currentIDs map[string]struct{}) []store.Runtime {
	if !mask.Runtimes {
		return nil
	}
	return diffUpserted(filterRuntimesByIDs(previous, currentIDs), filterRuntimesByIDs(current, currentIDs), func(value store.Runtime) string { return value.ID })
}

func diffScopedRuntimeRemovals(mask ScopeMask, previous, current []store.Runtime, previousIDs, currentIDs map[string]struct{}) []string {
	if !mask.Runtimes {
		return nil
	}
	return diffRemoved(filterRuntimesByIDs(previous, previousIDs), filterRuntimesByIDs(current, currentIDs), func(value store.Runtime) string { return value.ID })
}

func diffScopedExposures(mask ScopeMask, previous, current []store.Exposure, previousIDs, currentIDs map[string]struct{}) []store.Exposure {
	if !mask.Exposures {
		return nil
	}
	return diffUpserted(filterExposuresByIDs(previous, currentIDs), filterExposuresByIDs(current, currentIDs), func(value store.Exposure) string { return value.ID })
}

func diffScopedExposureRemovals(mask ScopeMask, previous, current []store.Exposure, previousIDs, currentIDs map[string]struct{}) []string {
	if !mask.Exposures {
		return nil
	}
	return diffRemoved(filterExposuresByIDs(previous, previousIDs), filterExposuresByIDs(current, currentIDs), func(value store.Exposure) string { return value.ID })
}

func diffScopedRoutes(mask ScopeMask, previous, current []store.Route, previousIDs, currentIDs map[string]struct{}) []store.Route {
	if !mask.Routes {
		return nil
	}
	return diffUpserted(filterRoutesByIDs(previous, currentIDs), filterRoutesByIDs(current, currentIDs), func(value store.Route) string { return value.ID })
}

func diffScopedRouteRemovals(mask ScopeMask, previous, current []store.Route, previousIDs, currentIDs map[string]struct{}) []string {
	if !mask.Routes {
		return nil
	}
	return diffRemoved(filterRoutesByIDs(previous, previousIDs), filterRoutesByIDs(current, currentIDs), func(value store.Route) string { return value.ID })
}

func diffScopedRouteHops(mask ScopeMask, previous, current []store.RouteHop, previousIDs, currentIDs map[string]struct{}) []store.RouteHop {
	if !mask.RouteHops {
		return nil
	}
	return diffUpserted(filterRouteHopsByIDs(previous, currentIDs), filterRouteHopsByIDs(current, currentIDs), func(value store.RouteHop) string { return value.ID })
}

func diffScopedRouteHopRemovals(mask ScopeMask, previous, current []store.RouteHop, previousIDs, currentIDs map[string]struct{}) []string {
	if !mask.RouteHops {
		return nil
	}
	return diffRemoved(filterRouteHopsByIDs(previous, previousIDs), filterRouteHopsByIDs(current, currentIDs), func(value store.RouteHop) string { return value.ID })
}

func diffScopedEvidence(mask ScopeMask, previous, current []store.Evidence, previousIDs, currentIDs map[string]struct{}) []store.Evidence {
	if !mask.Evidence {
		return nil
	}
	return diffUpserted(filterEvidenceByIDs(previous, currentIDs), filterEvidenceByIDs(current, currentIDs), func(value store.Evidence) string { return value.ID })
}

func diffScopedEvidenceRemovals(mask ScopeMask, previous, current []store.Evidence, previousIDs, currentIDs map[string]struct{}) []string {
	if !mask.Evidence {
		return nil
	}
	return diffRemoved(filterEvidenceByIDs(previous, previousIDs), filterEvidenceByIDs(current, currentIDs), func(value store.Evidence) string { return value.ID })
}

func buildNextNode(nodeName core.NodeName, snapshots []store.NodeSnapshot, statuses []collector.NodeStatus) (Node, bool) {
	snapshot, ok := findNodeSnapshot(snapshots, nodeName)
	if !ok {
		return Node{}, false
	}
	status, _ := findNodeStatus(statuses, nodeName)
	return buildNode(snapshot, status), true
}

func replaceNodeByName(current []Node, nextNode Node) []Node {
	next := make([]Node, 0, len(current)+1)
	replaced := false
	for _, node := range current {
		if node.Name == nextNode.Name {
			next = append(next, nextNode)
			replaced = true
			continue
		}
		next = append(next, node)
	}
	if !replaced {
		next = append(next, nextNode)
	}
	sort.Slice(next, func(i, j int) bool { return next[i].Name < next[j].Name })
	return next
}

func replaceServicesByIDs(current []store.Service, replaceIDs map[string]struct{}, projected []store.Service) []store.Service {
	next := make([]store.Service, 0, len(current)+len(projected))
	for _, service := range current {
		if _, ok := replaceIDs[service.ID]; ok {
			continue
		}
		next = append(next, service)
	}
	for _, service := range projected {
		if _, ok := replaceIDs[service.ID]; !ok {
			continue
		}
		next = append(next, service)
	}
	sort.Slice(next, func(i, j int) bool {
		if next[i].PrimaryNode != next[j].PrimaryNode {
			return next[i].PrimaryNode < next[j].PrimaryNode
		}
		if next[i].Name != next[j].Name {
			return next[i].Name < next[j].Name
		}
		return next[i].ID < next[j].ID
	})
	return next
}

func replaceRuntimesByIDs(current []store.Runtime, replaceIDs map[string]struct{}, projected []store.Runtime) []store.Runtime {
	next := make([]store.Runtime, 0, len(current)+len(projected))
	for _, runtime := range current {
		if _, ok := replaceIDs[runtime.ID]; ok {
			continue
		}
		next = append(next, runtime)
	}
	for _, runtime := range projected {
		if _, ok := replaceIDs[runtime.ID]; !ok {
			continue
		}
		next = append(next, runtime)
	}
	sort.Slice(next, func(i, j int) bool {
		if next[i].NodeID != next[j].NodeID {
			return next[i].NodeID < next[j].NodeID
		}
		if next[i].RuntimeName != next[j].RuntimeName {
			return next[i].RuntimeName < next[j].RuntimeName
		}
		return next[i].ID < next[j].ID
	})
	return next
}

func replaceExposuresByIDs(current []store.Exposure, replaceIDs map[string]struct{}, projected []store.Exposure) []store.Exposure {
	next := make([]store.Exposure, 0, len(current)+len(projected))
	for _, exposure := range current {
		if _, ok := replaceIDs[exposure.ID]; ok {
			continue
		}
		next = append(next, exposure)
	}
	for _, exposure := range projected {
		if _, ok := replaceIDs[exposure.ID]; !ok {
			continue
		}
		next = append(next, exposure)
	}
	sort.Slice(next, func(i, j int) bool {
		if next[i].NodeID != next[j].NodeID {
			return next[i].NodeID < next[j].NodeID
		}
		if next[i].Port != next[j].Port {
			return next[i].Port < next[j].Port
		}
		if next[i].Hostname != next[j].Hostname {
			return next[i].Hostname < next[j].Hostname
		}
		return next[i].ID < next[j].ID
	})
	return next
}

func collectProjectedNodeScope(nodeName core.NodeName, inventory resolver.TopologyData, routes resolver.TopologyData) ScopedIDs {
	scope := newScopedIDs()
	for _, service := range inventory.Services {
		if service.PrimaryNode == nodeName {
			scope.ServiceIDs[service.ID] = struct{}{}
		}
	}
	for _, runtime := range inventory.Runtimes {
		if runtime.NodeID != nodeName {
			continue
		}
		scope.RuntimeIDs[runtime.ID] = struct{}{}
		if runtime.ServiceID != "" {
			scope.ServiceIDs[runtime.ServiceID] = struct{}{}
		}
	}
	for _, exposure := range inventory.Exposures {
		if exposure.NodeID != nodeName {
			continue
		}
		scope.ExposureIDs[exposure.ID] = struct{}{}
		if exposure.ServiceID != "" {
			scope.ServiceIDs[exposure.ServiceID] = struct{}{}
		}
		if exposure.RuntimeID != "" {
			scope.RuntimeIDs[exposure.RuntimeID] = struct{}{}
		}
	}
	for _, route := range routes.Routes {
		scope.RouteIDs[route.ID] = struct{}{}
		if route.SourceServiceID != "" {
			scope.ServiceIDs[route.SourceServiceID] = struct{}{}
		}
		if route.TargetServiceID != "" {
			scope.ServiceIDs[route.TargetServiceID] = struct{}{}
		}
		if route.SourceExposureID != "" {
			scope.ExposureIDs[route.SourceExposureID] = struct{}{}
		}
		if route.TargetRuntimeID != "" {
			scope.RuntimeIDs[route.TargetRuntimeID] = struct{}{}
		}
	}
	for _, hop := range routes.RouteHops {
		scope.HopIDs[hop.ID] = struct{}{}
	}
	for _, evidence := range routes.Evidence {
		scope.EvidenceIDs[evidence.ID] = struct{}{}
	}
	return scope
}

func collectRouteSourceNodes(snapshot Snapshot, routeIDs map[string]struct{}) []core.NodeName {
	if len(routeIDs) == 0 {
		return nil
	}
	exposureNodeByID := make(map[string]core.NodeName, len(snapshot.Exposures))
	for _, exposure := range snapshot.Exposures {
		exposureNodeByID[exposure.ID] = exposure.NodeID
	}
	nodes := make([]core.NodeName, 0)
	seen := make(map[core.NodeName]struct{})
	for _, route := range snapshot.Routes {
		if _, ok := routeIDs[route.ID]; !ok {
			continue
		}
		nodeName := exposureNodeByID[route.SourceExposureID]
		if nodeName == "" {
			continue
		}
		if _, ok := seen[nodeName]; ok {
			continue
		}
		seen[nodeName] = struct{}{}
		nodes = append(nodes, nodeName)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
	return nodes
}

func hasNodeForwards(snapshots []store.NodeSnapshot, nodeName core.NodeName) bool {
	snapshot, ok := findNodeSnapshot(snapshots, nodeName)
	return ok && len(snapshot.Forwards) > 0
}

func appendUniqueNodeNames(values []core.NodeName, additions ...core.NodeName) []core.NodeName {
	seen := make(map[core.NodeName]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}

func unionStringSets(left, right map[string]struct{}) map[string]struct{} {
	if len(left) == 0 && len(right) == 0 {
		return map[string]struct{}{}
	}
	out := make(map[string]struct{}, len(left)+len(right))
	for id := range left {
		out[id] = struct{}{}
	}
	for id := range right {
		out[id] = struct{}{}
	}
	return out
}

func replaceRoutesByScope(current []store.Route, removeIDs map[string]struct{}, additions []store.Route) []store.Route {
	next := make([]store.Route, 0, len(current)+len(additions))
	for _, route := range current {
		if _, ok := removeIDs[route.ID]; ok {
			continue
		}
		next = append(next, route)
	}
	next = append(next, additions...)
	sort.Slice(next, func(i, j int) bool {
		if next[i].DisplayName != next[j].DisplayName {
			return next[i].DisplayName < next[j].DisplayName
		}
		return next[i].ID < next[j].ID
	})
	return next
}

func replaceRouteHopsByScope(current []store.RouteHop, removeIDs map[string]struct{}, additions []store.RouteHop) []store.RouteHop {
	next := make([]store.RouteHop, 0, len(current)+len(additions))
	for _, hop := range current {
		if _, ok := removeIDs[hop.ID]; ok {
			continue
		}
		next = append(next, hop)
	}
	next = append(next, additions...)
	sort.Slice(next, func(i, j int) bool {
		if next[i].RouteID != next[j].RouteID {
			return next[i].RouteID < next[j].RouteID
		}
		if next[i].Order != next[j].Order {
			return next[i].Order < next[j].Order
		}
		return next[i].ID < next[j].ID
	})
	return next
}

func replaceEvidenceByScope(current []store.Evidence, removeIDs map[string]struct{}, additions []store.Evidence) []store.Evidence {
	next := make([]store.Evidence, 0, len(current)+len(additions))
	for _, evidence := range current {
		if _, ok := removeIDs[evidence.ID]; ok {
			continue
		}
		next = append(next, evidence)
	}
	next = append(next, additions...)
	sort.Slice(next, func(i, j int) bool {
		if next[i].MatchedBy != next[j].MatchedBy {
			return next[i].MatchedBy < next[j].MatchedBy
		}
		if next[i].RawValue != next[j].RawValue {
			return next[i].RawValue < next[j].RawValue
		}
		return next[i].ID < next[j].ID
	})
	return next
}

func summarizeSnapshot(snapshot Snapshot) store.TopologySummary {
	summary := store.TopologySummary{
		NodeCount:     len(snapshot.Nodes),
		ServiceCount:  len(snapshot.Services),
		RuntimeCount:  len(snapshot.Runtimes),
		ExposureCount: len(snapshot.Exposures),
		RouteCount:    len(snapshot.Routes),
	}
	for _, route := range snapshot.Routes {
		if !route.Resolved {
			summary.UnresolvedRouteCount++
		}
	}
	return summary
}

func currentUpdatedAt(snapshots []store.NodeSnapshot) core.Timestamp {
	updatedAt := core.Timestamp{}
	for _, snapshot := range snapshots {
		if updatedAt.IsZero() || snapshot.CollectedAt.Time().After(updatedAt.Time()) {
			updatedAt = snapshot.CollectedAt
		}
	}
	if updatedAt.IsZero() {
		return core.NowTimestamp()
	}
	return updatedAt
}

func buildEntityState(snapshot Snapshot) entityState {
	state := entityState{
		nodesByName:   make(map[core.NodeName]Node, len(snapshot.Nodes)),
		servicesByID:  make(map[core.ID]store.Service, len(snapshot.Services)),
		runtimesByID:  make(map[core.ID]store.Runtime, len(snapshot.Runtimes)),
		exposuresByID: make(map[core.ID]store.Exposure, len(snapshot.Exposures)),
		routesByID:    make(map[core.ID]store.Route, len(snapshot.Routes)),
		routeHopsByID: make(map[core.ID]store.RouteHop, len(snapshot.RouteHops)),
		evidenceByID:  make(map[core.ID]store.Evidence, len(snapshot.Evidence)),
	}
	for _, node := range snapshot.Nodes {
		state.nodesByName[node.Name] = node
	}
	for _, service := range snapshot.Services {
		state.servicesByID[service.ID] = service
	}
	for _, runtime := range snapshot.Runtimes {
		state.runtimesByID[runtime.ID] = runtime
	}
	for _, exposure := range snapshot.Exposures {
		state.exposuresByID[exposure.ID] = exposure
	}
	for _, route := range snapshot.Routes {
		state.routesByID[route.ID] = route
	}
	for _, hop := range snapshot.RouteHops {
		state.routeHopsByID[hop.ID] = hop
	}
	for _, evidence := range snapshot.Evidence {
		state.evidenceByID[evidence.ID] = evidence
	}
	return state
}

func diffNodeNames(previous, current []Node) []core.NodeName {
	names := make(map[core.NodeName]struct{})
	for _, node := range diffUpserted(previous, current, func(value Node) string { return value.Name }) {
		names[node.Name] = struct{}{}
	}
	for _, nodeName := range diffRemoved(previous, current, func(value Node) string { return value.Name }) {
		names[core.NodeName(nodeName)] = struct{}{}
	}
	out := make([]core.NodeName, 0, len(names))
	for nodeName := range names {
		out = append(out, nodeName)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func diffUpserted[T any](previous, current []T, key func(T) string) []T {
	previousByKey := make(map[string]T, len(previous))
	for _, value := range previous {
		previousByKey[key(value)] = value
	}
	out := make([]T, 0)
	for _, value := range current {
		currentKey := key(value)
		if previousValue, ok := previousByKey[currentKey]; ok && reflect.DeepEqual(previousValue, value) {
			continue
		}
		out = append(out, value)
	}
	return out
}

func diffRemoved[T any](previous, current []T, key func(T) string) []string {
	currentByKey := make(map[string]struct{}, len(current))
	for _, value := range current {
		currentByKey[key(value)] = struct{}{}
	}
	out := make([]string, 0)
	for _, value := range previous {
		currentKey := key(value)
		if _, ok := currentByKey[currentKey]; ok {
			continue
		}
		out = append(out, currentKey)
	}
	sort.Strings(out)
	return out
}

func (p Patch) isEmpty() bool {
	return len(p.ChangedNodes) == 0 &&
		len(p.NodesUpserted) == 0 &&
		len(p.NodesRemoved) == 0 &&
		len(p.ServicesUpserted) == 0 &&
		len(p.ServicesRemoved) == 0 &&
		len(p.RuntimesUpserted) == 0 &&
		len(p.RuntimesRemoved) == 0 &&
		len(p.ExposuresUpserted) == 0 &&
		len(p.ExposuresRemoved) == 0 &&
		len(p.RoutesUpserted) == 0 &&
		len(p.RoutesRemoved) == 0 &&
		len(p.RouteHopsUpserted) == 0 &&
		len(p.RouteHopsRemoved) == 0 &&
		len(p.EvidenceUpserted) == 0 &&
		len(p.EvidenceRemoved) == 0
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Nodes = slices.Clone(snapshot.Nodes)
	snapshot.Services = slices.Clone(snapshot.Services)
	snapshot.Runtimes = slices.Clone(snapshot.Runtimes)
	snapshot.Exposures = slices.Clone(snapshot.Exposures)
	snapshot.Routes = slices.Clone(snapshot.Routes)
	snapshot.RouteHops = slices.Clone(snapshot.RouteHops)
	snapshot.Evidence = slices.Clone(snapshot.Evidence)
	return snapshot
}
