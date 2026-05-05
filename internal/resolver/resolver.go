package resolver

import (
	"net"
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
	ByNodeName    map[string]core.NodeName
	ByPort        map[uint16][]IndexedPort
	ByContainer   map[string][]IndexedPort
	ByService     map[string][]IndexedPort
	NodeState     map[core.NodeName]IndexedNodeState
}

type IndexedPort struct {
	NodeName      core.NodeName
	Port          uint16
	Process       string
	ContainerName string
	ServiceName   string
}

type targetDetails struct {
	Process          string
	Container        string
	Service          string
	RuntimeNode      core.NodeName
	RuntimeContainer string
}

type IndexedNodeState struct {
	HasServiceInventory bool
}

// EdgeDiff describes changes between edge sets.
type EdgeDiff struct {
	Added   []store.TopologyEdge `json:"added"`
	Removed []store.TopologyEdge `json:"removed"`
	Changed []store.TopologyEdge `json:"changed"`
}

// BuildIndex constructs lookup indexes from snapshots.
func BuildIndex(snapshots []store.NodeSnapshot) NodeIndex {
	index := NodeIndex{
		ByTailscaleIP: make(map[string]core.NodeName),
		ByLANIP:       make(map[string]core.NodeName),
		ByNodeName:    make(map[string]core.NodeName),
		ByPort:        make(map[uint16][]IndexedPort),
		ByContainer:   make(map[string][]IndexedPort),
		ByService:     make(map[string][]IndexedPort),
		NodeState:     make(map[core.NodeName]IndexedNodeState),
	}

	for _, snapshot := range snapshots {
		for _, key := range nodeLookupKeys(snapshot) {
			if key == "" {
				continue
			}
			index.ByNodeName[key] = snapshot.NodeName
		}
		if snapshot.TailscaleIP != "" {
			index.ByTailscaleIP[snapshot.TailscaleIP] = snapshot.NodeName
		}
		for _, ip := range collectLANIPs(snapshot) {
			if ip == "" || ip == snapshot.TailscaleIP {
				continue
			}
			index.ByLANIP[ip] = snapshot.NodeName
		}
		index.NodeState[snapshot.NodeName] = IndexedNodeState{
			HasServiceInventory: len(snapshot.Ports) > 0 || len(snapshot.Containers) > 0 || len(snapshot.Services) > 0,
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
			for _, publish := range directContainerPublishes(container) {
				indexed := IndexedPort{
					NodeName:      snapshot.NodeName,
					Port:          publish.HostPort,
					ContainerName: container.ContainerName,
				}
				index.ByPort[publish.HostPort] = append(index.ByPort[publish.HostPort], indexed)
				if container.ContainerName != "" {
					key := strings.ToLower(container.ContainerName)
					index.ByContainer[key] = append(index.ByContainer[key], indexed)
				}
			}
			for _, publish := range serviceContainerPublishes(container) {
				index.ByPort[publish.HostPort] = append(index.ByPort[publish.HostPort], IndexedPort{
					NodeName:      snapshot.NodeName,
					Port:          publish.HostPort,
					ContainerName: container.ContainerName,
					ServiceName:   container.ServiceName,
				})
			}
		}
		for _, service := range snapshot.Services {
			indexed := IndexedPort{
				NodeName:    snapshot.NodeName,
				Port:        service.HostPort,
				ServiceName: service.ServiceName,
			}
			index.ByPort[service.HostPort] = append(index.ByPort[service.HostPort], indexed)
			if service.ServiceName != "" {
				key := strings.ToLower(service.ServiceName)
				index.ByService[key] = append(index.ByService[key], indexed)
			}
		}
	}

	return index
}

// ResolveEdges resolves the current edge set for an arbitrary snapshot group.
func ResolveEdges(runID core.ID, snapshots []store.NodeSnapshot) []store.TopologyEdge {
	index := BuildIndex(snapshots)
	return resolveSnapshots(runID, snapshots, index)
}

