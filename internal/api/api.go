package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/wf-pro-dev/tailflow/internal/collector"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/resolver"
	"github.com/wf-pro-dev/tailflow/internal/sse"
	"github.com/wf-pro-dev/tailflow/internal/store"
)

type runStore interface {
	store.RunStore
}

type snapshotStore interface {
	store.SnapshotStore
}

type edgeStore interface {
	store.EdgeStore
}

type proxyConfigStore interface {
	store.ProxyConfigStore
}

type collectorReader interface {
	GetStatus(context.Context) ([]collector.NodeStatus, error)
	PreviewProxyConfig(context.Context, core.NodeName, string, string) (string, map[string]string, parser.ParseResult, error)
}

type triggerer interface {
	Trigger()
}

// Handler wires REST and SSE routes for the tailflow API.
type Handler struct {
	runs         runStore
	snapshots    snapshotStore
	edges        edgeStore
	proxyConfigs proxyConfigStore
	collector    collectorReader
	scheduler    triggerer
	bus          *core.EventBus
	parsers      parser.Registry
	mux          *http.ServeMux
}

func NewHandler(
	runs runStore,
	snapshots snapshotStore,
	edges edgeStore,
	proxyConfigs proxyConfigStore,
	collector collectorReader,
	scheduler triggerer,
	bus *core.EventBus,
	parsers parser.Registry,
) http.Handler {
	h := &Handler{
		runs:         runs,
		snapshots:    snapshots,
		edges:        edges,
		proxyConfigs: proxyConfigs,
		collector:    collector,
		scheduler:    scheduler,
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
	h.mux.HandleFunc("GET /api/v1/nodes/{name}/snapshot", h.getLatestSnapshot)
	h.mux.HandleFunc("GET /api/v1/nodes/stream", h.watchNodes)

	h.mux.HandleFunc("GET /api/v1/topology", h.getTopology)
	h.mux.HandleFunc("GET /api/v1/topology/edges", h.listEdges)
	h.mux.HandleFunc("GET /api/v1/topology/edges/unresolved", h.listUnresolvedEdges)
	h.mux.HandleFunc("GET /api/v1/topology/stream", h.watchTopology)

	h.mux.HandleFunc("GET /api/v1/runs", h.listRuns)
	h.mux.HandleFunc("GET /api/v1/runs/{id}", h.getRun)
	h.mux.HandleFunc("GET /api/v1/runs/{id}/snapshots", h.listRunSnapshots)
	h.mux.HandleFunc("POST /api/v1/runs", h.triggerRun)

	h.mux.HandleFunc("GET /api/v1/configs", h.listProxyConfigs)
	h.mux.HandleFunc("GET /api/v1/configs/{id}", h.getProxyConfig)
	h.mux.HandleFunc("PUT /api/v1/configs/{node}", h.setProxyConfig)
	h.mux.HandleFunc("DELETE /api/v1/configs/{id}", h.deleteProxyConfig)

	h.mux.HandleFunc("GET /api/v1/health", h.health)
}

type NodeResponse struct {
	Name        core.NodeName    `json:"name"`
	TailscaleIP string           `json:"tailscale_ip"`
	Online      bool             `json:"online"`
	Degraded    bool             `json:"degraded"`
	LastSeenAt  core.Timestamp   `json:"last_seen_at"`
	Snapshot    *SnapshotSummary `json:"snapshot,omitempty"`
}

type SnapshotSummary struct {
	CollectedAt    core.Timestamp `json:"collected_at"`
	PortCount      int            `json:"port_count"`
	ContainerCount int            `json:"container_count"`
	ForwardCount   int            `json:"forward_count"`
}

type TopologyResponse struct {
	RunID     core.ID              `json:"run_id"`
	Nodes     []TopologyNode       `json:"nodes"`
	Edges     []store.TopologyEdge `json:"edges"`
	UpdatedAt core.Timestamp       `json:"updated_at"`
}

type TopologyNode struct {
	Name        core.NodeName         `json:"name"`
	TailscaleIP string                `json:"tailscale_ip"`
	Online      bool                  `json:"online"`
	Ports       []store.ListenPort    `json:"ports"`
	Containers  []store.ContainerPort `json:"containers"`
}

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

type TriggerRunResponse struct {
	Accepted  bool           `json:"accepted"`
	StartedAt core.Timestamp `json:"started_at"`
}

type HealthResponse struct {
	Status    string         `json:"status"`
	NodeCount int            `json:"node_count"`
	LastRunAt core.Timestamp `json:"last_run_at"`
	TailnetIP string         `json:"tailnet_ip"`
}

func (h *Handler) listNodes(w http.ResponseWriter, r *http.Request) {
	statuses, err := h.collector.GetStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to load node status")
		return
	}

	nodes := make([]NodeResponse, 0, len(statuses))
	for _, status := range statuses {
		node := NodeResponse{
			Name:       status.NodeName,
			Online:     status.Online,
			Degraded:   status.Degraded,
			LastSeenAt: status.LastSeenAt,
		}
		if h.snapshots != nil {
			if snapshot, err := h.snapshots.LatestByNode(r.Context(), status.NodeName); err == nil {
				node.TailscaleIP = snapshot.TailscaleIP
				node.Snapshot = &SnapshotSummary{
					CollectedAt:    snapshot.CollectedAt,
					PortCount:      len(snapshot.Ports),
					ContainerCount: len(snapshot.Containers),
					ForwardCount:   len(snapshot.Forwards),
				}
			}
		}
		nodes = append(nodes, node)
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
		node := NodeResponse{
			Name:       status.NodeName,
			Online:     status.Online,
			Degraded:   status.Degraded,
			LastSeenAt: status.LastSeenAt,
		}
		if snapshot, err := h.snapshots.LatestByNode(r.Context(), name); err == nil {
			node.TailscaleIP = snapshot.TailscaleIP
			node.Snapshot = &SnapshotSummary{
				CollectedAt:    snapshot.CollectedAt,
				PortCount:      len(snapshot.Ports),
				ContainerCount: len(snapshot.Containers),
				ForwardCount:   len(snapshot.Forwards),
			}
		}
		writeJSON(w, http.StatusOK, node)
		return
	}

	writeError(w, http.StatusNotFound, errors.New("node not found"), "check the node name")
}

