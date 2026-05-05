package runtime

import (
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/wf-pro-dev/tailflow/internal/collector"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/store"
)

type InventoryState struct {
	mu          sync.RWMutex
	nodesByName map[core.NodeName]*NodeInventory
}

type NodeInventory struct {
	NodeName          core.NodeName
	TailscaleIP       string
	DNSName           string
	CollectedAt       core.Timestamp
	Status            collector.NodeStatus
	PortsByKey        map[string]store.ListenPort
	ForwardsByKey     map[string]parser.ForwardAction
	ContainersByID    map[string]store.Container
	ServicePortsByKey map[string]store.SwarmServicePort
}

type SnapshotDelta struct {
	TopologyMetadataChanged bool
	PortsChanged            []string
	ForwardsChanged         []string
	ContainersChanged       []string
	ServicePortsChanged     []string
}

func (d SnapshotDelta) Empty() bool {
	return !d.TopologyMetadataChanged &&
		len(d.PortsChanged) == 0 &&
		len(d.ForwardsChanged) == 0 &&
		len(d.ContainersChanged) == 0 &&
		len(d.ServicePortsChanged) == 0
}

func NewInventoryState() *InventoryState {
	return &InventoryState{nodesByName: make(map[core.NodeName]*NodeInventory)}
}

func (s *InventoryState) Reset(snapshots []store.NodeSnapshot, statuses []collector.NodeStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := make(map[core.NodeName]*NodeInventory, len(snapshots))
	for _, snapshot := range snapshots {
		next[snapshot.NodeName] = newNodeInventory(snapshot)
	}
	for _, status := range statuses {
		node := next[status.NodeName]
		if node == nil {
			node = &NodeInventory{
				NodeName:          status.NodeName,
				PortsByKey:        make(map[string]store.ListenPort),
				ForwardsByKey:     make(map[string]parser.ForwardAction),
				ContainersByID:    make(map[string]store.Container),
				ServicePortsByKey: make(map[string]store.SwarmServicePort),
			}
			next[status.NodeName] = node
		}
		node.Status = status
	}
	s.nodesByName = next
}

func (s *InventoryState) ApplySnapshot(snapshot store.NodeSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.nodesByName[snapshot.NodeName]
	status := collector.NodeStatus{}
	if current != nil {
		status = current.Status
	}
	next := newNodeInventory(snapshot)
	next.Status = status
	s.nodesByName[snapshot.NodeName] = next
}

func (s *InventoryState) ApplySnapshotWithDiff(snapshot store.NodeSnapshot) SnapshotDelta {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.nodesByName[snapshot.NodeName]
	status := collector.NodeStatus{}
	if current != nil {
		status = current.Status
	}
	next := newNodeInventory(snapshot)
	next.Status = status

	delta := SnapshotDelta{}
	if current == nil {
		delta.TopologyMetadataChanged = strings.TrimSpace(snapshot.TailscaleIP) != ""
		delta.PortsChanged = mapKeys(next.PortsByKey)
		delta.ForwardsChanged = mapKeys(next.ForwardsByKey)
		delta.ContainersChanged = mapKeys(next.ContainersByID)
		delta.ServicePortsChanged = mapKeys(next.ServicePortsByKey)
		s.nodesByName[snapshot.NodeName] = next
		return delta
	}

	delta.TopologyMetadataChanged = current.TailscaleIP != next.TailscaleIP
	delta.PortsChanged = changedValueMap(current.PortsByKey, next.PortsByKey)
	delta.ForwardsChanged = changedValueMap(current.ForwardsByKey, next.ForwardsByKey)
	delta.ContainersChanged = changedValueMap(current.ContainersByID, next.ContainersByID)
	delta.ServicePortsChanged = changedValueMap(current.ServicePortsByKey, next.ServicePortsByKey)

	s.nodesByName[snapshot.NodeName] = next
	return delta
}

func (s *InventoryState) ApplyStatus(status collector.NodeStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	node := s.ensureNodeLocked(status.NodeName)
	node.Status = status
}