// ResolveTarget resolves a normalized parser target against the snapshot index.
func ResolveTarget(target parser.ForwardTarget, index NodeIndex) (core.NodeName, uint16, bool) {
	if target.Kind != parser.TargetKindAddress || target.Port == 0 {
		return "", target.Port, false
	}

	if target.Host != "" {
		if node, ok := resolveHost(target.Host, target.Port, index); ok {
			return resolveKnownNode(node, target.Port, index)
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

	diff := EdgeDiff{
		Added:   []store.TopologyEdge{},
		Removed: []store.TopologyEdge{},
		Changed: []store.TopologyEdge{},
	}
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
		for _, publish := range directContainerPublishes(container) {
			edge := store.TopologyEdge{
				ID:          core.NewID(),
				RunID:       runID,
				FromNode:    snapshot.NodeName,
				FromPort:    publish.HostPort,
				ToNode:      snapshot.NodeName,
				ToPort:      publish.TargetPort,
				ToContainer: container.ContainerName,
				Kind:        store.EdgeKindContainerPublish,
				Resolved:    true,
				RawUpstream: container.ContainerName,
			}
			key := edgePublishKey(edge)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			edges = append(edges, edge)
		}
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
			if details := targetMetadata(index, toNode, toPort); details.Process != "" || details.Container != "" || details.Service != "" {
				edge.ToProcess = details.Process
				edge.ToContainer = details.Container
				edge.ToService = details.Service
				edge.ToRuntimeNode = details.RuntimeNode
				edge.ToRuntimeContainer = details.RuntimeContainer
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
		return resolveKnownNode(snapshot.NodeName, target.Port, index)
	}

	if target.Host != "" {
		if node, ok := resolveHost(target.Host, target.Port, index); ok {
			return resolveKnownNode(node, target.Port, index)
		}
	}

	if candidates := index.ByPort[target.Port]; len(candidates) == 1 {
		return candidates[0].NodeName, target.Port, true
	}
	return "", target.Port, false
}

func resolveHost(host string, port uint16, index NodeIndex) (core.NodeName, bool) {
	if node, ok := index.ByTailscaleIP[host]; ok {
		return node, true
	}
	if node, ok := index.ByLANIP[host]; ok {
		return node, true
	}
	for _, key := range hostLookupKeys(host) {
		if node, ok := index.ByNodeName[key]; ok {
			return node, true
		}
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
	if candidates, ok := index.ByService[strings.ToLower(host)]; ok {
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

func resolveKnownNode(nodeName core.NodeName, port uint16, index NodeIndex) (core.NodeName, uint16, bool) {
	if nodeName == "" {
		return "", port, false
	}
	if port == 0 || nodeHasPort(nodeName, port, index) || !nodeHasServiceInventory(nodeName, index) {
		return nodeName, port, true
	}
	return "", port, false
}

func isLocalHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1", "":
		return true
	default:
		return false
	}
}

func targetMetadata(index NodeIndex, nodeName core.NodeName, port uint16) targetDetails {
	var process string
	var service string
	for _, candidate := range index.ByPort[port] {
		if candidate.NodeName != nodeName {
			continue
		}
		if candidate.ContainerName != "" {
			return targetDetails{
				Process:          candidate.Process,
				Container:        candidate.ContainerName,
				Service:          candidate.ServiceName,
				RuntimeNode:      candidate.NodeName,
				RuntimeContainer: candidate.ContainerName,
			}
		}
		if service == "" && candidate.ServiceName != "" {
			service = candidate.ServiceName
		}
		if process == "" {
			process = candidate.Process
		}
	}
	runtimeNode, runtimeContainer := findServiceRuntime(index, service, port)
	return targetDetails{
		Process:          process,
		Service:          service,
		RuntimeNode:      runtimeNode,
		RuntimeContainer: runtimeContainer,
	}
}

func directContainerPublishes(container store.Container) []store.ContainerPublishedPort {
	if len(container.PublishedPorts) == 0 {
		return nil
	}
	out := make([]store.ContainerPublishedPort, 0, len(container.PublishedPorts))
	for _, publish := range container.PublishedPorts {
		if publish.Source != "container" {
			continue
		}
		out = append(out, publish)
	}
	return out
}

func serviceContainerPublishes(container store.Container) []store.ContainerPublishedPort {
	if len(container.PublishedPorts) == 0 || strings.TrimSpace(container.ServiceName) == "" {
		return nil
	}
	out := make([]store.ContainerPublishedPort, 0, len(container.PublishedPorts))
	for _, publish := range container.PublishedPorts {
		if publish.Source != "service" {
			continue
		}
		out = append(out, publish)
	}
	return out
}

func findServiceRuntime(index NodeIndex, serviceName string, port uint16) (core.NodeName, string) {
	serviceName = strings.ToLower(strings.TrimSpace(serviceName))
	if serviceName == "" || port == 0 {
		return "", ""
	}

	var (
		runtimeNode      core.NodeName
		runtimeContainer string
	)
	for _, candidate := range index.ByPort[port] {
		if candidate.ContainerName == "" {
			continue
		}
		if strings.ToLower(strings.TrimSpace(candidate.ServiceName)) != serviceName {
			continue
		}
		if runtimeNode == "" && runtimeContainer == "" {
			runtimeNode = candidate.NodeName
			runtimeContainer = candidate.ContainerName
			continue
		}
		if runtimeNode != candidate.NodeName || runtimeContainer != candidate.ContainerName {
			return "", ""
		}
	}
	return runtimeNode, runtimeContainer
}

func portProcess(ports []store.ListenPort, listenPort uint16) string {
	for _, port := range ports {
		if port.Port == listenPort {
			return port.Process
		}
	}
	return ""
}

func nodeLookupKeys(snapshot store.NodeSnapshot) []string {
	keys := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	add := func(value string) {
		value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		keys = append(keys, value)
	}

	add(string(snapshot.NodeName))
	add(snapshot.DNSName)
	if base, _, ok := strings.Cut(strings.TrimSuffix(snapshot.DNSName, "."), "."); ok {
		add(base)
	}
	// Common short target alias used in local proxy configs.
	add(string(snapshot.NodeName) + "-t")
	return keys
}

func hostLookupKeys(host string) []string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return nil
	}

	keys := make([]string, 0, 2)
	seen := make(map[string]struct{}, 4)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		keys = append(keys, value)
	}

	add(host)
	if firstLabel, _, ok := strings.Cut(host, "."); ok && shouldUseShortHostAlias(firstLabel) {
		add(firstLabel)
	}
	return keys
}

