package collector

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/swarm"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/store"
	"github.com/wf-pro-dev/tailkit"
	tailkittypes "github.com/wf-pro-dev/tailkit/types"
	integrationsTypes "github.com/wf-pro-dev/tailkit/types/integrations"
)

const degradeThreshold = 3
const defaultNodeTimeout = 10 * time.Second
const localStoreTimeout = 2 * time.Second

var errNodeClientUnavailable = errors.New("tailkit node client unavailable")

// NodeStatus tracks liveness per node between collection cycles.
type NodeStatus struct {
	NodeName   core.NodeName  `json:"node_name"`
	Online     bool           `json:"online"`
	Degraded   bool           `json:"degraded"`
	LastSeenAt core.Timestamp `json:"last_seen_at"`
	LastError  string         `json:"last_error,omitempty"`
}

// SnapshotEvent is emitted after a snapshot is stored or patched.
type SnapshotEvent struct {
	RunID    core.ID            `json:"run_id"`
	Snapshot store.NodeSnapshot `json:"snapshot"`
}

// NodeStatusEvent is emitted when a node status changes.
type NodeStatusEvent struct {
	Previous NodeStatus `json:"previous"`
	Current  NodeStatus `json:"current"`
}

// PortBoundEvent represents a newly observed listening port on a node.
type PortBoundEvent struct {
	NodeName core.NodeName `json:"node_name"`
	Port     store.ListenPort
}

// PortReleasedEvent represents a removed listening port on a node.
type PortReleasedEvent struct {
	NodeName core.NodeName `json:"node_name"`
	Port     store.ListenPort
}

// NodePortsReplacedEvent represents a full replacement of the current port inventory for one node.
type NodePortsReplacedEvent struct {
	NodeName core.NodeName      `json:"node_name"`
	Ports    []store.ListenPort `json:"ports"`
}

// NodeContainersReplacedEvent represents a full replacement of the current container inventory for one node.
type NodeContainersReplacedEvent struct {
	NodeName   core.NodeName     `json:"node_name"`
	Containers []store.Container `json:"containers"`
}

// NodeServicesReplacedEvent represents a full replacement of the current swarm service port inventory for one node.
type NodeServicesReplacedEvent struct {
	NodeName core.NodeName            `json:"node_name"`
	Services []store.SwarmServicePort `json:"services"`
}

// NodeForwardsReplacedEvent represents a full replacement of the current proxy forward inventory for one node.
type NodeForwardsReplacedEvent struct {
	NodeName core.NodeName          `json:"node_name"`
	Forwards []parser.ForwardAction `json:"forwards"`
}

type nodeClient interface {
	Metrics() metricsClient
	Docker() dockerClient
	Files() filesClient
}

type metricsClient interface {
	Ports(context.Context) ([]tailkittypes.Port, error)
	StreamPorts(context.Context, func(tailkittypes.Event[tailkittypes.PortUpdate]) error) error
}

type dockerClient interface {
	Config(context.Context) (integrationsTypes.DockerConfig, error)
	Containers(context.Context) ([]container.Summary, error)
	StreamContainers(context.Context, func(tailkittypes.Event[tailkittypes.DockerEvent]) error) error
	SwarmServices(context.Context) ([]swarm.Service, error)
}

type filesClient interface {
	Read(context.Context, string) (string, error)
	List(context.Context, string) ([]tailkittypes.DirEntry, error)
}

type tailkitNodeClient struct {
	node *tailkit.NodeClient
}

func (n tailkitNodeClient) Metrics() metricsClient {
	return tailkitMetricsClient{metrics: n.node.Metrics()}
}
func (n tailkitNodeClient) Docker() dockerClient { return tailkitDockerClient{docker: n.node.Docker()} }
func (n tailkitNodeClient) Files() filesClient   { return tailkitFilesClient{files: n.node.Files()} }

type tailkitMetricsClient struct{ metrics *tailkit.MetricsClient }

func (m tailkitMetricsClient) Ports(ctx context.Context) ([]tailkittypes.Port, error) {
	return m.metrics.Ports(ctx)
}

func (m tailkitMetricsClient) StreamPorts(ctx context.Context, fn func(tailkittypes.Event[tailkittypes.PortUpdate]) error) error {
	return m.metrics.StreamPorts(ctx, fn)
}

type tailkitDockerClient struct{ docker *tailkit.DockerClient }

func (d tailkitDockerClient) Config(ctx context.Context) (integrationsTypes.DockerConfig, error) {
	return d.docker.Config(ctx)
}

func (d tailkitDockerClient) Containers(ctx context.Context) ([]container.Summary, error) {
	return d.docker.Containers(ctx)
}

func (d tailkitDockerClient) StreamContainers(ctx context.Context, fn func(tailkittypes.Event[tailkittypes.DockerEvent]) error) error {
	return d.docker.StreamContainers(ctx, fn)
}

func (d tailkitDockerClient) SwarmServices(ctx context.Context) ([]swarm.Service, error) {
	return d.docker.Swarm().Services(ctx)
}

type tailkitFilesClient struct{ files *tailkit.FilesClient }

func (f tailkitFilesClient) Read(ctx context.Context, path string) (string, error) {
	return f.files.Read(ctx, path)
}

func (f tailkitFilesClient) List(ctx context.Context, path string) ([]tailkittypes.DirEntry, error) {
	return f.files.List(ctx, path)
}

