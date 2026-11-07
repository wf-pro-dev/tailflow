// Store is Tailflow's in-memory state. It is the only mutable state in the
// process; the collector writes to it, the API reads from it, and the hub
// receives change events from it.
//
// Concurrency model (design §5.3): per-host RWMutex, NOT one global lock,
// so a slow host never blocks reads for another. API handlers copy out
// immutable snapshot structs; they never hand out references into mutable
// state.
//
// Every mutator publishes the matching event to the hub after releasing the
// host lock (design §16.2 last paragraph). The hub reference is optional so
// tests can exercise the store without a hub.

package state

import (
	"sort"
	"sync"
	"time"
)

// HubPublisher is the subset of the hub.Hub that the store needs. Declared
// here (not imported) so state has no dependency on internal/hub — the hub
// is wired in by the caller via Store.SetHub.
type HubPublisher interface {
	Publish(e HubEvent)
}

// HubEvent is the store's view of an SSE event. Mirrors hub.HubEvent but
// declared here to avoid the import. The caller (collector) adapts between
// the two; the store only fills in Topic/Name/Data and lets the hub assign
// the ID.
type HubEvent struct {
	Topic string
	Name  string
	Data  any
}

// hostState is everything the store holds for one host, guarded by its own
// RWMutex. The fields are mutable; readers must copy under RLock.
type hostState struct {
	mu sync.RWMutex

	host        Host
	ring        *Ring[Sample]
	latest      Latest
	ports       map[PortKey]Port
	containers  ContainerInventory
	status      Status
	failures    int
}

// Store holds the live state of every discovered host.
type Store struct {
	mu    sync.RWMutex
	hosts map[string]*hostState

	// ringCapacity is the per-host ring capacity, derived at startup from
	// TAILFLOW_HISTORY_WINDOW / TAILFLOW_HISTORY_STEP.
	ringCapacity int

	hub HubPublisher
}

// New returns a Store with the given per-host ring capacity (e.g. 720 for
// 1h window / 5s step).
func New(ringCapacity int) *Store {
	if ringCapacity < 1 {
		ringCapacity = 720
	}
	return &Store{
		hosts:        make(map[string]*hostState),
		ringCapacity: ringCapacity,
	}
}

// SetHub wires the hub so mutators publish events. Optional; if not called,
// mutators silently skip publishing (useful for unit tests of the store
// itself).
func (s *Store) SetHub(h HubPublisher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hub = h
}

// publish is a small helper that no-ops if no hub is set.
func (s *Store) publish(topic, name string, data any) {
	if s.hub == nil {
		return
	}
	s.hub.Publish(HubEvent{Topic: topic, Name: name, Data: data})
}

// --- Host lifecycle ---------------------------------------------------------

// UpsertHost inserts or updates a host's static fields (everything except
// status and metrics, which are set by other mutators). Returns true if the
// host was newly created. Publishes host.updated on change.
func (s *Store) UpsertHost(h Host) bool {
	s.mu.Lock()
	hs, existed := s.hosts[h.Name]
	if !existed {
		hs = &hostState{
			host:  h,
			ring:  NewRing[Sample](s.ringCapacity),
			ports: make(map[PortKey]Port),
		}
		s.hosts[h.Name] = hs
		s.mu.Unlock()
		s.publish("hosts", "host.updated", h)
		return true
	}
	s.mu.Unlock()

	// Update existing: only mutable static fields change.
	hs.mu.Lock()
	changed := !hostStaticEqual(hs.host, h)
	if changed {
		// Preserve status, failures, last sample, caps-derived fields.
		h.Status = hs.host.Status
		// Carry over LastSampleAt and StreamInterval if the new host doesn't
		// carry them (discovery only refreshes peer info).
		if h.LastSampleAt == nil {
			h.LastSampleAt = hs.host.LastSampleAt
		}
		if h.StreamInterval == 0 {
			h.StreamInterval = hs.host.StreamInterval
		}
		hs.host = h
	}
	hs.mu.Unlock()

	if changed {
		s.publish("hosts", "host.updated", h)
	}
	return false
}