func shouldUseShortHostAlias(label string) bool {
	label = strings.TrimSpace(label)
	if label == "" {
		return false
	}
	hasHyphen := false
	hasDigit := false
	for _, r := range label {
		if r == '-' {
			hasHyphen = true
			continue
		}
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
	}
	return hasHyphen || hasDigit
}

func collectLANIPs(snapshot store.NodeSnapshot) []string {
	if len(snapshot.Ports) == 0 {
		return nil
	}
	out := make([]string, 0, len(snapshot.Ports))
	seen := make(map[string]struct{}, len(snapshot.Ports))
	for _, port := range snapshot.Ports {
		addr := strings.TrimSpace(port.Addr)
		if !isUsableNodeIP(addr) {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	return out
}

func isUsableNodeIP(value string) bool {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsUnspecified() {
		return false
	}
	return true
}

func nodeHasPort(nodeName core.NodeName, port uint16, index NodeIndex) bool {
	for _, candidate := range index.ByPort[port] {
		if candidate.NodeName == nodeName {
			return true
		}
	}
	return false
}

func nodeHasServiceInventory(nodeName core.NodeName, index NodeIndex) bool {
	state, ok := index.NodeState[nodeName]
	return ok && state.HasServiceInventory
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
		a.ToService == b.ToService &&
		a.ToRuntimeNode == b.ToRuntimeNode &&
		a.ToRuntimeContainer == b.ToRuntimeContainer &&
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

func edgePublishKey(edge store.TopologyEdge) string {
	return strings.Join([]string{
		string(edge.FromNode),
		strconv.FormatUint(uint64(edge.FromPort), 10),
		string(edge.ToNode),
		strconv.FormatUint(uint64(edge.ToPort), 10),
		edge.ToContainer,
		edge.ToService,
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
			edge.ToService,
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
