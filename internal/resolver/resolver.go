package resolver

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/store"
)

// NodeIndex is the lookup table built from snapshots in one run.
type NodeIndex struct {
	ByTailscaleIP map[string]core.NodeName
	ByLANIP       map[string]core.NodeName
	ByPort        map[uint16][]IndexedPort
	ByContainer   map[string][]IndexedPort
}

type IndexedPort struct {
	NodeName      core.NodeName
	Port          uint16
	Process       string
	ContainerName string
}

// EdgeEvent is emitted after topology resolution changes.
type EdgeEvent struct {
	RunID core.ID              `json:"run_id"`
	Edges []store.TopologyEdge `json:"edges"`
	Diff  EdgeDiff             `json:"diff"`
}

// EdgeDiff describes changes between edge sets.
type EdgeDiff struct {
	Added   []store.TopologyEdge `json:"added"`
	Removed []store.TopologyEdge `json:"removed"`
	Changed []store.TopologyEdge `json:"changed"`
}

// Resolver produces topology edges from stored snapshots.
type Resolver struct {
	edges     store.EdgeStore
	snapshots store.SnapshotStore
	bus       *core.EventBus
}

func NewResolver(edges store.EdgeStore, snapshots store.SnapshotStore, bus *core.EventBus) *Resolver {
	return &Resolver{edges: edges, snapshots: snapshots, bus: bus}
}

// ResolveRun builds and persists the full edge set for a completed run.
func (r *Resolver) ResolveRun(ctx context.Context, run store.CollectionRun, snapshots []store.NodeSnapshot) ([]store.TopologyEdge, error) {
	index := BuildIndex(snapshots)
	current := resolveSnapshots(run.ID, snapshots, index)

	var previous []store.TopologyEdge
	if r.edges != nil {
		var err error
		previous, err = r.edges.LatestEdges(ctx)
		if err != nil {
			return nil, err
		}
		for _, edge := range current {
			if err := r.edges.Save(ctx, edge); err != nil {
				return nil, err
			}
		}
	}

	diff := DiffEdges(previous, current)
	r.publishDiff(run.ID, current, diff)
	core.BroadcastEvent(r.bus, "topology.run_completed", run)
	return current, nil
}

// ResolveSnapshot recomputes only the edges involving one node and replaces them in the latest run.
func (r *Resolver) ResolveSnapshot(ctx context.Context, snapshot store.NodeSnapshot) ([]store.TopologyEdge, error) {
	if r.snapshots == nil || r.edges == nil {
		index := BuildIndex([]store.NodeSnapshot{snapshot})
		return filterEdgesForNode(resolveSnapshots(snapshot.RunID, []store.NodeSnapshot{snapshot}, index), snapshot.NodeName), nil
	}

	currentSnapshots, err := latestSnapshots(ctx, r.snapshots)
	if err != nil {
		return nil, err
	}
	currentSnapshots[snapshot.NodeName] = snapshot

	all := make([]store.NodeSnapshot, 0, len(currentSnapshots))
	for _, snap := range currentSnapshots {
		all = append(all, snap)
	}
	index := BuildIndex(all)
	recomputed := filterEdgesForNode(resolveSnapshots(snapshot.RunID, all, index), snapshot.NodeName)

	previous, err := r.edges.List(ctx, core.Filter{NodeName: snapshot.NodeName})
	if err != nil {
		return nil, err
	}
	for _, edge := range previous {
		if err := r.edges.Delete(ctx, edge.ID); err != nil {
			return nil, err
		}
	}
	for _, edge := range recomputed {
		if err := r.edges.Save(ctx, edge); err != nil {
			return nil, err
		}
	}

	diff := DiffEdges(previous, recomputed)
	r.publishDiff(snapshot.RunID, recomputed, diff)
	return recomputed, nil
}