// RemoveHost deletes a host. Publishes host.removed.
func (s *Store) RemoveHost(name string) {
	s.mu.Lock()
	_, existed := s.hosts[name]
	if existed {
		delete(s.hosts, name)
	}
	s.mu.Unlock()
	if existed {
		s.publish("hosts", "host.removed", map[string]string{"name": name})
	}
}

// Hosts returns a snapshot of every host (sorted by name for stable output).
// Each returned Host is a copy; callers may mutate the slice freely.
func (s *Store) Hosts() []Host {
	s.mu.RLock()
	names := make([]string, 0, len(s.hosts))
	for name := range s.hosts {
		names = append(names, name)
	}
	states := make([]*hostState, len(names))
	for i, name := range names {
		states[i] = s.hosts[name]
	}
	s.mu.RUnlock()

	sort.Strings(names)
	out := make([]Host, len(names))
	for i, name := range names {
		hs := s.hostsLocked(name, states)
		hs.mu.RLock()
		out[i] = hs.host
		hs.mu.RUnlock()
	}
	return out
}

// hostStateByName returns the *hostState for name, fetching under the store
// lock. Returns nil if the host is gone.
func (s *Store) hostStateByName(name string) *hostState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hosts[name]
}

// hostStateFromSnapshot is a helper for Hosts() that finds the state by name
// from a pre-locked snapshot.
func (s *Store) hostsLocked(name string, states []*hostState) *hostState {
	// We already have the states in the same order as sorted names, but the
	// names slice was sorted after we took the snapshot. Just look up directly.
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hosts[name]
}

// Host returns a copy of one host. Returns ok=false if the host is unknown.
func (s *Store) Host(name string) (Host, bool) {
	hs := s.hostStateByName(name)
	if hs == nil {
		return Host{}, false
	}
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	return hs.host, true
}

// --- Metrics ----------------------------------------------------------------

// PushSample appends an aggregated sample to the host's ring and updates
// LastSampleAt. Publishes metrics.sample.
func (s *Store) PushSample(name string, sample Sample) {
	hs := s.hostStateByName(name)
	if hs == nil {
		return
	}
	hs.mu.Lock()
	hs.ring.Push(sample)
	t := sample.T
	hs.host.LastSampleAt = &t
	hs.mu.Unlock()

	s.publish("metrics:"+name, "metrics.sample", map[string]any{
		"host":         name,
		"t":            sample.T.Unix(),
		"cpu_total":    sample.CPUTotal,
		"mem_used_pct": sample.MemUsedPct,
		"net_rx_bps":   sample.NetRxBps,
		"net_tx_bps":   sample.NetTxBps,
	})
}

// Series returns the host's metric samples for the given window, oldest to
// newest. The slice is a copy. Returns nil if the host is unknown.
func (s *Store) Series(name string, window time.Duration) []Sample {
	hs := s.hostStateByName(name)
	if hs == nil {
		return nil
	}
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	all := hs.ring.Snapshot()
	if window <= 0 || len(all) == 0 {
		return all
	}
	cutoff := time.Now().Add(-window)
	// Find the first sample at or after cutoff.
	idx := sort.Search(len(all), func(i int) bool {
		return !all[i].T.Before(cutoff)
	})
	out := make([]Sample, len(all)-idx)
	copy(out, all[idx:])
	return out
}

// Latest returns the latest sample + the latest non-time-series observation
// (processes, ports, disks, net interfaces) for a host. Returns ok=false if
// the host is unknown.
func (s *Store) Latest(name string) (Sample, Latest, bool) {
	hs := s.hostStateByName(name)
	if hs == nil {
		return Sample{}, Latest{}, false
	}
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	samp, _ := hs.ring.Latest()
	// Deep-copy the slices so the caller can't mutate state.
	latest := Latest{
		Processes:     append([]Process(nil), hs.latest.Processes...),
		Ports:         append([]Port(nil), hs.latest.Ports...),
		Disks:         append([]DiskUsage(nil), hs.latest.Disks...),
		NetInterfaces: append([]NetInterfaceCounters(nil), hs.latest.NetInterfaces...),
	}
	return samp, latest, true
}