func (s *InventoryState) ReplaceNodePortsWithDiff(nodeName core.NodeName, ports []store.ListenPort) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	node := s.ensureNodeLocked(nodeName)
	previous := make(map[string]struct{}, len(node.PortsByKey))
	for key := range node.PortsByKey {
		previous[key] = struct{}{}
	}

	node.PortsByKey = make(map[string]store.ListenPort, len(ports))
	for _, port := range ports {
		node.PortsByKey[listenPortKey(port)] = port
	}

	current := make(map[string]struct{}, len(node.PortsByKey))
	for key := range node.PortsByKey {
		current[key] = struct{}{}
	}
	return changedStringSet(previous, current)
}

func (s *InventoryState) ReplaceNodeContainersWithDiff(nodeName core.NodeName, containers []store.Container) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	node := s.ensureNodeLocked(nodeName)
	previous := make(map[string]struct{}, len(node.ContainersByID))
	for key := range node.ContainersByID {
		previous[key] = struct{}{}
	}

	node.ContainersByID = make(map[string]store.Container, len(containers))
	for _, container := range containers {
		node.ContainersByID[containerInventoryKey(container)] = container
	}

	current := make(map[string]struct{}, len(node.ContainersByID))
	for key := range node.ContainersByID {
		current[key] = struct{}{}
	}
	return changedStringSet(previous, current)
}

func (s *InventoryState) ReplaceNodeServicePortsWithDiff(nodeName core.NodeName, services []store.SwarmServicePort) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	node := s.ensureNodeLocked(nodeName)
	previous := make(map[string]struct{}, len(node.ServicePortsByKey))
	for key := range node.ServicePortsByKey {
		previous[key] = struct{}{}
	}

	node.ServicePortsByKey = make(map[string]store.SwarmServicePort, len(services))
	for _, servicePort := range services {
		node.ServicePortsByKey[swarmServicePortKey(servicePort)] = servicePort
	}

	current := make(map[string]struct{}, len(node.ServicePortsByKey))
	for key := range node.ServicePortsByKey {
		current[key] = struct{}{}
	}
	return changedStringSet(previous, current)
}

func (s *InventoryState) ReplaceNodeForwardsWithDiff(nodeName core.NodeName, forwards []parser.ForwardAction) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	node := s.ensureNodeLocked(nodeName)
	previous := make(map[string]struct{}, len(node.ForwardsByKey))
	for key := range node.ForwardsByKey {
		previous[key] = struct{}{}
	}

	node.ForwardsByKey = make(map[string]parser.ForwardAction, len(forwards))
	for _, forward := range forwards {
		node.ForwardsByKey[forwardActionKey(forward)] = forward
	}

	current := make(map[string]struct{}, len(node.ForwardsByKey))
	for key := range node.ForwardsByKey {
		current[key] = struct{}{}
	}
	return changedStringSet(previous, current)
}

func (s *InventoryState) UpsertNodePortWithDiff(nodeName core.NodeName, port store.ListenPort) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	node := s.ensureNodeLocked(nodeName)
	key := listenPortKey(port)
	previous, ok := node.PortsByKey[key]
	node.PortsByKey[key] = port
	if ok && previous == port {
		return nil
	}
	return []string{key}
}

func (s *InventoryState) RemoveNodePortWithDiff(nodeName core.NodeName, port store.ListenPort) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	node := s.ensureNodeLocked(nodeName)
	key := listenPortKey(port)
	if _, ok := node.PortsByKey[key]; !ok {
		return nil
	}
	delete(node.PortsByKey, key)
	return []string{key}
}

func (s *InventoryState) SnapshotNode(nodeName core.NodeName) (NodeInventory, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node := s.nodesByName[nodeName]
	if node == nil {
		return NodeInventory{}, false
	}
	return cloneNodeInventory(node), true
}

func (s *InventoryState) Snapshots() []store.NodeSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]core.NodeName, 0, len(s.nodesByName))
	for nodeName := range s.nodesByName {
		names = append(names, nodeName)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })

	out := make([]store.NodeSnapshot, 0, len(names))
	for _, nodeName := range names {
		out = append(out, nodeInventorySnapshot(s.nodesByName[nodeName]))
	}
	return out
}

func (s *InventoryState) ensureNodeLocked(nodeName core.NodeName) *NodeInventory {
	node := s.nodesByName[nodeName]
	if node != nil {
		return node
	}
	node = &NodeInventory{
		NodeName:          nodeName,
		PortsByKey:        make(map[string]store.ListenPort),
		ForwardsByKey:     make(map[string]parser.ForwardAction),
		ContainersByID:    make(map[string]store.Container),
		ServicePortsByKey: make(map[string]store.SwarmServicePort),
	}
	s.nodesByName[nodeName] = node
	return node
}

