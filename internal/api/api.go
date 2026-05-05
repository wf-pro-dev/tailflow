package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/wf-pro-dev/tailflow/internal/collector"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/sse"
	"github.com/wf-pro-dev/tailflow/internal/store"
	"github.com/wf-pro-dev/tailflow/internal/topology"
	"github.com/wf-pro-dev/tailkit"
)

func Init() {
	tailkit.TTL = 10 * time.Second
}

type proxyConfigStore interface {
	store.ProxyConfigStore
}

type collectorReader interface {
	GetStatus(context.Context) ([]collector.NodeStatus, error)
	PreviewProxyConfig(context.Context, core.NodeName, string, string) (string, map[string]string, parser.ParseResult, error)
	LatestSnapshot(core.NodeName) (store.NodeSnapshot, bool)
	Snapshots() []store.NodeSnapshot
	LocalTailscaleIP(context.Context) (string, error)
}

type topologyReader interface {
	Snapshot() topology.Snapshot
}

type refreshTrigger interface {
	RefreshNow(context.Context) error
}

// Handler wires REST and SSE routes for the tailflow API.
type Handler struct {
	proxyConfigs proxyConfigStore
	collector    collectorReader
	topology     topologyReader
	refresher    refreshTrigger
	bus          *core.EventBus
	parsers      parser.Registry
	mux          *http.ServeMux
}

func NewHandler(
	proxyConfigs proxyConfigStore,
	collector collectorReader,
	topology topologyReader,
	refresher refreshTrigger,
	bus *core.EventBus,
	parsers parser.Registry,
) http.Handler {
	h := &Handler{
		proxyConfigs: proxyConfigs,
		collector:    collector,
		topology:     topology,
		refresher:    refresher,
		bus:          bus,
		parsers:      parsers,
		mux:          http.NewServeMux(),
	}
	h.routes()
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) routes() {
	h.mux.HandleFunc("GET /api/v1/nodes", h.listNodes)
	h.mux.HandleFunc("GET /api/v1/nodes/{name}", h.getNode)
	h.mux.HandleFunc("GET /api/v1/nodes/stream", h.watchNodes)

	h.mux.HandleFunc("GET /api/v1/topology", h.getTopology)
	h.mux.HandleFunc("GET /api/v1/topology/stream", h.watchTopology)

	h.mux.HandleFunc("GET /api/v1/configs", h.listProxyConfigs)
	h.mux.HandleFunc("GET /api/v1/configs/{id}", h.getProxyConfig)
	h.mux.HandleFunc("PUT /api/v1/configs/{node}", h.setProxyConfig)
	h.mux.HandleFunc("DELETE /api/v1/configs/{id}", h.deleteProxyConfig)

	h.mux.HandleFunc("GET /api/v1/health", h.health)
}

type NodeResponse struct {
	Name              core.NodeName    `json:"name"`
	TailscaleIP       string           `json:"tailscale_ip"`
	Online            bool             `json:"online"`
	Degraded          bool             `json:"degraded"`
	CollectorDegraded bool             `json:"collector_degraded"`
	WorkloadDegraded  bool             `json:"workload_degraded"`
	LastSeenAt        core.Timestamp   `json:"last_seen_at"`
	CollectorError    string           `json:"collector_error,omitempty"`
	WorkloadIssues    []string         `json:"workload_issues,omitempty"`
	Snapshot          *SnapshotSummary `json:"snapshot,omitempty"`
}

type SnapshotSummary struct {
	CollectedAt    core.Timestamp `json:"collected_at"`
	PortCount      int            `json:"port_count"`
	ContainerCount int            `json:"container_count"`
	ServiceCount   int            `json:"service_count"`
	ForwardCount   int            `json:"forward_count"`
}

type TopologyResponse = topology.Snapshot
type TopologyNode = topology.Node
type TopologyPatch = topology.Patch
type TopologyReset = topology.Reset

type SetProxyConfigRequest struct {
	Kind       string `json:"kind"`
	ConfigPath string `json:"config_path"`
}