// Collector builds and maintains node snapshots from tailkit data.
type Collector struct {
	srv          *tailkit.Server
	proxyConfigs store.ProxyConfigStore
	bus          *core.EventBus
	parsers      parser.Registry

	discoverPeers func(context.Context, *tailkit.Server) ([]tailkittypes.Peer, error)
	newNode       func(string) nodeClient

	mu       sync.Mutex
	statuses map[core.NodeName]NodeStatus
	failures map[core.NodeName]int
	live     map[core.NodeName]store.NodeSnapshot

	nodeTimeout time.Duration
}

type proxyConfigFetchResult struct {
	content string
	bundle  map[string]string
	parsed  parser.ParseResult
	err     error
}

type collectNodeResult struct {
	peer     tailkittypes.Peer
	snapshot store.NodeSnapshot
	err      error
}

func NewCollector(
	srv *tailkit.Server,
	proxyConfigs store.ProxyConfigStore,
	bus *core.EventBus,
	parsers parser.Registry,
) *Collector {
	c := &Collector{
		srv:           srv,
		proxyConfigs:  proxyConfigs,
		bus:           bus,
		parsers:       parsers,
		discoverPeers: tailkit.OnlinePeers,
		statuses:      make(map[core.NodeName]NodeStatus),
		failures:      make(map[core.NodeName]int),
		live:          make(map[core.NodeName]store.NodeSnapshot),
		nodeTimeout:   defaultNodeTimeout,
	}
	c.newNode = func(hostname string) nodeClient {
		if srv == nil {
			return nil
		}
		peer, err := tailkit.GetTailkitPeer(context.Background(), srv, hostname)
		if err != nil || peer == nil {
			return nil
		}
		node := tailkit.Node(srv, hostname)
		if node == nil {
			return nil
		}
		return tailkitNodeClient{node: node}
	}
	return c
}

func (c *Collector) SetNodeTimeout(timeout time.Duration) {
	if timeout <= 0 {
		timeout = defaultNodeTimeout
	}
	c.nodeTimeout = timeout
}

func (c *Collector) LocalTailscaleIP(ctx context.Context) (string, error) {
	if c.srv == nil || c.srv.Server == nil {
		return "", nil
	}

	lc, err := c.srv.Server.LocalClient()
	if err != nil {
		return "", err
	}
	status, err := lc.StatusWithoutPeers(ctx)
	if err != nil {
		return "", err
	}
	if status == nil || len(status.TailscaleIPs) == 0 {
		return "", nil
	}
	return status.TailscaleIPs[0].String(), nil
}

func (c *Collector) Bootstrap(ctx context.Context) error {
	peers, err := c.discoverPeers(ctx, c.srv)
	if err != nil {
		return fmt.Errorf("discover peers: %w", err)
	}
	core.Infof("collector: bootstrap peers=%d", len(peers))

	results := c.collectPeers(ctx, peers, "")
	seen := make(map[core.NodeName]struct{}, len(peers))
	successes := 0
	failures := 0
	for _, res := range results {
		nodeName := core.NodeName(res.peer.Status.HostName)
		seen[nodeName] = struct{}{}
		c.setSnapshot(res.snapshot)
		if res.err != nil {
			failures++
			c.applyCollectionFailure(nodeName, res.err)
			continue
		}
		successes++
		c.applyCollectionSuccess(nodeName)
	}
	c.markOfflineNodes(seen)
	core.Infof("collector: bootstrap complete successes=%d failures=%d live=%d", successes, failures, len(c.Snapshots()))
	return nil
}

func (c *Collector) RefreshPeers(ctx context.Context) ([]core.NodeName, error) {
	peers, err := c.discoverPeers(ctx, c.srv)
	if err != nil {
		return nil, fmt.Errorf("discover peers: %w", err)
	}
	core.Infof("collector: refresh peers=%d", len(peers))

	seen := make(map[core.NodeName]struct{}, len(peers))
	successes := 0
	failures := 0
	for _, peer := range peers {
		nodeName := core.NodeName(peer.Status.HostName)
		seen[nodeName] = struct{}{}
		snapshot, collectErr := c.collectNode(ctx, peer, "")
		c.setSnapshot(snapshot)
		if collectErr != nil {
			failures++
			c.applyCollectionFailure(nodeName, collectErr)
			continue
		}
		successes++
		c.applyCollectionSuccess(nodeName)
		core.BroadcastEvent(c.bus, core.EventSnapshotUpdated.String(), SnapshotEvent{
			RunID:    snapshot.RunID,
			Snapshot: snapshot,
		})
	}
	c.markOfflineNodes(seen)

	online := make([]core.NodeName, 0, len(peers))
	for _, peer := range peers {
		online = append(online, core.NodeName(peer.Status.HostName))
	}
	sort.Slice(online, func(i, j int) bool { return online[i] < online[j] })
	core.Infof("collector: refresh complete online=%d successes=%d failures=%d", len(online), successes, failures)
	return online, nil
}

func (c *Collector) Snapshots() []store.NodeSnapshot {
	c.mu.Lock()
	if len(c.live) > 0 {
		snapshots := make([]store.NodeSnapshot, 0, len(c.live))
		for _, snapshot := range c.live {
			snapshots = append(snapshots, snapshot)
		}
		c.mu.Unlock()
		sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].NodeName < snapshots[j].NodeName })
		return snapshots
	}
	c.mu.Unlock()

	return nil
}