func (h *Handler) getLatestSnapshot(w http.ResponseWriter, r *http.Request) {
	name := core.NodeName(r.PathValue("name"))
	snapshot, err := h.snapshots.LatestByNode(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, err, "check the node name")
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (h *Handler) getTopology(w http.ResponseWriter, r *http.Request) {
	run, err := h.runs.Latest(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, err, "no collection run is available yet")
		return
	}

	snapshots, err := h.snapshots.ListByRun(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to load run snapshots")
		return
	}
	edges, err := h.edges.ListByRun(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to load topology edges")
		return
	}

	statusByName := make(map[core.NodeName]collector.NodeStatus)
	if statuses, err := h.collector.GetStatus(r.Context()); err == nil {
		for _, status := range statuses {
			statusByName[status.NodeName] = status
		}
	}

	nodes := make([]TopologyNode, 0, len(snapshots))
	for _, snapshot := range snapshots {
		status := statusByName[snapshot.NodeName]
		nodes = append(nodes, TopologyNode{
			Name:        snapshot.NodeName,
			TailscaleIP: snapshot.TailscaleIP,
			Online:      status.Online,
			Ports:       snapshot.Ports,
			Containers:  snapshot.Containers,
		})
	}

	writeJSON(w, http.StatusOK, TopologyResponse{
		RunID:     run.ID,
		Nodes:     nodes,
		Edges:     edges,
		UpdatedAt: run.FinishedAt,
	})
}

func (h *Handler) listEdges(w http.ResponseWriter, r *http.Request) {
	edges, err := h.edges.LatestEdges(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to load edges")
		return
	}
	writeJSON(w, http.StatusOK, edges)
}

func (h *Handler) listUnresolvedEdges(w http.ResponseWriter, r *http.Request) {
	edges, err := h.edges.ListUnresolved(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to load unresolved edges")
		return
	}
	writeJSON(w, http.StatusOK, edges)
}