type SetProxyConfigResponse struct {
	Config  parser.ProxyConfigInput `json:"config"`
	Preview parser.ParseResult      `json:"preview"`
}

type ParsedProxyConfigResponse struct {
	Config parser.ProxyConfigInput `json:"config"`
	Parsed parser.ParseResult      `json:"parsed"`
}

type HealthResponse struct {
	Status                     string         `json:"status"`
	NodeCount                  int            `json:"node_count"`
	CollectorDegradedNodeCount int            `json:"collector_degraded_node_count"`
	WorkloadDegradedNodeCount  int            `json:"workload_degraded_node_count"`
	UpdatedAt                  core.Timestamp `json:"updated_at"`
	TopologyVersion            uint64         `json:"topology_version"`
	TailnetIP                  string         `json:"tailnet_ip"`
}

type workloadAssessment struct {
	Degraded bool
	Issues   []string
}

func assessWorkload(snapshot store.NodeSnapshot) workloadAssessment {
	issues := make([]string, 0)
	for _, container := range snapshot.Containers {
		if issue, ok := assessContainerWorkload(container); ok {
			issues = append(issues, issue)
		}
	}
	return workloadAssessment{
		Degraded: len(issues) > 0,
		Issues:   issues,
	}
}

func assessContainerWorkload(container store.Container) (string, bool) {
	state := strings.ToLower(strings.TrimSpace(container.State))
	status := strings.ToLower(strings.TrimSpace(container.Status))
	name := container.ContainerName

	if strings.Contains(status, "unhealthy") {
		return fmt.Sprintf("container %s is unhealthy", name), true
	}

	if container.ServiceName != "" {
		return "", false
	}
	if len(container.PublishedPorts) == 0 {
		return "", false
	}

	switch state {
	case "", "running":
		return "", false
	default:
		return fmt.Sprintf("container %s is %s", name, container.State), true
	}
}

func (h *Handler) buildNodeResponse(ctx context.Context, status collector.NodeStatus) NodeResponse {
	node := NodeResponse{
		Name:              status.NodeName,
		Online:            status.Online,
		Degraded:          status.Degraded,
		CollectorDegraded: status.Degraded,
		LastSeenAt:        status.LastSeenAt,
		CollectorError:    status.LastError,
	}
	snapshot, ok := h.collector.LatestSnapshot(status.NodeName)
	if !ok {
		return node
	}

	node.TailscaleIP = snapshot.TailscaleIP
	node.Snapshot = &SnapshotSummary{
		CollectedAt:    snapshot.CollectedAt,
		PortCount:      len(snapshot.Ports),
		ContainerCount: len(snapshot.Containers),
		ServiceCount:   len(snapshot.Services),
		ForwardCount:   len(snapshot.Forwards),
	}

	workload := assessWorkload(snapshot)
	node.WorkloadDegraded = workload.Degraded
	node.WorkloadIssues = workload.Issues
	node.Degraded = node.Degraded || workload.Degraded
	return node
}

func (h *Handler) listNodes(w http.ResponseWriter, r *http.Request) {
	statuses, err := h.collector.GetStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to load node status")
		return
	}

	nodes := make([]NodeResponse, 0, len(statuses))
	for _, status := range statuses {
		nodes = append(nodes, h.buildNodeResponse(r.Context(), status))
	}

	writeJSON(w, http.StatusOK, nodes)
}

func (h *Handler) getNode(w http.ResponseWriter, r *http.Request) {
	name := core.NodeName(r.PathValue("name"))
	statuses, err := h.collector.GetStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to load node status")
		return
	}

	for _, status := range statuses {
		if status.NodeName != name {
			continue
		}
		writeJSON(w, http.StatusOK, h.buildNodeResponse(r.Context(), status))
		return
	}

	writeError(w, http.StatusNotFound, errors.New("node not found"), "check the node name")
}

func (h *Handler) getTopology(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.topology.Snapshot())
}