// BuildIndex constructs lookup indexes from snapshots.
func BuildIndex(snapshots []store.NodeSnapshot) NodeIndex {
	index := NodeIndex{
		ByTailscaleIP: make(map[string]core.NodeName),
		ByLANIP:       make(map[string]core.NodeName),
		ByPort:        make(map[uint16][]IndexedPort),
		ByContainer:   make(map[string][]IndexedPort),
	}

	for _, snapshot := range snapshots {
		if snapshot.TailscaleIP != "" {
			index.ByTailscaleIP[snapshot.TailscaleIP] = snapshot.NodeName
		}
		for _, port := range snapshot.Ports {
			indexed := IndexedPort{
				NodeName: snapshot.NodeName,
				Port:     port.Port,
				Process:  port.Process,
			}
			index.ByPort[port.Port] = append(index.ByPort[port.Port], indexed)
		}
		for _, container := range snapshot.Containers {
			indexed := IndexedPort{
				NodeName:      snapshot.NodeName,
				Port:          container.HostPort,
				ContainerName: container.ContainerName,
			}
			index.ByPort[container.HostPort] = append(index.ByPort[container.HostPort], indexed)
			if container.ContainerName != "" {
				key := strings.ToLower(container.ContainerName)
				index.ByContainer[key] = append(index.ByContainer[key], indexed)
			}
		}
	}

	return index
}

// ResolveTarget resolves a normalized parser target against the snapshot index.
func ResolveTarget(target parser.ForwardTarget, index NodeIndex) (core.NodeName, uint16, bool) {
	if target.Kind != parser.TargetKindAddress || target.Port == 0 {
		return "", target.Port, false
	}

	if target.Host != "" {
		if node, ok := resolveHost(target.Host, target.Port, index); ok {
			return node, target.Port, true
		}
	}
	if candidates := index.ByPort[target.Port]; len(candidates) == 1 {
		return candidates[0].NodeName, target.Port, true
	}
	return "", target.Port, false
}

// DiffEdges compares two edge sets and classifies additions, removals, and changes.
func DiffEdges(previous, current []store.TopologyEdge) EdgeDiff {
	prevByKey := make(map[string]store.TopologyEdge, len(previous))
	curByKey := make(map[string]store.TopologyEdge, len(current))
	for _, edge := range previous {
		prevByKey[edgeKey(edge)] = edge
	}
	for _, edge := range current {
		curByKey[edgeKey(edge)] = edge
	}

	var diff EdgeDiff
	for key, currentEdge := range curByKey {
		if previousEdge, ok := prevByKey[key]; !ok {
			diff.Added = append(diff.Added, currentEdge)
		} else if !edgesEquivalent(previousEdge, currentEdge) {
			diff.Changed = append(diff.Changed, currentEdge)
		}
	}
	for key, previousEdge := range prevByKey {
		if _, ok := curByKey[key]; !ok {
			diff.Removed = append(diff.Removed, previousEdge)
		}
	}

	sortEdges(diff.Added)
	sortEdges(diff.Removed)
	sortEdges(diff.Changed)
	return diff
}

func resolveSnapshots(runID core.ID, snapshots []store.NodeSnapshot, index NodeIndex) []store.TopologyEdge {
	edges := make([]store.TopologyEdge, 0)
	for _, snapshot := range snapshots {
		edges = append(edges, resolveContainerEdges(runID, snapshot)...)
		edges = append(edges, resolveProxyEdges(runID, snapshot, index)...)
	}
	sortEdges(edges)
	return edges
}

func resolveContainerEdges(runID core.ID, snapshot store.NodeSnapshot) []store.TopologyEdge {
	edges := make([]store.TopologyEdge, 0, len(snapshot.Containers))
	seen := make(map[string]struct{})
	for _, container := range snapshot.Containers {
		edge := store.TopologyEdge{
			ID:          core.NewID(),
			RunID:       runID,
			FromNode:    snapshot.NodeName,
			FromPort:    container.HostPort,
			ToNode:      snapshot.NodeName,
			ToPort:      container.ContainerPort,
			ToContainer: container.ContainerName,
			Kind:        store.EdgeKindContainerPublish,
			Resolved:    true,
			RawUpstream: container.ContainerName,
		}
		key := edgeContainerPublishKey(edge)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		edges = append(edges, edge)
	}
	return edges
}