func (c *Collector) LatestSnapshot(nodeName core.NodeName) (store.NodeSnapshot, bool) {
	c.mu.Lock()
	snapshot, ok := c.live[nodeName]
	c.mu.Unlock()
	if ok {
		return snapshot, true
	}
	return store.NodeSnapshot{}, false
}

func (c *Collector) MarkNodeDegraded(nodeName core.NodeName, err error) {
	if err == nil {
		err = errors.New("watcher failed")
	}
	c.applyCollectionFailure(nodeName, err)
}

// WatchNode patches the latest stored snapshot from the live port stream.
func (c *Collector) WatchNode(ctx context.Context, nodeName core.NodeName) error {
	node := c.newNode(string(nodeName))
	if node == nil {
		return fmt.Errorf("%w: %s", errNodeClientUnavailable, nodeName)
	}
	core.Infof("collector: watch stream starting node=%s", nodeName)

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var snapshotMu sync.Mutex
	errCh := make(chan error, 2)

	go func() {
		errCh <- c.watchPortStream(watchCtx, nodeName, node, &snapshotMu)
	}()

	go func() {
		errCh <- c.watchDockerStream(watchCtx, nodeName, node, &snapshotMu)
	}()

	var firstErr error
	for i := 0; i < 2; i++ {
		err := <-errCh
		if firstErr == nil && err != nil && !errors.Is(err, context.Canceled) {
			firstErr = err
			cancel()
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func (c *Collector) watchPortStream(ctx context.Context, nodeName core.NodeName, node nodeClient, snapshotMu *sync.Mutex) error {
	return node.Metrics().StreamPorts(ctx, func(event tailkittypes.Event[tailkittypes.PortUpdate]) error {
		snapshotMu.Lock()
		defer snapshotMu.Unlock()

		snapshot := c.latestOrEmptySnapshot(nodeName)

		switch event.Data.Kind {
		case "snapshot":
			snapshot.Ports = mapPorts(event.Data.Ports)
			core.BroadcastEvent(c.bus, core.EventNodePortsReplaced.String(), NodePortsReplacedEvent{
				NodeName: nodeName,
				Ports:    snapshot.Ports,
			})
		case "bound":
			port := mapPort(event.Data.Port)
			snapshot.Ports = upsertPort(snapshot.Ports, port)
			core.BroadcastEvent(c.bus, core.EventNodePortUpserted.String(), PortBoundEvent{NodeName: nodeName, Port: port})
		case "released":
			port := mapPort(event.Data.Port)
			snapshot.Ports = removePort(snapshot.Ports, port)
			core.BroadcastEvent(c.bus, core.EventNodePortRemoved.String(), PortReleasedEvent{NodeName: nodeName, Port: port})
		}

		forwards, proxyErrs := c.collectProxyForwards(ctx, nodeName, node.Files())
		forwardsChanged := !reflect.DeepEqual(snapshot.Forwards, forwards)
		snapshot.Forwards = forwards
		if forwardsChanged {
			core.BroadcastEvent(c.bus, core.EventNodeForwardsReplaced.String(), NodeForwardsReplacedEvent{
				NodeName: nodeName,
				Forwards: snapshot.Forwards,
			})
		}
		snapshot.CollectedAt = core.NowTimestamp()
		snapshot.Error = ""
		if len(proxyErrs) > 0 {
			snapshot.Error = joinErrors(proxyErrs).Error()
		}

		c.commitPatchedSnapshot(nodeName, snapshot)
		core.Debugf("collector: watch port event node=%s kind=%s ports=%d", nodeName, event.Data.Kind, len(snapshot.Ports))
		return nil
	})
}

func (c *Collector) watchDockerStream(ctx context.Context, nodeName core.NodeName, node nodeClient, snapshotMu *sync.Mutex) error {
	dockerEnabled, swarmReadEnabled, ok := c.watchDockerCapabilities(ctx, nodeName, node.Docker())
	if !ok || !dockerEnabled {
		return nil
	}

	return node.Docker().StreamContainers(ctx, func(event tailkittypes.Event[tailkittypes.DockerEvent]) error {
		if !shouldRefreshDockerInventory(event.Data) {
			return nil
		}

		snapshotMu.Lock()
		defer snapshotMu.Unlock()

		snapshot := c.latestOrEmptySnapshot(nodeName)
		if err := c.refreshDockerInventory(ctx, nodeName, node.Docker(), swarmReadEnabled, &snapshot); err != nil {
			core.Warnf("collector: watch docker refresh failed node=%s action=%s err=%v", nodeName, event.Data.Action, err)
			c.applyCollectionFailure(nodeName, err)
			return nil
		}

		c.commitPatchedSnapshot(nodeName, snapshot)
		core.Debugf("collector: watch docker event node=%s action=%s containers=%d services=%d", nodeName, event.Data.Action, len(snapshot.Containers), len(snapshot.Services))
		return nil
	})
}

func (c *Collector) latestOrEmptySnapshot(nodeName core.NodeName) store.NodeSnapshot {
	snapshot, ok := c.LatestSnapshot(nodeName)
	if ok {
		return snapshot
	}
	return store.NodeSnapshot{
		ID:          core.NewID(),
		NodeName:    nodeName,
		CollectedAt: core.NowTimestamp(),
	}
}

func (c *Collector) commitPatchedSnapshot(nodeName core.NodeName, snapshot store.NodeSnapshot) {
	snapshot.CollectedAt = core.NowTimestamp()
	c.setSnapshot(snapshot)
	c.markNodeOnline(nodeName)
}

func (c *Collector) watchDockerCapabilities(ctx context.Context, nodeName core.NodeName, docker dockerClient) (bool, bool, bool) {
	dockerConfigCtx, cancelDockerConfig := c.remoteCallContext(ctx)
	defer cancelDockerConfig()

	dockerConfig, err := docker.Config(dockerConfigCtx)
	if err != nil {
		core.Debugf("collector: watch docker config skipped node=%s err=%v", nodeName, err)
		return false, false, false
	}
	return dockerConfig.Enabled, dockerConfig.Swarm.Permits("read"), true
}

func (c *Collector) refreshDockerInventory(ctx context.Context, nodeName core.NodeName, docker dockerClient, swarmReadEnabled bool, snapshot *store.NodeSnapshot) error {
	if snapshot == nil {
		return errors.New("docker refresh snapshot is nil")
	}

	services := snapshot.Services
	if swarmReadEnabled {
		swarmCtx, cancelSwarm := c.remoteCallContext(ctx)
		currentServices, err := docker.SwarmServices(swarmCtx)
		cancelSwarm()
		if err != nil && !errors.Is(err, tailkittypes.ErrDockerUnavailable) {
			core.Debugf("collector: watch docker swarm services skipped node=%s err=%v", nodeName, err)
		} else if err == nil {
			services = mapSwarmServices(currentServices)
		}
	}

	containersCtx, cancelContainers := c.remoteCallContext(ctx)
	currentContainers, err := docker.Containers(containersCtx)
	cancelContainers()
	if err != nil && !errors.Is(err, tailkittypes.ErrDockerUnavailable) {
		return fmt.Errorf("docker containers: %w", err)
	}

	servicesChanged := !reflect.DeepEqual(snapshot.Services, services)
	snapshot.Services = services
	if servicesChanged {
		core.BroadcastEvent(c.bus, core.EventNodeServicesReplaced.String(), NodeServicesReplacedEvent{
			NodeName: nodeName,
			Services: snapshot.Services,
		})
	}
	if err == nil {
		containers := mapContainers(currentContainers, services)
		containersChanged := !reflect.DeepEqual(snapshot.Containers, containers)
		snapshot.Containers = containers
		if containersChanged {
			core.BroadcastEvent(c.bus, core.EventNodeContainersReplaced.String(), NodeContainersReplacedEvent{
				NodeName:   nodeName,
				Containers: snapshot.Containers,
			})
		}
	}
	snapshot.Error = ""
	return nil
}

func shouldRefreshDockerInventory(event tailkittypes.DockerEvent) bool {
	action := string(event.Action)
	switch {
	case action == string(events.ActionAttach),
		action == string(events.ActionDetach),
		action == "exec_create",
		action == "exec_start",
		action == "exec_die",
		action == "top",
		action == "resize",
		action == "archive-path",
		action == "extract-to-dir":
		return false
	}

	if action == "" {
		return true
	}

	switch {
	case action == string(events.ActionCreate),
		action == string(events.ActionStart),
		action == string(events.ActionRestart),
		action == string(events.ActionStop),
		action == string(events.ActionKill),
		action == string(events.ActionDie),
		action == string(events.ActionOOM),
		action == string(events.ActionPause),
		action == string(events.ActionUnPause),
		action == string(events.ActionUpdate),
		action == string(events.ActionRename),
		action == string(events.ActionDestroy),
		action == string(events.ActionRemove):
		return true
	case strings.HasPrefix(action, string(events.ActionHealthStatus)):
		return true
	default:
		return event.Type == "" || event.Type == events.ContainerEventType
	}
}

// GetStatus returns current collector node status state.
func (c *Collector) GetStatus(ctx context.Context) ([]NodeStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	statuses := make([]NodeStatus, 0, len(c.statuses))
	for _, status := range c.statuses {
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].NodeName < statuses[j].NodeName })
	return statuses, nil
}

// PreviewProxyConfig reads a remote proxy config file and parses its content.
func (c *Collector) PreviewProxyConfig(ctx context.Context, nodeName core.NodeName, kind string, configPath string) (string, map[string]string, parser.ParseResult, error) {
	node := c.newNode(string(nodeName))
	if node == nil {
		return "", nil, parser.ParseResult{}, fmt.Errorf("tailkit node client unavailable")
	}

	nodeCtx, cancel := c.remoteCallContext(ctx)
	defer cancel()

	result := c.readAndParseProxyConfig(nodeCtx, node.Files(), kind, configPath)
	return result.content, result.bundle, result.parsed, result.err
}

func (c *Collector) collectNode(ctx context.Context, peer tailkittypes.Peer, runID core.ID) (store.NodeSnapshot, error) {
	nodeName := core.NodeName(peer.Status.HostName)
	core.Debugf("collector: collect start node=%s run_id=%s", nodeName, runID)
	snapshot := store.NodeSnapshot{
		ID:          core.NewID(),
		RunID:       runID,
		NodeName:    nodeName,
		TailscaleIP: firstTailscaleIP(peer),
		DNSName:     normalizeDNSName(peer.Status.DNSName),
		CollectedAt: core.NowTimestamp(),
	}

	node := c.newNode(string(nodeName))
	if node == nil {
		err := fmt.Errorf("%w: %s", errNodeClientUnavailable, nodeName)
		snapshot.Error = err.Error()
		return snapshot, err
	}

	var partialErrs []error

	portsCtx, cancelPorts := c.remoteCallContext(ctx)
	ports, err := node.Metrics().Ports(portsCtx)
	cancelPorts()
	if err != nil {
		partialErrs = append(partialErrs, fmt.Errorf("ports: %w", err))
	} else {
		snapshot.Ports = mapPorts(ports)
	}

	dockerEnabled := true
	swarmReadEnabled := false

	dockerConfigCtx, cancelDockerConfig := c.remoteCallContext(ctx)
	dockerConfig, err := node.Docker().Config(dockerConfigCtx)
	cancelDockerConfig()
	if err != nil {
		partialErrs = append(partialErrs, fmt.Errorf("docker config: %w", err))
	} else {
		dockerEnabled = dockerConfig.Enabled
		swarmReadEnabled = dockerConfig.Swarm.Permits("read")
	}

	if swarmReadEnabled {
		swarmCtx, cancelSwarm := c.remoteCallContext(ctx)
		services, err := node.Docker().SwarmServices(swarmCtx)
		cancelSwarm()
		if err != nil && !errors.Is(err, tailkittypes.ErrDockerUnavailable) {
			core.Debugf("collector: swarm services skipped node=%s err=%v", nodeName, err)
		} else if err == nil {
			snapshot.Services = mapSwarmServices(services)
		}
	}

	if dockerEnabled {
		containersCtx, cancelContainers := c.remoteCallContext(ctx)
		containers, err := node.Docker().Containers(containersCtx)
		cancelContainers()
		if err != nil && !errors.Is(err, tailkittypes.ErrDockerUnavailable) {
			partialErrs = append(partialErrs, fmt.Errorf("docker: %w", err))
		} else if err == nil {
			snapshot.Containers = mapContainers(containers, snapshot.Services)
		}
	}

	if c.proxyConfigs != nil {
		snapshot.Forwards, partialErrs = c.collectProxyForwards(ctx, nodeName, node.Files(), partialErrs...)
	}

	merged := joinErrors(partialErrs)
	if merged != nil {
		snapshot.Error = merged.Error()
		core.Warnf("collector: collect partial_failure node=%s ports=%d containers=%d services=%d forwards=%d err=%v", nodeName, len(snapshot.Ports), len(snapshot.Containers), len(snapshot.Services), len(snapshot.Forwards), merged)
		return snapshot, merged
	}
	core.Debugf("collector: collect success node=%s ports=%d containers=%d services=%d forwards=%d", nodeName, len(snapshot.Ports), len(snapshot.Containers), len(snapshot.Services), len(snapshot.Forwards))
	return snapshot, nil
}

func (c *Collector) collectProxyForwards(ctx context.Context, nodeName core.NodeName, files filesClient, errs ...error) ([]parser.ForwardAction, []error) {
	if c.proxyConfigs == nil {
		return nil, errs
	}

	storeCtx, cancelStore := c.localStoreContext(ctx)
	configs, err := c.proxyConfigs.ListByNode(storeCtx, nodeName)
	cancelStore()
	if err != nil {
		return nil, append(errs, fmt.Errorf("proxy config store: %w", err))
	}

	allForwards := make([]parser.ForwardAction, 0)
	for _, config := range configs {
		configCtx, cancelConfig := c.remoteCallContext(ctx)
		result := c.readAndParseProxyConfig(configCtx, files, config.Kind, config.ConfigPath)
		cancelConfig()
		allForwards = append(allForwards, result.parsed.Forwards...)
		for _, warning := range result.parsed.Errors {
			errs = append(errs, errors.New("proxy config warning: "+warning))
		}
		if result.err != nil {
			errs = append(errs, result.err)
		}
	}
	return parser.DedupeForwards(allForwards), errs
}

func (c *Collector) collectPeers(ctx context.Context, peers []tailkittypes.Peer, runID core.ID) []collectNodeResult {
	results := make(chan collectNodeResult, len(peers))
	var wg sync.WaitGroup
	for _, peer := range peers {
		peer := peer
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot, err := c.collectNode(ctx, peer, runID)
			results <- collectNodeResult{peer: peer, snapshot: snapshot, err: err}
		}()
	}
	wg.Wait()
	close(results)

	collected := make([]collectNodeResult, 0, len(peers))
	for result := range results {
		collected = append(collected, result)
	}
	enrichResultsWithSwarmServicePublishes(collected)
	return collected
}