func (h *Handler) listProxyConfigs(w http.ResponseWriter, r *http.Request) {

	nodeName := core.NodeName(r.URL.Query().Get("node"))
	if nodeName != "" {
		configs, err := h.proxyConfigs.ListByNode(r.Context(), nodeName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err, "failed to load proxy configs")
			return
		}
		writeJSON(w, http.StatusOK, configs)
		return
	}

	configs, err := h.proxyConfigs.ListAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to load proxy configs")
		return
	}
	writeJSON(w, http.StatusOK, configs)
}

func (h *Handler) getProxyConfig(w http.ResponseWriter, r *http.Request) {
	config, err := h.proxyConfigs.Get(r.Context(), core.ID(r.PathValue("id")))
	if err != nil {
		writeError(w, http.StatusNotFound, err, "check the config id")
		return
	}

	parsed, err := h.loadParsedConfig(r.Context(), config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to load parsed config")
		return
	}

	writeJSON(w, http.StatusOK, ParsedProxyConfigResponse{
		Config: config,
		Parsed: parsed,
	})
}

func (h *Handler) setProxyConfig(w http.ResponseWriter, r *http.Request) {
	var req SetProxyConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err, "request body must be valid JSON")
		return
	}

	nodeName := core.NodeName(r.PathValue("node"))
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	configPath := strings.TrimSpace(req.ConfigPath)

	if kind == "" || configPath == "" {
		writeError(w, http.StatusBadRequest, errors.New("kind and config_path are required"), "provide kind and config_path")
		return
	}
	if _, ok := h.parsers[kind]; !ok {
		writeError(w, http.StatusBadRequest, errors.New("unsupported proxy parser kind"), "use nginx or caddy")
		return
	}

	config := parser.ProxyConfigInput{
		ID:         core.NewID(),
		NodeName:   nodeName,
		Kind:       kind,
		ConfigPath: configPath,
		UpdatedAt:  core.NowTimestamp(),
	}

	content, bundleFiles, preview, err := h.collector.PreviewProxyConfig(r.Context(), nodeName, kind, configPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err, "failed to read or parse the remote config")
		return
	}
	config.Content = content
	config.BundleFiles = bundleFiles

	if existing, err := h.proxyConfigs.GetByNodeAndPath(r.Context(), config.NodeName, config.ConfigPath); err == nil {
		config.ID = existing.ID
	}
	if err := h.proxyConfigs.Save(r.Context(), config); err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to save proxy config")
		return
	}
	if h.refresher != nil {
		if err := h.refresher.RefreshNow(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, err, "failed to refresh topology after saving proxy config")
			return
		}
	}
	writeJSON(w, http.StatusOK, SetProxyConfigResponse{
		Config:  config,
		Preview: preview,
	})
}

func (h *Handler) deleteProxyConfig(w http.ResponseWriter, r *http.Request) {
	config, err := h.proxyConfigs.Get(r.Context(), core.ID(r.PathValue("id")))
	if err != nil {
		writeError(w, http.StatusNotFound, err, "check the config id")
		return
	}
	if err := h.proxyConfigs.Delete(r.Context(), config.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to delete proxy config")
		return
	}
	if h.refresher != nil {
		if err := h.refresher.RefreshNow(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, err, "failed to refresh topology after deleting proxy config")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	statuses, err := h.collector.GetStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to load health status")
		return
	}

	health := HealthResponse{Status: "ok", NodeCount: len(statuses)}
	for _, status := range statuses {
		node := h.buildNodeResponse(r.Context(), status)
		if node.CollectorDegraded {
			health.CollectorDegradedNodeCount++
		}
		if node.WorkloadDegraded {
			health.WorkloadDegradedNodeCount++
		}
	}
	if health.CollectorDegradedNodeCount > 0 || health.WorkloadDegradedNodeCount > 0 {
		health.Status = "degraded"
	}
	snapshot := h.topology.Snapshot()
	health.UpdatedAt = snapshot.UpdatedAt
	health.TopologyVersion = snapshot.Version
	if ip, err := h.collector.LocalTailscaleIP(r.Context()); err == nil {
		health.TailnetIP = ip
	}
	writeJSON(w, http.StatusOK, health)
}