func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := h.runs.List(r.Context(), core.Filter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to load collection runs")
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := h.runs.Get(r.Context(), core.ID(r.PathValue("id")))
	if err != nil {
		writeError(w, http.StatusNotFound, err, "check the run id")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) listRunSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshots, err := h.snapshots.ListByRun(r.Context(), core.ID(r.PathValue("id")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, "failed to load run snapshots")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": snapshots})
}

func (h *Handler) triggerRun(w http.ResponseWriter, r *http.Request) {
	if h.scheduler != nil {
		h.scheduler.Trigger()
	}
	writeJSON(w, http.StatusAccepted, TriggerRunResponse{
		Accepted:  true,
		StartedAt: core.NowTimestamp(),
	})
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
	if h.scheduler != nil {
		h.scheduler.Trigger()
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
		if status.Degraded {
			health.Status = "degraded"
			break
		}
	}
	if run, err := h.runs.Latest(r.Context()); err == nil {
		health.LastRunAt = run.FinishedAt
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
	h.streamTopics(r.Context(), writer, core.TopicNode, core.TopicSnapshot)
}

func (h *Handler) watchTopology(w http.ResponseWriter, r *http.Request) {
	writer, ok := h.newStreamWriter(w, r)
	if !ok {
		return
	}
	if err := h.sendInitialTopologySnapshot(r.Context(), writer); err != nil {
		return
	}
	h.streamTopics(r.Context(), writer, core.TopicEdge, core.TopicSnapshot)
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
			case core.Event[collector.SnapshotEvent]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[collector.PortBoundEvent]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[collector.PortReleasedEvent]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[resolver.EdgeEvent]:
				_ = writer.Send(e.Name, e.Data)
			case core.Event[store.CollectionRun]:
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
		node := NodeResponse{
			Name:       status.NodeName,
			Online:     status.Online,
			Degraded:   status.Degraded,
			LastSeenAt: status.LastSeenAt,
		}
		if h.snapshots != nil {
			if snapshot, err := h.snapshots.LatestByNode(ctx, status.NodeName); err == nil {
				node.TailscaleIP = snapshot.TailscaleIP
				node.Snapshot = &SnapshotSummary{
					CollectedAt:    snapshot.CollectedAt,
					PortCount:      len(snapshot.Ports),
					ContainerCount: len(snapshot.Containers),
					ForwardCount:   len(snapshot.Forwards),
				}
			}
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return writer.Send("nodes.snapshot", nodes)
}

func (h *Handler) sendInitialTopologySnapshot(ctx context.Context, writer *sse.Writer) error {
	if h.runs == nil || h.snapshots == nil || h.edges == nil {
		return writer.Send("topology.snapshot", TopologyResponse{})
	}

	run, err := h.runs.Latest(ctx)
	if err != nil {
		return writer.Send("topology.snapshot", TopologyResponse{})
	}

	snapshots, err := h.snapshots.ListByRun(ctx, run.ID)
	if err != nil {
		writer.Error(err.Error())
		return err
	}
	edges, err := h.edges.ListByRun(ctx, run.ID)
	if err != nil {
		writer.Error(err.Error())
		return err
	}

	statusByName := make(map[core.NodeName]collector.NodeStatus)
	if h.collector != nil {
		if statuses, err := h.collector.GetStatus(ctx); err == nil {
			for _, status := range statuses {
				statusByName[status.NodeName] = status
			}
		}
	}

	nodes := make([]TopologyNode, 0, len(snapshots))
	for _, snapshot := range snapshots {
		status := statusByName[snapshot.NodeName]
		nodes = append(nodes, TopologyNode{
			Name:        snapshot.NodeName,
			TailscaleIP: snapshot.TailscaleIP,
			Online:      status.Online,
			Ports:       snapshot.Ports,
			Containers:  snapshot.Containers,
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

	return writer.Send("topology.snapshot", TopologyResponse{
		RunID:     run.ID,
		Nodes:     nodes,
		Edges:     edges,
		UpdatedAt: run.FinishedAt,
	})
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