func (c *Collector) setSnapshot(snapshot store.NodeSnapshot) {
	c.mu.Lock()
	if snapshot.ID == "" {
		snapshot.ID = core.NewID()
	}
	c.live[snapshot.NodeName] = snapshot
	c.mu.Unlock()
	core.Debugf("collector: snapshot stored node=%s ports=%d containers=%d services=%d forwards=%d error=%t", snapshot.NodeName, len(snapshot.Ports), len(snapshot.Containers), len(snapshot.Services), len(snapshot.Forwards), snapshot.Error != "")
}

func (c *Collector) localStoreContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, localStoreTimeout)
}

func (c *Collector) remoteCallContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := ctx
	if base == nil {
		base = context.Background()
	}
	if c.nodeTimeout <= 0 {
		return base, func() {}
	}
	return context.WithTimeout(base, c.nodeTimeout)
}

func (c *Collector) readAndParseProxyConfig(ctx context.Context, files filesClient, kind string, configPath string) proxyConfigFetchResult {
	content, err := files.Read(ctx, configPath)
	if err != nil {
		return proxyConfigFetchResult{
			err: fmt.Errorf("proxy config read: %w", err),
		}
	}

	bundle := map[string]string{configPath: content}
	if strings.EqualFold(kind, "nginx") {
		bundle, err = c.fetchNginxConfigBundle(ctx, files, configPath, content)
		if err != nil {
			return proxyConfigFetchResult{
				content: content,
				bundle:  map[string]string{configPath: content},
				err:     err,
			}
		}
	}

	parseResult, parseErr := c.parsers.ParseBundle(kind, configPath, bundle)
	if parseErr != nil {
		return proxyConfigFetchResult{
			content: content,
			bundle:  bundle,
			parsed:  parseResult,
			err:     fmt.Errorf("proxy config parse: %w", parseErr),
		}
	}

	return proxyConfigFetchResult{
		content: content,
		bundle:  bundle,
		parsed:  parseResult,
	}
}