func newNodeInventory(snapshot store.NodeSnapshot) *NodeInventory {
	node := &NodeInventory{
		NodeName:          snapshot.NodeName,
		TailscaleIP:       snapshot.TailscaleIP,
		DNSName:           snapshot.DNSName,
		CollectedAt:       snapshot.CollectedAt,
		PortsByKey:        make(map[string]store.ListenPort, len(snapshot.Ports)),
		ForwardsByKey:     make(map[string]parser.ForwardAction, len(snapshot.Forwards)),
		ContainersByID:    make(map[string]store.Container, len(snapshot.Containers)),
		ServicePortsByKey: make(map[string]store.SwarmServicePort, len(snapshot.Services)),
	}
	for _, port := range snapshot.Ports {
		node.PortsByKey[listenPortKey(port)] = port
	}
	for _, forward := range snapshot.Forwards {
		node.ForwardsByKey[forwardActionKey(forward)] = forward
	}
	for _, container := range snapshot.Containers {
		node.ContainersByID[containerInventoryKey(container)] = container
	}
	for _, servicePort := range snapshot.Services {
		node.ServicePortsByKey[swarmServicePortKey(servicePort)] = servicePort
	}
	return node
}

func cloneNodeInventory(node *NodeInventory) NodeInventory {
	cloned := NodeInventory{
		NodeName:          node.NodeName,
		TailscaleIP:       node.TailscaleIP,
		DNSName:           node.DNSName,
		CollectedAt:       node.CollectedAt,
		Status:            node.Status,
		PortsByKey:        make(map[string]store.ListenPort, len(node.PortsByKey)),
		ForwardsByKey:     make(map[string]parser.ForwardAction, len(node.ForwardsByKey)),
		ContainersByID:    make(map[string]store.Container, len(node.ContainersByID)),
		ServicePortsByKey: make(map[string]store.SwarmServicePort, len(node.ServicePortsByKey)),
	}
	for key, port := range node.PortsByKey {
		cloned.PortsByKey[key] = port
	}
	for key, forward := range node.ForwardsByKey {
		cloned.ForwardsByKey[key] = forward
	}
	for key, container := range node.ContainersByID {
		cloned.ContainersByID[key] = container
	}
	for key, servicePort := range node.ServicePortsByKey {
		cloned.ServicePortsByKey[key] = servicePort
	}
	return cloned
}

func nodeInventorySnapshot(node *NodeInventory) store.NodeSnapshot {
	snapshot := store.NodeSnapshot{
		NodeName:    node.NodeName,
		TailscaleIP: node.TailscaleIP,
		DNSName:     node.DNSName,
		CollectedAt: node.CollectedAt,
		Ports:       make([]store.ListenPort, 0, len(node.PortsByKey)),
		Forwards:    make([]parser.ForwardAction, 0, len(node.ForwardsByKey)),
		Containers:  make([]store.Container, 0, len(node.ContainersByID)),
		Services:    make([]store.SwarmServicePort, 0, len(node.ServicePortsByKey)),
	}

	for _, port := range node.PortsByKey {
		snapshot.Ports = append(snapshot.Ports, port)
	}
	sort.Slice(snapshot.Ports, func(i, j int) bool {
		if snapshot.Ports[i].Port != snapshot.Ports[j].Port {
			return snapshot.Ports[i].Port < snapshot.Ports[j].Port
		}
		if snapshot.Ports[i].Proto != snapshot.Ports[j].Proto {
			return snapshot.Ports[i].Proto < snapshot.Ports[j].Proto
		}
		if snapshot.Ports[i].Addr != snapshot.Ports[j].Addr {
			return snapshot.Ports[i].Addr < snapshot.Ports[j].Addr
		}
		if snapshot.Ports[i].Process != snapshot.Ports[j].Process {
			return snapshot.Ports[i].Process < snapshot.Ports[j].Process
		}
		return snapshot.Ports[i].PID < snapshot.Ports[j].PID
	})

	for _, forward := range node.ForwardsByKey {
		snapshot.Forwards = append(snapshot.Forwards, forward)
	}
	parser.SortForwards(snapshot.Forwards)

	for _, container := range node.ContainersByID {
		snapshot.Containers = append(snapshot.Containers, container)
	}
	sort.Slice(snapshot.Containers, func(i, j int) bool {
		if snapshot.Containers[i].ContainerName != snapshot.Containers[j].ContainerName {
			return snapshot.Containers[i].ContainerName < snapshot.Containers[j].ContainerName
		}
		return snapshot.Containers[i].ContainerID < snapshot.Containers[j].ContainerID
	})

	for _, servicePort := range node.ServicePortsByKey {
		snapshot.Services = append(snapshot.Services, servicePort)
	}
	sort.Slice(snapshot.Services, func(i, j int) bool {
		if snapshot.Services[i].ServiceName != snapshot.Services[j].ServiceName {
			return snapshot.Services[i].ServiceName < snapshot.Services[j].ServiceName
		}
		if snapshot.Services[i].HostPort != snapshot.Services[j].HostPort {
			return snapshot.Services[i].HostPort < snapshot.Services[j].HostPort
		}
		return snapshot.Services[i].TargetPort < snapshot.Services[j].TargetPort
	})

	return snapshot
}

