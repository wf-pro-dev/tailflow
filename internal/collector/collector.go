package collector

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/store"
	"github.com/wf-pro-dev/tailkit"
	tailkittypes "github.com/wf-pro-dev/tailkit/types"
)

const degradeThreshold = 3

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
	Containers(context.Context) ([]container.Summary, error)
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

func (d tailkitDockerClient) Containers(ctx context.Context) ([]container.Summary, error) {
	return d.docker.Containers(ctx)
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
	runs         store.RunStore
	snapshots    store.SnapshotStore
	proxyConfigs store.ProxyConfigStore
	bus          *core.EventBus
	parsers      parser.Registry

	discoverPeers func(context.Context, *tailkit.Server) ([]tailkittypes.Peer, error)
	newNode       func(string) nodeClient

	mu       sync.Mutex
	statuses map[core.NodeName]NodeStatus
	failures map[core.NodeName]int
}

type proxyConfigFetchResult struct {
	content string
	bundle  map[string]string
	parsed  parser.ParseResult
	err     error
}

func NewCollector(
	srv *tailkit.Server,
	runs store.RunStore,
	snapshots store.SnapshotStore,
	proxyConfigs store.ProxyConfigStore,
	bus *core.EventBus,
	parsers parser.Registry,
) *Collector {
	c := &Collector{
		srv:           srv,
		runs:          runs,
		snapshots:     snapshots,
		proxyConfigs:  proxyConfigs,
		bus:           bus,
		parsers:       parsers,
		discoverPeers: tailkit.OnlinePeers,
		statuses:      make(map[core.NodeName]NodeStatus),
		failures:      make(map[core.NodeName]int),
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

// RunOnce executes one collection cycle across all online peers.
func (c *Collector) RunOnce(ctx context.Context) (store.CollectionRun, error) {
	run := store.CollectionRun{
		ID:        core.NewID(),
		StartedAt: core.NowTimestamp(),
	}

	peers, err := c.discoverPeers(ctx, c.srv)
	if err != nil {
		return run, fmt.Errorf("discover peers: %w", err)
	}
	run.NodeCount = len(peers)

	type result struct {
		peer     tailkittypes.Peer
		snapshot store.NodeSnapshot
		err      error
	}

	results := make(chan result, len(peers))
	var wg sync.WaitGroup
	for _, peer := range peers {
		peer := peer
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot, err := c.collectNode(ctx, peer, run.ID)
			results <- result{peer: peer, snapshot: snapshot, err: err}
		}()
	}

	wg.Wait()
	close(results)

	seen := make(map[core.NodeName]struct{}, len(peers))
	errs := make(map[core.NodeName]error)
	for res := range results {
		nodeName := core.NodeName(res.peer.Status.HostName)
		seen[nodeName] = struct{}{}
		if saveErr := c.snapshots.Save(ctx, res.snapshot); saveErr != nil {
			errs[nodeName] = fmt.Errorf("save snapshot: %w", saveErr)
			run.ErrorCount++
			c.applyCollectionFailure(nodeName, errs[nodeName])
			continue
		}

		if res.err != nil {
			errs[nodeName] = res.err
			run.ErrorCount++
			c.applyCollectionFailure(nodeName, res.err)
		} else {
			c.applyCollectionSuccess(nodeName)
		}

		core.BroadcastEvent(c.bus, "snapshot.updated", SnapshotEvent{
			RunID:    run.ID,
			Snapshot: res.snapshot,
		})
	}

	c.markOfflineNodes(seen)

	run.FinishedAt = core.NowTimestamp()
	if c.runs != nil {
		if err := c.runs.Save(ctx, run); err != nil {
			return run, fmt.Errorf("save collection run: %w", err)
		}
	}
	return run, core.MergeErrors(errs)
}

// WatchNode patches the latest stored snapshot from the live port stream.
func (c *Collector) WatchNode(ctx context.Context, nodeName core.NodeName) error {
	node := c.newNode(string(nodeName))
	if node == nil {
		return fmt.Errorf("create node client for %s", nodeName)
	}

	return node.Metrics().StreamPorts(ctx, func(event tailkittypes.Event[tailkittypes.PortUpdate]) error {
		snapshot, err := c.snapshots.LatestByNode(ctx, nodeName)
		if err != nil {
			return err
		}

		switch event.Data.Kind {
		case "snapshot":
			snapshot.Ports = mapPorts(event.Data.Ports)
		case "bound":
			port := mapPort(event.Data.Port)
			snapshot.Ports = upsertPort(snapshot.Ports, port)
			core.BroadcastEvent(c.bus, "port.bound", PortBoundEvent{NodeName: nodeName, Port: port})
		case "released":
			port := mapPort(event.Data.Port)
			snapshot.Ports = removePort(snapshot.Ports, port)
			core.BroadcastEvent(c.bus, "port.released", PortReleasedEvent{NodeName: nodeName, Port: port})
		}

		snapshot.CollectedAt = core.NowTimestamp()
		if err := c.snapshots.Save(ctx, snapshot); err != nil {
			return err
		}
		core.BroadcastEvent(c.bus, "snapshot.updated", SnapshotEvent{
			RunID:    snapshot.RunID,
			Snapshot: snapshot,
		})
		return nil
	})
}

// GetStatus returns current collector node status state.
func (c *Collector) GetStatus(ctx context.Context) ([]NodeStatus, error) {
	c.mu.Lock()
	statusMap := make(map[core.NodeName]NodeStatus, len(c.statuses))
	for nodeName, status := range c.statuses {
		statusMap[nodeName] = status
	}
	c.mu.Unlock()

	if c.srv != nil && c.discoverPeers != nil {
		if peers, err := c.discoverPeers(ctx, c.srv); err == nil {
			now := core.NowTimestamp()
			seen := make(map[core.NodeName]struct{}, len(peers))
			for _, peer := range peers {
				nodeName := core.NodeName(peer.Status.HostName)
				seen[nodeName] = struct{}{}
				status := statusMap[nodeName]
				status.NodeName = nodeName
				status.Online = true
				if status.LastSeenAt.IsZero() {
					status.LastSeenAt = now
				}
				statusMap[nodeName] = status
			}
			for nodeName, status := range statusMap {
				if _, ok := seen[nodeName]; ok {
					continue
				}
				status.Online = false
				statusMap[nodeName] = status
			}
		}
	}

	statuses := make([]NodeStatus, 0, len(statusMap))
	for _, status := range statusMap {
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

	result := c.readAndParseProxyConfig(ctx, node.Files(), kind, configPath)
	return result.content, result.bundle, result.parsed, result.err
}

func (c *Collector) collectNode(ctx context.Context, peer tailkittypes.Peer, runID core.ID) (store.NodeSnapshot, error) {
	nodeName := core.NodeName(peer.Status.HostName)
	snapshot := store.NodeSnapshot{
		ID:          core.NewID(),
		RunID:       runID,
		NodeName:    nodeName,
		TailscaleIP: firstTailscaleIP(peer),
		CollectedAt: core.NowTimestamp(),
	}

	node := c.newNode(string(nodeName))
	if node == nil {
		err := fmt.Errorf("tailkit node client unavailable")
		snapshot.Error = err.Error()
		return snapshot, err
	}

	var partialErrs []error

	ports, err := node.Metrics().Ports(ctx)
	if err != nil {
		partialErrs = append(partialErrs, fmt.Errorf("ports: %w", err))
	} else {
		snapshot.Ports = mapPorts(ports)
	}

	containers, err := node.Docker().Containers(ctx)
	if err != nil && !errors.Is(err, tailkittypes.ErrDockerUnavailable) {
		partialErrs = append(partialErrs, fmt.Errorf("docker: %w", err))
	} else if err == nil {
		snapshot.Containers = mapContainers(containers)
	}

	if c.proxyConfigs != nil {
		configs, err := c.proxyConfigs.ListByNode(ctx, nodeName)
		if err == nil {
			allForwards := make([]parser.ForwardAction, 0)
			for _, config := range configs {
				result := c.readAndParseProxyConfig(ctx, node.Files(), config.Kind, config.ConfigPath)
				allForwards = append(allForwards, result.parsed.Forwards...)
				for _, warning := range result.parsed.Errors {
					partialErrs = append(partialErrs, errors.New("proxy config warning: "+warning))
				}
				if result.err != nil {
					partialErrs = append(partialErrs, result.err)
				}
			}
			snapshot.Forwards = parser.DedupeForwards(allForwards)
		}
	}

	merged := joinErrors(partialErrs)
	if merged != nil {
		snapshot.Error = merged.Error()
	}
	return snapshot, merged
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
		bundle, err = c.fetchNginxConfigBundle(ctx, files, configPath)
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

func (c *Collector) fetchNginxConfigBundle(ctx context.Context, files filesClient, mainPath string) (map[string]string, error) {
	bundle := make(map[string]string)
	visiting := make(map[string]struct{})
	if err := c.fetchNginxConfigRecursive(ctx, files, mainPath, bundle, visiting); err != nil {
		return nil, err
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

func (c *Collector) applyCollectionFailure(nodeName core.NodeName, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	previous := c.statuses[nodeName]
	c.failures[nodeName]++
	current := NodeStatus{
		NodeName:   nodeName,
		Online:     true,
		Degraded:   c.failures[nodeName] >= degradeThreshold,
		LastSeenAt: previous.LastSeenAt,
		LastError:  err.Error(),
	}
	if current.LastSeenAt.IsZero() {
		current.LastSeenAt = core.NowTimestamp()
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
	if current == previous {
		return
	}

	var eventName string
	switch {
	case current.Online && !previous.Online:
		eventName = "node.connected"
	case !current.Online && previous.Online:
		eventName = "node.disconnected"
	case current.Degraded && !previous.Degraded:
		eventName = "node.degraded"
	default:
		return
	}

	core.BroadcastEvent(c.bus, eventName, NodeStatusEvent{
		Previous: previous,
		Current:  current,
	})
}

func firstTailscaleIP(peer tailkittypes.Peer) string {
	if len(peer.Status.TailscaleIPs) == 0 {
		return ""
	}
	return peer.Status.TailscaleIPs[0].String()
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

func mapContainers(containers []container.Summary) []store.ContainerPort {
	out := make([]store.ContainerPort, 0)
	seen := make(map[string]struct{})
	for _, summary := range containers {
		name := firstContainerName(summary.Names)
		for _, port := range summary.Ports {
			if port.PublicPort == 0 {
				continue
			}
			mapped := store.ContainerPort{
				ContainerID:   summary.ID,
				ContainerName: name,
				HostPort:      port.PublicPort,
				ContainerPort: port.PrivatePort,
				Proto:         port.Type,
			}
			key := containerPortKey(mapped)
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
		return out[i].ContainerName < out[j].ContainerName
	})
	return out
}

func containerPortKey(port store.ContainerPort) string {
	return strings.Join([]string{
		port.ContainerID,
		port.ContainerName,
		strconv.FormatUint(uint64(port.HostPort), 10),
		strconv.FormatUint(uint64(port.ContainerPort), 10),
		port.Proto,
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