func (c *Collector) fetchNginxConfigBundle(ctx context.Context, files filesClient, mainPath string, mainContent string) (map[string]string, error) {
	mainPath = filepath.Clean(mainPath)
	bundle := map[string]string{mainPath: mainContent}

	includePaths, err := parser.NginxIncludePaths(mainPath, mainContent)
	if err != nil {
		return nil, fmt.Errorf("proxy config include parse %s: %w", mainPath, err)
	}
	for _, includePath := range includePaths {
		matches, err := c.expandRemoteIncludePaths(ctx, files, includePath)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			if err := c.fetchNginxConfigRecursive(ctx, files, match, bundle, make(map[string]struct{})); err != nil {
				return nil, err
			}
		}
	}
	return bundle, nil
}

func (c *Collector) fetchNginxConfigRecursive(ctx context.Context, files filesClient, path string, bundle map[string]string, visiting map[string]struct{}) error {
	path = filepath.Clean(path)
	if _, ok := bundle[path]; ok {
		return nil
	}
	if _, ok := visiting[path]; ok {
		return fmt.Errorf("proxy config include cycle: %s", path)
	}
	visiting[path] = struct{}{}
	defer delete(visiting, path)

	content, err := files.Read(ctx, path)
	if err != nil {
		return fmt.Errorf("proxy config read %s: %w", path, err)
	}
	bundle[path] = content

	includePaths, err := parser.NginxIncludePaths(path, content)
	if err != nil {
		return fmt.Errorf("proxy config include parse %s: %w", path, err)
	}
	for _, includePath := range includePaths {
		matches, err := c.expandRemoteIncludePaths(ctx, files, includePath)
		if err != nil {
			return err
		}
		for _, match := range matches {
			if err := c.fetchNginxConfigRecursive(ctx, files, match, bundle, visiting); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Collector) expandRemoteIncludePaths(ctx context.Context, files filesClient, includePath string) ([]string, error) {
	includePath = filepath.Clean(includePath)
	if !hasGlobMeta(includePath) {
		return []string{includePath}, nil
	}

	dir := filepath.Dir(includePath)
	pattern := filepath.Base(includePath)
	entries, err := files.List(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("proxy config list %s: %w", dir, err)
	}

	matches := make([]string, 0)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name, ".") {
			continue
		}
		matched, matchErr := filepath.Match(pattern, entry.Name)
		if matchErr != nil {
			return nil, fmt.Errorf("proxy config include glob %s: %w", includePath, matchErr)
		}
		if !matched || entry.IsDir {
			continue
		}
		matches = append(matches, filepath.Join(dir, entry.Name))
	}
	sort.Strings(matches)
	return matches, nil
}