func listenPortKey(port store.ListenPort) string {
	return strings.Join([]string{
		strings.TrimSpace(strings.ToLower(port.Addr)),
		strconv.FormatUint(uint64(port.Port), 10),
		strings.TrimSpace(strings.ToLower(port.Proto)),
		strconv.Itoa(port.PID),
		strings.TrimSpace(strings.ToLower(port.Process)),
	}, "|")
}

func containerInventoryKey(container store.Container) string {
	if strings.TrimSpace(container.ContainerID) != "" {
		return strings.TrimSpace(container.ContainerID)
	}
	return strings.TrimSpace(container.ContainerName)
}

func swarmServicePortKey(servicePort store.SwarmServicePort) string {
	return strings.Join([]string{
		strings.TrimSpace(servicePort.ServiceID),
		strconv.FormatUint(uint64(servicePort.HostPort), 10),
		strconv.FormatUint(uint64(servicePort.TargetPort), 10),
		strings.TrimSpace(strings.ToLower(servicePort.Proto)),
		strings.TrimSpace(strings.ToLower(servicePort.Mode)),
	}, "|")
}

func forwardActionKey(forward parser.ForwardAction) string {
	hostnames := slices.Clone(forward.Hostnames)
	sort.Strings(hostnames)
	return strings.Join([]string{
		strings.TrimSpace(strings.ToLower(forward.Listener.Addr)),
		strconv.FormatUint(uint64(forward.Listener.Port), 10),
		strings.TrimSpace(strings.ToLower(forward.Target.Kind)),
		strings.TrimSpace(strings.ToLower(forward.Target.Host)),
		strconv.FormatUint(uint64(forward.Target.Port), 10),
		strings.TrimSpace(forward.Target.Socket),
		strings.TrimSpace(forward.Target.Raw),
		strings.Join(hostnames, ","),
	}, "|")
}

func forwardImpactKey(nodeName core.NodeName, forward parser.ForwardAction) string {
	hostnames := slices.Clone(forward.Hostnames)
	sort.Strings(hostnames)
	return strings.Join([]string{
		string(nodeName),
		strconv.FormatUint(uint64(forward.Listener.Port), 10),
		strings.TrimSpace(forward.Target.Raw),
		strings.Join(hostnames, ","),
	}, "|")
}

func changedStringSet(previous, current map[string]struct{}) []string {
	changed := make(map[string]struct{})
	for key := range previous {
		if _, ok := current[key]; !ok {
			changed[key] = struct{}{}
		}
	}
	for key := range current {
		if _, ok := previous[key]; !ok {
			changed[key] = struct{}{}
		}
	}
	out := make([]string, 0, len(changed))
	for key := range changed {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func mapKeys[T any](values map[string]T) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func changedValueMap[T any](previous, current map[string]T) []string {
	changed := make(map[string]struct{})
	for key, previousValue := range previous {
		currentValue, ok := current[key]
		if !ok || !reflect.DeepEqual(previousValue, currentValue) {
			changed[key] = struct{}{}
		}
	}
	for key, currentValue := range current {
		previousValue, ok := previous[key]
		if !ok || !reflect.DeepEqual(previousValue, currentValue) {
			changed[key] = struct{}{}
		}
	}
	out := make([]string, 0, len(changed))
	for key := range changed {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