func (h *Handler) loadParsedConfig(ctx context.Context, config parser.ProxyConfigInput) (parser.ParseResult, error) {
	liveContent, bundleFiles, parsed, err := h.collector.PreviewProxyConfig(ctx, config.NodeName, config.Kind, config.ConfigPath)
	if err == nil {
		config.Content = liveContent
		config.BundleFiles = bundleFiles
		if saveErr := h.proxyConfigs.Save(ctx, config); saveErr != nil {
			return parser.ParseResult{}, saveErr
		}
		return parsed, nil
	}

	if len(config.BundleFiles) > 0 {
		return h.parsers.ParseBundle(config.Kind, config.ConfigPath, config.BundleFiles)
	}
	content := strings.TrimSpace(config.Content)
	if content == "" {
		return parser.ParseResult{}, err
	}
	return h.parsers.Parse(config.Kind, content)
}

func (h *Handler) watchNodes(w http.ResponseWriter, r *http.Request) {
	writer, ok := h.newStreamWriter(w, r)
	if !ok {
		return
	}
	if err := h.sendInitialNodeSnapshot(r.Context(), writer); err != nil {
		return
	}
	h.streamTopics(r.Context(), writer, core.TopicNode)
}

func (h *Handler) watchTopology(w http.ResponseWriter, r *http.Request) {
	writer, ok := h.newStreamWriter(w, r)
	if !ok {
		return
	}
	if err := h.sendInitialTopologySnapshot(r.Context(), writer); err != nil {
		return
	}
	h.streamTopics(r.Context(), writer, core.TopicEdge)
}

func (h *Handler) newStreamWriter(w http.ResponseWriter, r *http.Request) (*sse.Writer, bool) {
	writer, err := sse.NewWriter(w, r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, "streaming is not supported")
		return nil, false
	}
	writer.SetSequence(sse.ResumeFrom(r))
	return writer, true
}

func (h *Handler) streamTopics(ctx context.Context, writer *sse.Writer, topics ...core.Topic) {
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	channels := make([]<-chan any, 0, len(topics))
	for _, topic := range topics {
		channels = append(channels, h.bus.Subscribe(ctx, topic))
	}
	merged := merge(ctx, channels...)

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if err := writer.Heartbeat(); err != nil {
				return
			}
		case event, ok := <-merged:
			if !ok {
				return
			}
			switch e := event.(type) {
			case core.Event[collector.NodeStatusEvent]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[collector.NodePortsReplacedEvent]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[collector.PortBoundEvent]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[collector.PortReleasedEvent]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[collector.NodeContainersReplacedEvent]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[collector.NodeServicesReplacedEvent]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[collector.NodeForwardsReplacedEvent]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[collector.SnapshotEvent]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[topology.Patch]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[topology.Reset]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[topology.Snapshot]:
				_ = writer.Send(e.Name, e.Data)
			default:
				_ = writer.Send("message", event)
			}
		}
	}
}

func (h *Handler) sendInitialNodeSnapshot(ctx context.Context, writer *sse.Writer) error {
	statuses, err := h.collector.GetStatus(ctx)
	if err != nil {
		writer.Error(err.Error())
		return err
	}

	nodes := make([]NodeResponse, 0, len(statuses))
	for _, status := range statuses {
		nodes = append(nodes, h.buildNodeResponse(ctx, status))
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return writer.Send("nodes.snapshot", nodes)
}

func (h *Handler) sendInitialTopologySnapshot(ctx context.Context, writer *sse.Writer) error {
	return writer.Send(core.EventTopologySnapshot.String(), h.topology.Snapshot())
}

func merge(ctx context.Context, channels ...<-chan any) <-chan any {
	out := make(chan any)
	for _, ch := range channels {
		go func() {
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
	return out
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, err error, hint string) {
	writeJSON(w, status, map[string]string{
		"error": err.Error(),
		"hint":  hint,
	})
}