func hasGlobMeta(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func (c *Collector) applyCollectionSuccess(nodeName core.NodeName) {
	c.mu.Lock()
	defer c.mu.Unlock()

	previous := c.statuses[nodeName]
	current := NodeStatus{
		NodeName:   nodeName,
		Online:     true,
		Degraded:   false,
		LastSeenAt: core.NowTimestamp(),
	}
	c.statuses[nodeName] = current
	c.failures[nodeName] = 0
	c.publishStatusEvent(previous, current)
}

func (c *Collector) markNodeOnline(nodeName core.NodeName) {
	c.mu.Lock()
	defer c.mu.Unlock()

	previous := c.statuses[nodeName]
	current := previous
	current.NodeName = nodeName
	current.Online = true
	current.Degraded = false
	current.LastError = ""
	if current.LastSeenAt.IsZero() {
		current.LastSeenAt = core.NowTimestamp()
	} else {
		current.LastSeenAt = core.NowTimestamp()
	}
	c.statuses[nodeName] = current
	c.publishStatusEvent(previous, current)
}

func (c *Collector) applyCollectionFailure(nodeName core.NodeName, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	previous := c.statuses[nodeName]
	c.failures[nodeName]++
	degraded := c.failures[nodeName] >= degradeThreshold
	if errors.Is(err, errNodeClientUnavailable) {
		degraded = true
		c.failures[nodeName] = degradeThreshold
	}
	current := NodeStatus{
		NodeName:   nodeName,
		Online:     true,
		Degraded:   degraded,
		LastSeenAt: core.NowTimestamp(),
		LastError:  err.Error(),
	}
	c.statuses[nodeName] = current
	c.publishStatusEvent(previous, current)
}

func (c *Collector) markOfflineNodes(seen map[core.NodeName]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for nodeName, previous := range c.statuses {
		if _, ok := seen[nodeName]; ok {
			continue
		}
		if !previous.Online {
			continue
		}
		current := previous
		current.Online = false
		c.statuses[nodeName] = current
		c.publishStatusEvent(previous, current)
	}
}

func (c *Collector) publishStatusEvent(previous, current NodeStatus) {
	if statusTopologyEquivalent(previous, current) {
		return
	}

	core.BroadcastEvent(c.bus, core.EventNodeStatusChanged.String(), NodeStatusEvent{
		Previous: previous,
		Current:  current,
	})
}

func statusTopologyEquivalent(previous, current NodeStatus) bool {
	return previous.NodeName == current.NodeName &&
		previous.Online == current.Online &&
		previous.Degraded == current.Degraded &&
		previous.LastError == current.LastError
}

func firstTailscaleIP(peer tailkittypes.Peer) string {
	if len(peer.Status.TailscaleIPs) == 0 {
		return ""
	}
	return peer.Status.TailscaleIPs[0].String()
}

func normalizeDNSName(value string) string {
	return strings.TrimSuffix(strings.TrimSpace(value), ".")
}

func mapPorts(ports []tailkittypes.Port) []store.ListenPort {
	out := make([]store.ListenPort, 0, len(ports))
	for _, port := range ports {
		out = append(out, mapPort(port))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		if out[i].Addr != out[j].Addr {
			return out[i].Addr < out[j].Addr
		}
		return out[i].Process < out[j].Process
	})
	return out
}