// SetLatest updates the latest non-time-series observation for a host
// (processes, disks, net interfaces). Used by the metrics loop after each
// raw sample; ports and containers have their own mutators.
func (s *Store) SetLatest(name string, latest Latest) {
	hs := s.hostStateByName(name)
	if hs == nil {
		return
	}
	hs.mu.Lock()
	hs.latest = latest
	hs.mu.Unlock()
}

// --- Ports ------------------------------------------------------------------

// SetPorts replaces the port set for a host (used on the initial snapshot
// from Metrics().Ports()).
func (s *Store) SetPorts(name string, ports []Port) {
	hs := s.hostStateByName(name)
	if hs == nil {
		return
	}
	hs.mu.Lock()
	hs.ports = make(map[PortKey]Port, len(ports))
	for _, p := range ports {
		hs.ports[p.Key()] = p
	}
	hs.latest.Ports = append([]Port(nil), ports...)
	hs.mu.Unlock()
}

// ApplyPortUpdate applies one snapshot/bound/released delta (design §F5).
// kind is "snapshot", "bound", or "released".
func (s *Store) ApplyPortUpdate(name, kind string, port Port, ports []Port) {
	hs := s.hostStateByName(name)
	if hs == nil {
		return
	}
	hs.mu.Lock()
	switch kind {
	case "snapshot":
		hs.ports = make(map[PortKey]Port, len(ports))
		for _, p := range ports {
			hs.ports[p.Key()] = p
		}
	case "bound":
		hs.ports[port.Key()] = port
		s.publish("ports:"+name, "port.bound", map[string]any{"host": name, "port": port})
	case "released":
		delete(hs.ports, port.Key())
		s.publish("ports:"+name, "port.released", map[string]any{"host": name, "port": port})
	}
	// Rebuild latest.Ports from the map.
	hs.latest.Ports = make([]Port, 0, len(hs.ports))
	for _, p := range hs.ports {
		hs.latest.Ports = append(hs.latest.Ports, p)
	}
	sort.Slice(hs.latest.Ports, func(i, j int) bool {
		if hs.latest.Ports[i].Port != hs.latest.Ports[j].Port {
			return hs.latest.Ports[i].Port < hs.latest.Ports[j].Port
		}
		return hs.latest.Ports[i].Proto < hs.latest.Ports[j].Proto
	})
	hs.mu.Unlock()

	if kind == "snapshot" {
		// Don't publish per-port events on snapshot; the whole list changed.
	}
}

// Ports returns a copy of the host's port set.
func (s *Store) Ports(name string) []Port {
	hs := s.hostStateByName(name)
	if hs == nil {
		return nil
	}
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	out := make([]Port, 0, len(hs.ports))
	for _, p := range hs.ports {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Proto < out[j].Proto
	})
	return out
}

// --- Docker -----------------------------------------------------------------

// SetContainers replaces the container inventory for a host. Publishes
// docker.inventory.replaced and one docker.container.changed per changed
// container (design §F6).
func (s *Store) SetContainers(name string, inv ContainerInventory) {
	hs := s.hostStateByName(name)
	if hs == nil {
		return
	}
	hs.mu.Lock()
	prev := hs.containers
	hs.containers = inv
	hs.mu.Unlock()

	// Diff and publish.
	prevByID := make(map[string]Container, len(prev.Containers))
	for _, c := range prev.Containers {
		prevByID[c.ID] = c
	}
	curByID := make(map[string]Container, len(inv.Containers))
	for _, c := range inv.Containers {
		curByID[c.ID] = c
	}
	for id, c := range curByID {
		if old, ok := prevByID[id]; !ok || !containerEqual(old, c) {
			s.publish("docker:"+name, "docker.container.changed", map[string]any{"host": name, "container": c})
		}
	}
	s.publish("docker:"+name, "docker.inventory.replaced", map[string]any{"host": name, "containers": inv.Containers})
}