func resolveProxyEdges(runID core.ID, snapshot store.NodeSnapshot, index NodeIndex) []store.TopologyEdge {
	edges := make([]store.TopologyEdge, 0, len(snapshot.Forwards))
	for _, forward := range snapshot.Forwards {
		edge := store.TopologyEdge{
			ID:          core.NewID(),
			RunID:       runID,
			FromNode:    snapshot.NodeName,
			FromPort:    forward.Listener.Port,
			Kind:        store.EdgeKindProxyPass,
			RawUpstream: forward.Target.Raw,
		}

		if source := portProcess(snapshot.Ports, forward.Listener.Port); source != "" {
			edge.FromProcess = source
		}

		toNode, toPort, resolved := resolveForSource(snapshot, forward.Target, index)
		edge.ToNode = toNode
		edge.ToPort = toPort
		edge.Resolved = resolved
		if resolved {
			if process, container := targetMetadata(index, toNode, toPort); process != "" || container != "" {
				edge.ToProcess = process
				edge.ToContainer = container
			}
		}
		edges = append(edges, edge)
	}
	sortEdges(edges)
	return dedupeProxyEdges(edges)
}

func resolveForSource(snapshot store.NodeSnapshot, target parser.ForwardTarget, index NodeIndex) (core.NodeName, uint16, bool) {
	if target.Kind != parser.TargetKindAddress {
		return "", target.Port, false
	}

	if isLocalHost(target.Host) {
		for _, candidate := range index.ByPort[target.Port] {
			if candidate.NodeName == snapshot.NodeName {
				return candidate.NodeName, target.Port, true
			}
		}
		return snapshot.NodeName, target.Port, true
	}

	if target.Host != "" {
		if node, ok := resolveHost(target.Host, target.Port, index); ok {
			return node, target.Port, true
		}
	}

	if candidates := index.ByPort[target.Port]; len(candidates) == 1 {
		return candidates[0].NodeName, target.Port, true
	}
	return "", target.Port, false
}

func resolveHost(host string, port uint16, index NodeIndex) (core.NodeName, bool) {
	if node, ok := index.ByTailscaleIP[host]; ok {
		return resolveNodePort(node, port, index)
	}
	if node, ok := index.ByLANIP[host]; ok {
		return resolveNodePort(node, port, index)
	}
	if candidates, ok := index.ByContainer[strings.ToLower(host)]; ok {
		if len(candidates) == 1 {
			return candidates[0].NodeName, true
		}
		for _, candidate := range candidates {
			if candidate.Port == port {
				return candidate.NodeName, true
			}
		}
	}
	return "", false
}

func resolveNodePort(nodeName core.NodeName, port uint16, index NodeIndex) (core.NodeName, bool) {
	if port == 0 {
		return nodeName, true
	}
	for _, candidate := range index.ByPort[port] {
		if candidate.NodeName == nodeName {
			return nodeName, true
		}
	}
	return "", false
}

func isLocalHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1", "":
		return true
	default:
		return false
	}
}

func targetMetadata(index NodeIndex, nodeName core.NodeName, port uint16) (string, string) {
	for _, candidate := range index.ByPort[port] {
		if candidate.NodeName != nodeName {
			continue
		}
		return candidate.Process, candidate.ContainerName
	}
	return "", ""
}

func portProcess(ports []store.ListenPort, listenPort uint16) string {
	for _, port := range ports {
		if port.Port == listenPort {
			return port.Process
		}
	}
	return ""
}

func latestSnapshots(ctx context.Context, snapshots store.SnapshotStore) (map[core.NodeName]store.NodeSnapshot, error) {
	list, err := snapshots.List(ctx, core.Filter{})
	if err != nil {
		return nil, err
	}
	latest := make(map[core.NodeName]store.NodeSnapshot)
	for _, snapshot := range list {
		if _, ok := latest[snapshot.NodeName]; !ok {
			latest[snapshot.NodeName] = snapshot
		}
	}
	return latest, nil
}