func mapPort(port tailkittypes.Port) store.ListenPort {
	return store.ListenPort{
		Addr:    port.Addr,
		Port:    port.Port,
		Proto:   port.Proto,
		PID:     port.PID,
		Process: port.Process,
	}
}

func mapContainers(containers []container.Summary, services []store.SwarmServicePort) []store.Container {
	out := make([]store.Container, 0, len(containers))
	servicePublishesByName := make(map[string][]store.SwarmServicePort)
	for _, service := range services {
		if service.ServiceName == "" {
			continue
		}
		key := strings.ToLower(service.ServiceName)
		servicePublishesByName[key] = append(servicePublishesByName[key], service)
	}
	for _, summary := range containers {
		if strings.EqualFold(string(summary.State), "exited") {
			continue
		}
		name := firstContainerName(summary.Names)
		serviceName := strings.TrimSpace(summary.Labels["com.docker.swarm.service.name"])
		publishedPorts := mapContainerPublishedPorts(summary, servicePublishesByName[strings.ToLower(serviceName)])
		out = append(out, store.Container{
			ContainerID:    summary.ID,
			ContainerName:  name,
			Image:          summary.Image,
			State:          string(summary.State),
			Status:         summary.Status,
			ServiceName:    serviceName,
			PublishedPorts: publishedPorts,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ServiceName != out[j].ServiceName {
			return out[i].ServiceName < out[j].ServiceName
		}
		if out[i].ContainerName != out[j].ContainerName {
			return out[i].ContainerName < out[j].ContainerName
		}
		return out[i].ContainerID < out[j].ContainerID
	})
	return out
}

func enrichResultsWithSwarmServicePublishes(results []collectNodeResult) {
	servicePublishesByName := make(map[string][]store.SwarmServicePort)
	seen := make(map[string]struct{})
	for _, result := range results {
		for _, service := range result.snapshot.Services {
			if service.ServiceName == "" {
				continue
			}
			serviceNameKey := strings.ToLower(service.ServiceName)
			serviceKey := serviceNameKey + "|" + swarmServicePortKey(service)
			if _, ok := seen[serviceKey]; ok {
				continue
			}
			seen[serviceKey] = struct{}{}
			servicePublishesByName[serviceNameKey] = append(servicePublishesByName[serviceNameKey], service)
		}
	}
	if len(servicePublishesByName) == 0 {
		return
	}

	for i := range results {
		results[i].snapshot.Containers = mergeContainersWithServicePublishes(results[i].snapshot.Containers, servicePublishesByName)
	}
}

func mergeContainersWithServicePublishes(containers []store.Container, servicePublishesByName map[string][]store.SwarmServicePort) []store.Container {
	if len(containers) == 0 || len(servicePublishesByName) == 0 {
		return containers
	}

	out := make([]store.Container, len(containers))
	for i, container := range containers {
		merged := container
		serviceName := strings.ToLower(strings.TrimSpace(container.ServiceName))
		if serviceName != "" && strings.EqualFold(container.State, "running") {
			merged.PublishedPorts = mergePublishedPorts(container.PublishedPorts, servicePublishesByName[serviceName])
		}
		out[i] = merged
	}
	return out
}

func mergePublishedPorts(existing []store.ContainerPublishedPort, servicePublishes []store.SwarmServicePort) []store.ContainerPublishedPort {
	if len(servicePublishes) == 0 {
		return existing
	}

	out := make([]store.ContainerPublishedPort, 0, len(existing)+len(servicePublishes))
	seen := make(map[string]struct{}, len(existing)+len(servicePublishes))
	for _, port := range existing {
		key := containerPublishedPortKey(port)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, port)
	}
	for _, publish := range servicePublishes {
		mapped := store.ContainerPublishedPort{
			HostPort:   publish.HostPort,
			TargetPort: publish.TargetPort,
			Proto:      publish.Proto,
			Source:     "service",
			Mode:       publish.Mode,
		}
		key := containerPublishedPortKey(mapped)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, mapped)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HostPort != out[j].HostPort {
			return out[i].HostPort < out[j].HostPort
		}
		if out[i].TargetPort != out[j].TargetPort {
			return out[i].TargetPort < out[j].TargetPort
		}
		if out[i].Proto != out[j].Proto {
			return out[i].Proto < out[j].Proto
		}
		return out[i].Source < out[j].Source
	})
	return out
}

