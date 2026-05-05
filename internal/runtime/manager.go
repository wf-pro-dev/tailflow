package runtime

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/wf-pro-dev/tailflow/internal/collector"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/store"
	"github.com/wf-pro-dev/tailflow/internal/topology"
)

const (
	initialBackoff     = 250 * time.Millisecond
	maxBackoff         = 10 * time.Second
	startupSyncTimeout = 15 * time.Second
	startupRetryDelay  = time.Second
)

type Collector interface {
	Bootstrap(context.Context) error
	RefreshPeers(context.Context) ([]core.NodeName, error)
	WatchNode(context.Context, core.NodeName) error
	GetStatus(context.Context) ([]collector.NodeStatus, error)
	Snapshots() []store.NodeSnapshot
	MarkNodeDegraded(core.NodeName, error)
	LocalTailscaleIP(context.Context) (string, error)
}

type Config struct {
	DisableWatchers bool
}

type Manager struct {
	config     Config
	collector  Collector
	state      *core.GlobalState
	inventory  *InventoryState
	projection *ProjectionIndex
	topology   *topology.Manager
	bus        *core.EventBus

	watchMu  sync.Mutex
	watching map[core.NodeName]struct{}
}

func New(cfg Config, collector Collector, topologyManager *topology.Manager, bus *core.EventBus) *Manager {
	return &Manager{
		config:     cfg,
		collector:  collector,
		state:      core.NewGlobalState(),
		inventory:  NewInventoryState(),
		projection: NewProjectionIndex(),
		topology:   topologyManager,
		bus:        bus,
		watching:   make(map[core.NodeName]struct{}),
	}
}

func (m *Manager) Run(ctx context.Context) error {
	if m.collector == nil {
		return errors.New("runtime: collector is required")
	}
	if m.topology == nil {
		return errors.New("runtime: topology manager is required")
	}

	core.Infof("runtime: starting disable_watchers=%t", m.config.DisableWatchers)
	m.state.BeginBootstrap("waiting for tailnet readiness")
	m.waitForTailnetReady(ctx)
	if err := m.collector.Bootstrap(ctx); err != nil {
		core.Warnf("runtime: bootstrap failed err=%v", err)
		m.state.SetDegraded("bootstrap failed: " + err.Error())
		return err
	}
	statuses, err := m.collector.GetStatus(ctx)
	if err != nil {
		core.Warnf("runtime: bootstrap status load failed err=%v", err)
		m.state.SetDegraded("bootstrap status load failed: " + err.Error())
		return err
	}
	m.inventory.Reset(m.collector.Snapshots(), statuses)
	reset := m.topology.Reset(m.inventory.Snapshots(), statuses, "bootstrap")
	m.projection.Reset(reset.Snapshot)
	m.resetGlobalState(reset.Snapshot)
	core.Infof("runtime: bootstrap complete nodes=%d topology_version=%d", len(statuses), reset.Snapshot.Version)

	go m.watchEvents(ctx)

	if !m.config.DisableWatchers {
		m.ensureWatchers(ctx, statuses)
	}

	<-ctx.Done()
	return ctx.Err()
}

func (m *Manager) RefreshNow(ctx context.Context) error {
	core.Infof("runtime: on-demand refresh starting")
	if _, err := m.collector.RefreshPeers(ctx); err != nil {
		core.Warnf("runtime: on-demand refresh failed err=%v", err)
		return err
	}
	if !m.config.DisableWatchers {
		statuses, err := m.collector.GetStatus(ctx)
		if err != nil {
			core.Warnf("runtime: on-demand refresh status load failed err=%v", err)
			return err
		}
		m.ensureWatchers(ctx, statuses)
	}
	core.Infof("runtime: on-demand refresh complete")
	return nil
}