func filterEdgesForNode(edges []store.TopologyEdge, nodeName core.NodeName) []store.TopologyEdge {
	filtered := make([]store.TopologyEdge, 0)
	for _, edge := range edges {
		if edge.FromNode == nodeName || edge.ToNode == nodeName {
			filtered = append(filtered, edge)
		}
	}
	sortEdges(filtered)
	return filtered
}

func edgeKey(edge store.TopologyEdge) string {
	return strings.Join([]string{
		string(edge.FromNode),
		strconv.FormatUint(uint64(edge.FromPort), 10),
		edge.FromProcess,
		edge.FromContainer,
		string(edge.Kind),
		edge.RawUpstream,
	}, "|")
}

func edgesEquivalent(a, b store.TopologyEdge) bool {
	return a.FromNode == b.FromNode &&
		a.FromPort == b.FromPort &&
		a.FromProcess == b.FromProcess &&
		a.FromContainer == b.FromContainer &&
		a.ToNode == b.ToNode &&
		a.ToPort == b.ToPort &&
		a.ToProcess == b.ToProcess &&
		a.ToContainer == b.ToContainer &&
		a.Kind == b.Kind &&
		a.Resolved == b.Resolved &&
		a.RawUpstream == b.RawUpstream
}

func sortEdges(edges []store.TopologyEdge) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].FromNode != edges[j].FromNode {
			return edges[i].FromNode < edges[j].FromNode
		}
		if edges[i].FromPort != edges[j].FromPort {
			return edges[i].FromPort < edges[j].FromPort
		}
		if edges[i].Kind != edges[j].Kind {
			return edges[i].Kind < edges[j].Kind
		}
		return edges[i].RawUpstream < edges[j].RawUpstream
	})
}

func edgeContainerPublishKey(edge store.TopologyEdge) string {
	return strings.Join([]string{
		string(edge.FromNode),
		strconv.FormatUint(uint64(edge.FromPort), 10),
		string(edge.ToNode),
		strconv.FormatUint(uint64(edge.ToPort), 10),
		edge.ToContainer,
		string(edge.Kind),
		edge.RawUpstream,
	}, "|")
}

func dedupeProxyEdges(edges []store.TopologyEdge) []store.TopologyEdge {
	if len(edges) == 0 {
		return nil
	}
	out := make([]store.TopologyEdge, 0, len(edges))
	seen := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		key := strings.Join([]string{
			string(edge.FromNode),
			strconv.FormatUint(uint64(edge.FromPort), 10),
			edge.FromProcess,
			string(edge.ToNode),
			strconv.FormatUint(uint64(edge.ToPort), 10),
			edge.ToProcess,
			edge.ToContainer,
			string(edge.Kind),
			edge.RawUpstream,
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, edge)
	}
	return out
}

func (r *Resolver) publishDiff(runID core.ID, edges []store.TopologyEdge, diff EdgeDiff) {
	if len(diff.Added) > 0 {
		core.BroadcastEvent(r.bus, "topology.edge_added", EdgeEvent{RunID: runID, Edges: diff.Added, Diff: diff})
	}
	if len(diff.Removed) > 0 {
		core.BroadcastEvent(r.bus, "topology.edge_removed", EdgeEvent{RunID: runID, Edges: diff.Removed, Diff: diff})
	}
	if len(diff.Changed) > 0 {
		core.BroadcastEvent(r.bus, "topology.edge_changed", EdgeEvent{RunID: runID, Edges: diff.Changed, Diff: diff})
	}
	if len(edges) == 0 && len(diff.Added) == 0 && len(diff.Removed) == 0 && len(diff.Changed) == 0 {
		core.BroadcastEvent(r.bus, "topology.edge_changed", EdgeEvent{RunID: runID, Edges: nil, Diff: diff})
	}
}