// Containers returns a copy of the host's container inventory.
func (s *Store) Containers(name string) ContainerInventory {
	hs := s.hostStateByName(name)
	if hs == nil {
		return ContainerInventory{}
	}
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	return ContainerInventory{
		Containers:      append([]Container(nil), hs.containers.Containers...),
		SwarmServices:   append([]SwarmService(nil), hs.containers.SwarmServices...),
		ComposeProjects: append([]ComposeProject(nil), hs.containers.ComposeProjects...),
	}
}

// --- Status -----------------------------------------------------------------

// SetStatus updates the host's derived status. Publishes host.updated only
// if the status actually changed.
func (s *Store) SetStatus(name string, status Status) {
	hs := s.hostStateByName(name)
	if hs == nil {
		return
	}
	hs.mu.Lock()
	if hs.host.Status == status {
		hs.mu.Unlock()
		return
	}
	hs.host.Status = status
	h := hs.host
	hs.mu.Unlock()
	s.publish("hosts", "host.updated", h)
}

// IncFailures increments the consecutive-stream-failure counter.
func (s *Store) IncFailures(name string) {
	hs := s.hostStateByName(name)
	if hs == nil {
		return
	}
	hs.mu.Lock()
	hs.failures++
	hs.mu.Unlock()
}

// ResetFailures resets the consecutive-stream-failure counter to 0.
func (s *Store) ResetFailures(name string) {
	hs := s.hostStateByName(name)
	if hs == nil {
		return
	}
	hs.mu.Lock()
	hs.failures = 0
	hs.mu.Unlock()
}

// Failures returns the current failure count.
func (s *Store) Failures(name string) int {
	hs := s.hostStateByName(name)
	if hs == nil {
		return 0
	}
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	return hs.failures
}

// --- Fleet summary ----------------------------------------------------------

// FleetSummary returns counts by status and fleet-wide CPU/memory means.
// Mirrors the OpenAPI FleetSummary schema.
func (s *Store) FleetSummary() (counts map[Status]int, meanCPU, meanMem float64, containersRunning int) {
	hosts := s.Hosts()
	counts = make(map[Status]int, 5)
	for _, h := range hosts {
		counts[h.Status]++
	}
	var cpuSum, memSum float64
	var n int
	for _, h := range hosts {
		if h.Status != StatusHealthy {
			continue
		}
		if samp, _, ok := s.Latest(h.Name); ok {
			cpuSum += samp.CPUTotal
			memSum += samp.MemUsedPct
			n++
		}
	}
	if n > 0 {
		meanCPU = cpuSum / float64(n)
		meanMem = memSum / float64(n)
	}
	for _, h := range hosts {
		inv := s.Containers(h.Name)
		for _, c := range inv.Containers {
			if c.State == "running" {
				containersRunning++
			}
		}
	}
	return
}

// --- helpers ----------------------------------------------------------------

func hostStaticEqual(a, b Host) bool {
	// Compare only the fields discovery refreshes; skip Status, LastSampleAt,
	// StreamInterval (those are owned by other mutators).
	return a.Name == b.Name &&
		a.Role == b.Role &&
		a.Environment == b.Environment &&
		a.Online == b.Online &&
		a.OS == b.OS &&
		a.DNSName == b.DNSName &&
		stringsEqual(a.Tags, b.Tags) &&
		stringsEqual(a.TSIPs, b.TSIPs) &&
		capsEqual(a.Caps, b.Caps)
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func capsEqual(a, b Capabilities) bool {
	if a.Metrics != b.Metrics || a.Docker != b.Docker || a.Swarm != b.Swarm || a.Compose != b.Compose {
		return false
	}
	if len(a.Reasons) != len(b.Reasons) {
		return false
	}
	for k, v := range a.Reasons {
		if b.Reasons[k] != v {
			return false
		}
	}
	return true
}

func containerEqual(a, b Container) bool {
	if a.ID != b.ID || a.Name != b.Name || a.Image != b.Image || a.State != b.State || a.Status != b.Status || a.Health != b.Health {
		return false
	}
	if len(a.Ports) != len(b.Ports) {
		return false
	}
	for i := range a.Ports {
		if a.Ports[i] != b.Ports[i] {
			return false
		}
	}
	return true
}