func (m *Manager) watchEvents(ctx context.Context) {
	snapshotEvents := m.bus.Subscribe(ctx, core.TopicSnapshot)
	nodeEvents := m.bus.Subscribe(ctx, core.TopicNode)
	merged := merge(ctx, snapshotEvents, nodeEvents)

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-merged:
			if !ok {
				return
			}
			switch e := event.(type) {
			case core.Event[collector.NodeStatusEvent]:
				core.Debugf("runtime: event received type=%T", event)
				m.inventory.ApplyStatus(e.Data.Current)
				m.syncLiveNode(e.Data.Current.NodeName)
				if patch, changed := m.topology.ApplyNodeStatus(e.Data.Current); changed {
					m.refreshProjectionIndex()
					m.state.SetTopologyVersion(patch.Version)
					core.Infof("runtime: topology patch version=%d changed_nodes=%d", patch.Version, len(patch.ChangedNodes))
					core.BroadcastEvent(m.bus, core.EventTopologyPatch.String(), patch)
				}
			case core.Event[collector.NodePortsReplacedEvent]:
				core.Debugf("runtime: event received type=%T", event)
				changedPortKeys := m.inventory.ReplaceNodePortsWithDiff(e.Data.NodeName, e.Data.Ports)
				m.reconcileNodeScope(ctx, e.Data.NodeName, m.projection.ScopeForPortKeys(e.Data.NodeName, changedPortKeys), topology.FullNodeScopeMask, "node-ports")
			case core.Event[collector.PortBoundEvent]:
				core.Debugf("runtime: event received type=%T", event)
				changedPortKeys := m.inventory.UpsertNodePortWithDiff(e.Data.NodeName, e.Data.Port)
				m.reconcileNodeScope(ctx, e.Data.NodeName, m.projection.ScopeForPortKeys(e.Data.NodeName, changedPortKeys), topology.FullNodeScopeMask, "node-ports")
			case core.Event[collector.PortReleasedEvent]:
				core.Debugf("runtime: event received type=%T", event)
				changedPortKeys := m.inventory.RemoveNodePortWithDiff(e.Data.NodeName, e.Data.Port)
				m.reconcileNodeScope(ctx, e.Data.NodeName, m.projection.ScopeForPortKeys(e.Data.NodeName, changedPortKeys), topology.FullNodeScopeMask, "node-ports")
			case core.Event[collector.NodeContainersReplacedEvent]:
				core.Debugf("runtime: event received type=%T", event)
				changedContainerIDs := m.inventory.ReplaceNodeContainersWithDiff(e.Data.NodeName, e.Data.Containers)
				m.reconcileNodeScope(ctx, e.Data.NodeName, m.projection.ScopeForContainerIDs(e.Data.NodeName, changedContainerIDs), topology.ContainerScopeMask, "node-containers")
			case core.Event[collector.NodeServicesReplacedEvent]:
				core.Debugf("runtime: event received type=%T", event)
				changedServiceKeys := m.inventory.ReplaceNodeServicePortsWithDiff(e.Data.NodeName, e.Data.Services)
				m.reconcileNodeScope(ctx, e.Data.NodeName, m.projection.ScopeForServiceKeys(e.Data.NodeName, changedServiceKeys), topology.ServiceScopeMask, "node-services")
			case core.Event[collector.NodeForwardsReplacedEvent]:
				core.Debugf("runtime: event received type=%T", event)
				changedForwardKeys := m.inventory.ReplaceNodeForwardsWithDiff(e.Data.NodeName, e.Data.Forwards)
				m.reconcileForwardRoutes(ctx, e.Data.NodeName, m.projection.ScopeForForwardKeys(e.Data.NodeName, changedForwardKeys))
			case core.Event[collector.SnapshotEvent]:
				core.Debugf("runtime: event received type=%T", event)
				delta := m.inventory.ApplySnapshotWithDiff(e.Data.Snapshot)
				m.syncLiveNode(e.Data.Snapshot.NodeName)
				if delta.Empty() {
					core.Debugf("runtime: snapshot reconcile skipped node=%s reason=no_topology_delta", e.Data.Snapshot.NodeName)
					continue
				}
				m.reconcileSnapshotDelta(ctx, e.Data.Snapshot.NodeName, delta)
			}
		}
	}
}