func mapContainerPublishedPorts(summary container.Summary, servicePublishes []store.SwarmServicePort) []store.ContainerPublishedPort {
	out := make([]store.ContainerPublishedPort, 0)
	seen := make(map[string]struct{})
	for _, port := range summary.Ports {
		if port.PublicPort == 0 {
			continue
		}
		mapped := store.ContainerPublishedPort{
			HostPort:   port.PublicPort,
			TargetPort: port.PrivatePort,
			Proto:      port.Type,
			Source:     "container",
		}
		key := containerPublishedPortKey(mapped)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, mapped)
	}
	if strings.EqualFold(string(summary.State), "running") {
		for _, publish := range servicePublishes {
			mapped := store.ContainerPublishedPort{
				HostPort:   publish.HostPort,
				TargetPort: publish.TargetPort,
				Proto:      publish.Proto,
				Source:     "service",
				Mode:       publish.Mode,
			}
			key := containerPublishedPortKey(mapped)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, mapped)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HostPort != out[j].HostPort {
			return out[i].HostPort < out[j].HostPort
		}
		if out[i].TargetPort != out[j].TargetPort {
			return out[i].TargetPort < out[j].TargetPort
		}
		if out[i].Proto != out[j].Proto {
			return out[i].Proto < out[j].Proto
		}
		return out[i].Source < out[j].Source
	})
	return out
}

func mapSwarmServices(services []swarm.Service) []store.SwarmServicePort {
	out := make([]store.SwarmServicePort, 0)
	seen := make(map[string]struct{})
	for _, service := range services {
		name := strings.TrimSpace(service.Spec.Annotations.Name)
		for _, port := range servicePortConfigs(service) {
			if port.PublishedPort == 0 || port.TargetPort == 0 {
				continue
			}
			mapped := store.SwarmServicePort{
				ServiceID:   service.ID,
				ServiceName: name,
				HostPort:    uint16(port.PublishedPort),
				TargetPort:  uint16(port.TargetPort),
				Proto:       strings.ToLower(string(port.Protocol)),
				Mode:        strings.ToLower(string(port.PublishMode)),
			}
			key := swarmServicePortKey(mapped)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, mapped)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HostPort != out[j].HostPort {
			return out[i].HostPort < out[j].HostPort
		}
		return out[i].ServiceName < out[j].ServiceName
	})
	return out
}

func servicePortConfigs(service swarm.Service) []swarm.PortConfig {
	if len(service.Endpoint.Ports) > 0 {
		return service.Endpoint.Ports
	}
	if service.Spec.EndpointSpec != nil {
		return service.Spec.EndpointSpec.Ports
	}
	return nil
}

func swarmServicePortKey(port store.SwarmServicePort) string {
	return strings.Join([]string{
		port.ServiceID,
		port.ServiceName,
		strconv.FormatUint(uint64(port.HostPort), 10),
		strconv.FormatUint(uint64(port.TargetPort), 10),
		port.Proto,
		port.Mode,
	}, "|")
}

func containerPublishedPortKey(port store.ContainerPublishedPort) string {
	return strings.Join([]string{
		strconv.FormatUint(uint64(port.HostPort), 10),
		strconv.FormatUint(uint64(port.TargetPort), 10),
		port.Proto,
		port.Source,
		port.Mode,
	}, "|")
}

func firstContainerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		parts = append(parts, err.Error())
	}
	if len(parts) == 0 {
		return nil
	}
	sort.Strings(parts)
	return errors.New(strings.Join(parts, "; "))
}

func upsertPort(ports []store.ListenPort, target store.ListenPort) []store.ListenPort {
	for i, port := range ports {
		if samePort(port, target) {
			ports[i] = target
			return ports
		}
	}
	return append(ports, target)
}

func removePort(ports []store.ListenPort, target store.ListenPort) []store.ListenPort {
	filtered := ports[:0]
	for _, port := range ports {
		if samePort(port, target) {
			continue
		}
		filtered = append(filtered, port)
	}
	return filtered
}

func samePort(a, b store.ListenPort) bool {
	return a.Addr == b.Addr && a.Port == b.Port && a.Proto == b.Proto && a.PID == b.PID && a.Process == b.Process
}