func (m *Manager) waitForTailnetReady(ctx context.Context) {
	deadline := time.Now().Add(startupSyncTimeout)
	for {
		ip, err := m.collector.LocalTailscaleIP(ctx)
		switch {
		case err == nil && ip != "":
			core.Infof("runtime: tailnet ready local_ip=%s", ip)
			m.state.BeginBootstrap("bootstrapping live topology state")
			return
		case err != nil:
			core.Debugf("runtime: tailnet readiness pending err=%v", err)
		default:
			core.Debugf("runtime: tailnet readiness pending local_ip_unavailable=true")
		}

		if ctx.Err() != nil {
			return
		}
		if time.Now().After(deadline) {
			core.Warnf("runtime: tailnet readiness timeout after=%s", startupSyncTimeout)
			m.state.SetDegraded("tailnet readiness timeout")
			return
		}

		timer := time.NewTimer(startupRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (m *Manager) reconcile(ctx context.Context) {
	statuses, err := m.collector.GetStatus(ctx)
	if err != nil {
		core.Warnf("runtime: reconcile skipped status_err=%v", err)
		return
	}
	if patch, changed := m.topology.Apply(m.inventory.Snapshots(), statuses); changed {
		m.refreshProjectionIndex()
		m.publishCommittedTopologyState(m.topology.Snapshot())
		core.Infof("runtime: topology patch version=%d changed_nodes=%d services_upserted=%d routes_upserted=%d", patch.Version, len(patch.ChangedNodes), len(patch.ServicesUpserted), len(patch.RoutesUpserted))
		core.BroadcastEvent(m.bus, core.EventTopologyPatch.String(), patch)
		return
	}
	core.Debugf("runtime: reconcile no-op")
}

func (m *Manager) reconcileNodeScope(ctx context.Context, nodeName core.NodeName, previousScope topology.ScopedIDs, mask topology.ScopeMask, reason string) {
	statuses, err := m.collector.GetStatus(ctx)
	if err != nil {
		core.Warnf("runtime: reconcile node scope skipped node=%s reason=%s status_err=%v", nodeName, reason, err)
		return
	}
	previousScope = m.scopeOrNodeFallback(nodeName, previousScope)
	if patch, changed := m.topology.ApplyNodeScope(nodeName, previousScope, mask, m.inventory.Snapshots(), statuses); changed {
		m.refreshProjectionIndex()
		m.publishCommittedTopologyState(m.topology.Snapshot())
		core.Infof("runtime: topology node scope patch node=%s reason=%s version=%d changed_nodes=%d services_upserted=%d routes_upserted=%d", nodeName, reason, patch.Version, len(patch.ChangedNodes), len(patch.ServicesUpserted), len(patch.RoutesUpserted))
		core.BroadcastEvent(m.bus, core.EventTopologyPatch.String(), patch)
		return
	}
	core.Debugf("runtime: reconcile node scope no-op node=%s reason=%s", nodeName, reason)
}

func (m *Manager) reconcileForwardRoutes(ctx context.Context, nodeName core.NodeName, previousScope topology.ScopedIDs) {
	statuses, err := m.collector.GetStatus(ctx)
	if err != nil {
		core.Warnf("runtime: reconcile forward routes skipped node=%s status_err=%v", nodeName, err)
		return
	}
	previousScope = m.scopeOrNodeFallback(nodeName, previousScope)
	if patch, changed := m.topology.ApplyForwardRoutes(nodeName, previousScope, m.inventory.Snapshots(), statuses); changed {
		m.refreshProjectionIndex()
		m.publishCommittedTopologyState(m.topology.Snapshot())
		core.Infof("runtime: topology forward-routes patch node=%s version=%d routes_upserted=%d routes_removed=%d", nodeName, patch.Version, len(patch.RoutesUpserted), len(patch.RoutesRemoved))
		core.BroadcastEvent(m.bus, core.EventTopologyPatch.String(), patch)
		return
	}
	core.Debugf("runtime: reconcile forward routes no-op node=%s", nodeName)
}

func (m *Manager) refreshProjectionIndex() {
	if m.projection == nil {
		return
	}
	m.projection.Reset(m.topology.Snapshot())
}

func (m *Manager) reconcileSnapshotDelta(ctx context.Context, nodeName core.NodeName, delta SnapshotDelta) {
	previousScope := topology.ScopedIDs{}
	mask := topology.ScopeMask{}
	hasNodeScope := false

	if delta.TopologyMetadataChanged {
		mergeRuntimeScopedIDs(&previousScope, m.projection.ScopeForNode(nodeName))
		mask = unionScopeMask(mask, topology.FullNodeScopeMask)
		hasNodeScope = true
	}
	if len(delta.PortsChanged) > 0 {
		mergeRuntimeScopedIDs(&previousScope, m.projection.ScopeForPortKeys(nodeName, delta.PortsChanged))
		mask = unionScopeMask(mask, topology.FullNodeScopeMask)
		hasNodeScope = true
	}
	if len(delta.ContainersChanged) > 0 {
		mergeRuntimeScopedIDs(&previousScope, m.projection.ScopeForContainerIDs(nodeName, delta.ContainersChanged))
		mask = unionScopeMask(mask, topology.ContainerScopeMask)
		hasNodeScope = true
	}
	if len(delta.ServicePortsChanged) > 0 {
		mergeRuntimeScopedIDs(&previousScope, m.projection.ScopeForServiceKeys(nodeName, delta.ServicePortsChanged))
		mask = unionScopeMask(mask, topology.ServiceScopeMask)
		hasNodeScope = true
	}

	forwardScope := m.projection.ScopeForForwardKeys(nodeName, delta.ForwardsChanged)
	if hasNodeScope {
		mergeRuntimeScopedIDs(&previousScope, forwardScope)
		m.reconcileNodeScope(ctx, nodeName, previousScope, mask, "snapshot")
		return
	}
	if len(delta.ForwardsChanged) > 0 {
		m.reconcileForwardRoutes(ctx, nodeName, forwardScope)
		return
	}
	core.Debugf("runtime: snapshot reconcile skipped node=%s reason=live_state_only_delta", nodeName)
}

func (m *Manager) resetGlobalState(snapshot topology.Snapshot) {
	if m.state == nil {
		return
	}
	snapshots := m.inventory.Snapshots()
	nodes := make([]core.LiveNode, 0, len(snapshots))
	for _, snapshot := range snapshots {
		node, ok := m.inventory.SnapshotNode(snapshot.NodeName)
		if !ok {
			nodes = append(nodes, liveNodeFromSnapshot(snapshot, collector.NodeStatus{}))
			continue
		}
		nodes = append(nodes, liveNodeFromInventory(node))
	}
	m.state.Reset(nodes, snapshot.Version, true)
	m.state.SetTopologySummary(len(snapshot.Nodes), len(snapshot.Services), len(snapshot.Routes))
}

func (m *Manager) syncLiveNode(nodeName core.NodeName) {
	if m.state == nil {
		return
	}
	node, ok := m.inventory.SnapshotNode(nodeName)
	if !ok {
		return
	}
	m.state.UpsertNode(liveNodeFromInventory(node))
}

func (m *Manager) publishCommittedTopologyState(snapshot topology.Snapshot) {
	if m.state == nil {
		return
	}
	m.state.SetTopologyVersion(snapshot.Version)
	m.state.SetTopologySummary(len(snapshot.Nodes), len(snapshot.Services), len(snapshot.Routes))
}

func liveNodeFromInventory(node NodeInventory) core.LiveNode {
	snapshot := nodeInventorySnapshot(&node)
	return liveNodeFromSnapshot(snapshot, node.Status)
}

func liveNodeFromSnapshot(snapshot store.NodeSnapshot, status collector.NodeStatus) core.LiveNode {
	live := core.LiveNode{
		NodeName:    snapshot.NodeName,
		TailscaleIP: snapshot.TailscaleIP,
		DNSName:     snapshot.DNSName,
		CollectedAt: snapshot.CollectedAt,
		Status:      liveNodeStatusFromCollector(status),
		Ports:       make([]core.LivePort, 0, len(snapshot.Ports)),
		Containers:  make([]core.LiveContainer, 0, len(snapshot.Containers)),
		Services:    make([]core.LiveServicePort, 0, len(snapshot.Services)),
		Forwards:    make([]core.LiveForward, 0, len(snapshot.Forwards)),
	}
	for _, port := range snapshot.Ports {
		live.Ports = append(live.Ports, core.LivePort{
			Addr:    port.Addr,
			Port:    port.Port,
			Proto:   port.Proto,
			PID:     port.PID,
			Process: port.Process,
		})
	}
	for _, container := range snapshot.Containers {
		liveContainer := core.LiveContainer{
			ContainerID:    container.ContainerID,
			ContainerName:  container.ContainerName,
			Image:          container.Image,
			State:          container.State,
			Status:         container.Status,
			ServiceName:    container.ServiceName,
			PublishedPorts: make([]core.LiveContainerPort, 0, len(container.PublishedPorts)),
		}
		for _, port := range container.PublishedPorts {
			liveContainer.PublishedPorts = append(liveContainer.PublishedPorts, core.LiveContainerPort{
				HostPort:   port.HostPort,
				TargetPort: port.TargetPort,
				Proto:      port.Proto,
				Source:     port.Source,
				Mode:       port.Mode,
			})
		}
		live.Containers = append(live.Containers, liveContainer)
	}
	for _, service := range snapshot.Services {
		live.Services = append(live.Services, core.LiveServicePort{
			ServiceID:   service.ServiceID,
			ServiceName: service.ServiceName,
			HostPort:    service.HostPort,
			TargetPort:  service.TargetPort,
			Proto:       service.Proto,
			Mode:        service.Mode,
		})
	}
	for _, forward := range snapshot.Forwards {
		live.Forwards = append(live.Forwards, core.LiveForward{
			ListenerAddr: forward.Listener.Addr,
			ListenerPort: forward.Listener.Port,
			Target: core.LiveForwardTarget{
				Raw:    forward.Target.Raw,
				Kind:   forward.Target.Kind,
				Host:   forward.Target.Host,
				Port:   forward.Target.Port,
				Socket: forward.Target.Socket,
			},
			Hostnames: append([]string(nil), forward.Hostnames...),
		})
	}
	return live
}

func liveNodeStatusFromCollector(status collector.NodeStatus) core.LiveNodeStatus {
	return core.LiveNodeStatus{
		Online:            status.Online,
		CollectorDegraded: status.Degraded,
		CollectorError:    status.LastError,
		LastSeenAt:        status.LastSeenAt,
	}
}

func mergeRuntimeScopedIDs(target *topology.ScopedIDs, source topology.ScopedIDs) {
	if target == nil {
		return
	}
	if target.ServiceIDs == nil {
		*target = topology.ScopedIDs{
			ServiceIDs:  make(map[string]struct{}),
			RuntimeIDs:  make(map[string]struct{}),
			ExposureIDs: make(map[string]struct{}),
			RouteIDs:    make(map[string]struct{}),
			HopIDs:      make(map[string]struct{}),
			EvidenceIDs: make(map[string]struct{}),
		}
	}
	for id := range source.ServiceIDs {
		target.ServiceIDs[id] = struct{}{}
	}
	for id := range source.RuntimeIDs {
		target.RuntimeIDs[id] = struct{}{}
	}
	for id := range source.ExposureIDs {
		target.ExposureIDs[id] = struct{}{}
	}
	for id := range source.RouteIDs {
		target.RouteIDs[id] = struct{}{}
	}
	for id := range source.HopIDs {
		target.HopIDs[id] = struct{}{}
	}
	for id := range source.EvidenceIDs {
		target.EvidenceIDs[id] = struct{}{}
	}
}

func unionScopeMask(left, right topology.ScopeMask) topology.ScopeMask {
	return topology.ScopeMask{
		Nodes:     left.Nodes || right.Nodes,
		Services:  left.Services || right.Services,
		Runtimes:  left.Runtimes || right.Runtimes,
		Exposures: left.Exposures || right.Exposures,
		Routes:    left.Routes || right.Routes,
		RouteHops: left.RouteHops || right.RouteHops,
		Evidence:  left.Evidence || right.Evidence,
	}
}

func (m *Manager) scopeOrNodeFallback(nodeName core.NodeName, scope topology.ScopedIDs) topology.ScopedIDs {
	if !scopeIsEmpty(scope) {
		return scope
	}
	return m.projection.ScopeForNode(nodeName)
}

func scopeIsEmpty(scope topology.ScopedIDs) bool {
	return len(scope.ServiceIDs) == 0 &&
		len(scope.RuntimeIDs) == 0 &&
		len(scope.ExposureIDs) == 0 &&
		len(scope.RouteIDs) == 0 &&
		len(scope.HopIDs) == 0 &&
		len(scope.EvidenceIDs) == 0
}

func (m *Manager) ensureWatchers(ctx context.Context, statuses []collector.NodeStatus) {
	names := make([]core.NodeName, 0, len(statuses))
	for _, status := range statuses {
		if status.Online && shouldWatchNode(status) {
			names = append(names, status.NodeName)
		}
	}
	m.ensureWatchingNames(ctx, names)
}

func (m *Manager) ensureWatchingNames(ctx context.Context, names []core.NodeName) {
	for _, nodeName := range names {
		m.startWatcher(ctx, nodeName)
	}
}

func (m *Manager) startWatcher(ctx context.Context, nodeName core.NodeName) {
	m.watchMu.Lock()
	if _, ok := m.watching[nodeName]; ok {
		m.watchMu.Unlock()
		return
	}
	m.watching[nodeName] = struct{}{}
	m.watchMu.Unlock()

	go func() {
		defer func() {
			m.watchMu.Lock()
			delete(m.watching, nodeName)
			m.watchMu.Unlock()
			core.Infof("runtime: watcher stopped node=%s", nodeName)
		}()

		backoff := initialBackoff
		for {
			if ctx.Err() != nil {
				return
			}
			core.Infof("runtime: watcher starting node=%s", nodeName)
			err := m.collector.WatchNode(ctx, nodeName)
			if err == nil || errors.Is(err, context.Canceled) {
				return
			}
			core.Warnf("runtime: watcher failed node=%s backoff=%s err=%v", nodeName, backoff, err)
			if shouldMarkWatcherFailureDegraded(err) {
				m.collector.MarkNodeDegraded(nodeName, err)
			}
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}()
}

func shouldWatchNode(status collector.NodeStatus) bool {
	return !strings.Contains(strings.ToLower(status.LastError), "tailkit node client unavailable")
}

func shouldMarkWatcherFailureDegraded(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false
	}
	return true
}

func merge(ctx context.Context, channels ...<-chan any) <-chan any {
	out := make(chan any)
	var wg sync.WaitGroup
	wg.Add(len(channels))
	for _, ch := range channels {
		ch := ch
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case event, ok := <-ch:
					if !ok {
						return
					}
					select {
					case <-ctx.Done():
						return
					case out <- event:
					}
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}
