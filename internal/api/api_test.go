package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wf-pro-dev/tailflow/internal/collector"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/resolver"
	"github.com/wf-pro-dev/tailflow/internal/store"
)

func TestListNodes(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	if err := deps.snapshots.Save(context.Background(), store.NodeSnapshot{
		ID:          "snap-1",
		RunID:       "run-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 80}},
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	handler := newHandlerForTest(deps, fakeCollectorReader{statuses: []collector.NodeStatus{{
		NodeName:   "node-a",
		Online:     true,
		LastSeenAt: core.NowTimestamp(),
	}}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"name":"node-a"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestListNodesIncludesSeparateCollectorAndWorkloadDegradation(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	if err := deps.snapshots.Save(context.Background(), store.NodeSnapshot{
		ID:          "snap-1",
		RunID:       "run-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Containers: []store.Container{{
			ContainerID:   "container-1",
			ContainerName: "api",
			State:         "running",
			Status:        "Up 2 hours (unhealthy)",
			PublishedPorts: []store.ContainerPublishedPort{{
				HostPort:   8080,
				TargetPort: 8080,
				Proto:      "tcp",
				Source:     "container",
			}},
		}},
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	handler := newHandlerForTest(deps, fakeCollectorReader{statuses: []collector.NodeStatus{{
		NodeName:   "node-a",
		Online:     true,
		LastSeenAt: core.NowTimestamp(),
	}}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"collector_degraded":false`) {
		t.Fatalf("body = %s", body)
	}
	if !strings.Contains(body, `"workload_degraded":true`) {
		t.Fatalf("body = %s", body)
	}
	if !strings.Contains(body, `"degraded":true`) {
		t.Fatalf("body = %s", body)
	}
	if !strings.Contains(body, `container api is unhealthy`) {
		t.Fatalf("body = %s", body)
	}
}

func TestTriggerRun(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	triggers := 0
	handler := NewHandler(
		deps.runs, deps.snapshots, deps.edges, deps.proxyConfigs,
		fakeCollectorReader{}, triggerFunc(func() { triggers++ }),
		deps.bus, parser.NewRegistry(),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if triggers != 1 {
		t.Fatalf("triggers = %d, want 1", triggers)
	}
}

func TestSetProxyConfig(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	handler := newHandlerForTest(deps, fakeCollectorReader{
		previewContent: map[core.NodeName]string{
			"node-a": `
server {
    listen 80;
    server_name app.example.com;
    location / {
        proxy_pass http://127.0.0.1:3000;
    }
}`,
		},
	})

	body := bytes.NewBufferString(`{"kind":"nginx","config_path":"/etc/nginx/nginx.conf"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/configs/node-a", body)
	req.SetPathValue("node", "node-a")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	config, err := deps.proxyConfigs.GetByNodeAndPath(context.Background(), "node-a", "/etc/nginx/nginx.conf")
	if err != nil {
		t.Fatalf("GetByNodeAndPath(): %v", err)
	}
	if config.Kind != "nginx" {
		t.Fatalf("config.Kind = %s, want nginx", config.Kind)
	}
	if !strings.Contains(config.Content, "proxy_pass http://127.0.0.1:3000;") {
		t.Fatalf("config.Content = %q", config.Content)
	}
	if len(config.BundleFiles) != 1 || !strings.Contains(config.BundleFiles["/etc/nginx/nginx.conf"], "proxy_pass http://127.0.0.1:3000;") {
		t.Fatalf("config.BundleFiles = %#v", config.BundleFiles)
	}
	if !strings.Contains(rec.Body.String(), `"listener":{"port":80}`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestGetProxyConfigReturnsParsedConfigByID(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	config := parser.ProxyConfigInput{
		ID:         "cfg-1",
		NodeName:   "node-a",
		Kind:       "nginx",
		ConfigPath: "/etc/nginx/nginx.conf",
		Content: `
server {
    listen 80;
    server_name app.example.com;
    location / {
        proxy_pass http://127.0.0.1:3000;
    }
}`,
		UpdatedAt: core.NowTimestamp(),
	}
	if err := deps.proxyConfigs.Save(context.Background(), config); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	handler := newHandlerForTest(deps, fakeCollectorReader{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/configs/cfg-1", nil)
	req.SetPathValue("id", "cfg-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"config":{"id":"cfg-1"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"parsed":{"forwards":[{"listener":{"port":80}`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestGetProxyConfigFallsBackToStoredBundle(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	config := parser.ProxyConfigInput{
		ID:         "cfg-include",
		NodeName:   "node-a",
		Kind:       "nginx",
		ConfigPath: "/etc/nginx/nginx.conf",
		Content:    `include /etc/nginx/conf.d/*.conf;`,
		BundleFiles: map[string]string{
			"/etc/nginx/nginx.conf": `include /etc/nginx/conf.d/*.conf;`,
			"/etc/nginx/conf.d/app.conf": `
server {
    listen 8080;
    location / {
        proxy_pass http://127.0.0.1:3000;
    }
}`,
		},
		UpdatedAt: core.NowTimestamp(),
	}
	if err := deps.proxyConfigs.Save(context.Background(), config); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	handler := newHandlerForTest(deps, fakeCollectorReader{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/configs/cfg-include", nil)
	req.SetPathValue("id", "cfg-include")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"listener":{"port":8080}`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestListProxyConfigsReturnsAllConfigsWithoutNodeFilter(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	ctx := context.Background()
	for _, cfg := range []parser.ProxyConfigInput{
		{
			ID:         "cfg-1",
			NodeName:   "node-a",
			Kind:       "nginx",
			ConfigPath: "/etc/nginx/nginx.conf",
			Content:    "server { listen 80; }",
			UpdatedAt:  core.NowTimestamp(),
		},
		{
			ID:         "cfg-2",
			NodeName:   "node-b",
			Kind:       "caddy",
			ConfigPath: "/etc/caddy/Caddyfile",
			Content:    "example.com { reverse_proxy localhost:3000 }",
			UpdatedAt:  core.NowTimestamp(),
		},
	} {
		if err := deps.proxyConfigs.Save(ctx, cfg); err != nil {
			t.Fatalf("Save(): %v", err)
		}
	}

	handler := newHandlerForTest(deps, fakeCollectorReader{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/configs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":"cfg-1"`) || !strings.Contains(rec.Body.String(), `"id":"cfg-2"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestGetTopology(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	ctx := context.Background()
	run := store.CollectionRun{ID: "run-1", FinishedAt: core.NowTimestamp()}
	if err := deps.runs.Save(ctx, run); err != nil {
		t.Fatalf("save run: %v", err)
	}
	snapA := store.NodeSnapshot{
		ID:          "snap-a",
		RunID:       "run-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
		Forwards: []parser.ForwardAction{{
			Listener: parser.Listener{Port: 80},
			Target: parser.ForwardTarget{
				Raw:  "http://100.64.0.2:8080",
				Kind: parser.TargetKindAddress,
				Host: "100.64.0.2",
				Port: 8080,
			},
		}},
	}
	snapB := store.NodeSnapshot{
		ID:          "snap-b",
		RunID:       "run-1",
		NodeName:    "node-b",
		TailscaleIP: "100.64.0.2",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 8080, Process: "app"}},
	}
	for _, snapshot := range []store.NodeSnapshot{snapA, snapB} {
		if err := deps.snapshots.Save(ctx, snapshot); err != nil {
			t.Fatalf("save snapshot: %v", err)
		}
	}
	res := resolver.NewResolver(deps.edges, deps.snapshots, deps.bus)
	if _, err := res.ResolveRun(ctx, run, []store.NodeSnapshot{snapA, snapB}); err != nil {
		t.Fatalf("ResolveRun(): %v", err)
	}

	handler := newHandlerForTest(deps, fakeCollectorReader{statuses: []collector.NodeStatus{
		{NodeName: "node-a", Online: true},
		{NodeName: "node-b", Online: true},
	}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/topology", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var topology TopologyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &topology); err != nil {
		t.Fatalf("decode topology: %v", err)
	}
	if topology.RunID != "run-1" {
		t.Fatalf("topology.RunID = %q, want run-1", topology.RunID)
	}
	if len(topology.Routes) != 1 {
		t.Fatalf("len(topology.Routes) = %d, want 1", len(topology.Routes))
	}
	if len(topology.Services) < 2 {
		t.Fatalf("len(topology.Services) = %d, want at least 2", len(topology.Services))
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"edges":`)) {
		t.Fatalf("body unexpectedly includes legacy edges: %s", rec.Body.String())
	}
}

func TestHealth(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	if err := deps.runs.Save(context.Background(), store.CollectionRun{
		ID:         "run-1",
		FinishedAt: core.NowTimestamp(),
	}); err != nil {
		t.Fatalf("save run: %v", err)
	}
	handler := newHandlerForTest(deps, fakeCollectorReader{statuses: []collector.NodeStatus{{NodeName: "node-a", Degraded: true}}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var health HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if health.Status != "degraded" {
		t.Fatalf("health.Status = %s, want degraded", health.Status)
	}
	if health.CollectorDegradedNodeCount != 1 || health.WorkloadDegradedNodeCount != 0 {
		t.Fatalf("health = %#v, want collector degraded count only", health)
	}
}

func TestHealthDegradesForWorkloadIssues(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	if err := deps.runs.Save(context.Background(), store.CollectionRun{
		ID:         "run-1",
		FinishedAt: core.NowTimestamp(),
	}); err != nil {
		t.Fatalf("save run: %v", err)
	}
	if err := deps.snapshots.Save(context.Background(), store.NodeSnapshot{
		ID:          "snap-1",
		RunID:       "run-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Containers: []store.Container{{
			ContainerID:   "container-1",
			ContainerName: "api",
			State:         "running",
			Status:        "Up 2 hours (unhealthy)",
			PublishedPorts: []store.ContainerPublishedPort{{
				HostPort:   8080,
				TargetPort: 8080,
				Proto:      "tcp",
				Source:     "container",
			}},
		}},
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	handler := newHandlerForTest(deps, fakeCollectorReader{statuses: []collector.NodeStatus{{NodeName: "node-a"}}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var health HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if health.Status != "degraded" {
		t.Fatalf("health.Status = %s, want degraded", health.Status)
	}
	if health.CollectorDegradedNodeCount != 0 || health.WorkloadDegradedNodeCount != 1 {
		t.Fatalf("health = %#v, want workload degraded count only", health)
	}
}

func TestWatchNodesSendsInitialSnapshot(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	if err := deps.snapshots.Save(context.Background(), store.NodeSnapshot{
		ID:          "snap-1",
		RunID:       "run-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 80}},
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	handler := newHandlerForTest(deps, fakeCollectorReader{statuses: []collector.NodeStatus{{
		NodeName:   "node-a",
		Online:     true,
		LastSeenAt: core.NowTimestamp(),
	}}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/stream", nil).WithContext(ctx)
	rec := newStreamingRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rec, req)
		close(done)
	}()

	waitForBody(t, rec, "event: nodes.snapshot")
	if !strings.Contains(rec.Body.String(), `"name":"node-a"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected nodes stream to stop after cancel")
	}
}

func TestWatchNodesStreamsSnapshotUpdates(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	handler := newHandlerForTest(deps, fakeCollectorReader{statuses: []collector.NodeStatus{{
		NodeName:   "node-a",
		Online:     true,
		LastSeenAt: core.NowTimestamp(),
	}}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/stream", nil).WithContext(ctx)
	rec := newStreamingRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rec, req)
		close(done)
	}()

	waitForBody(t, rec, "event: nodes.snapshot")
	core.BroadcastEvent(deps.bus, "snapshot.updated", collector.SnapshotEvent{
		RunID: "run-1",
		Snapshot: store.NodeSnapshot{
			ID:          "snap-1",
			RunID:       "run-1",
			NodeName:    "node-a",
			TailscaleIP: "100.64.0.1",
			CollectedAt: core.NowTimestamp(),
			Ports:       []store.ListenPort{{Port: 80}},
		},
	})
	waitForBody(t, rec, "event: snapshot.updated")

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected nodes stream to stop after cancel")
	}
}

func TestWatchTopologySendsInitialSnapshot(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	ctx := context.Background()
	run := store.CollectionRun{ID: "run-1", FinishedAt: core.NowTimestamp()}
	if err := deps.runs.Save(ctx, run); err != nil {
		t.Fatalf("save run: %v", err)
	}
	snapA := store.NodeSnapshot{
		ID:          "snap-a",
		RunID:       "run-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
	}
	if err := deps.snapshots.Save(ctx, snapA); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	handler := newHandlerForTest(deps, fakeCollectorReader{statuses: []collector.NodeStatus{{NodeName: "node-a", Online: true}}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/topology/stream", nil).WithContext(ctx)
	rec := newStreamingRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rec, req)
		close(done)
	}()

	waitForBody(t, rec, "event: topology.snapshot")
	if !strings.Contains(rec.Body.String(), `"run_id":"run-1"`) ||
		!strings.Contains(rec.Body.String(), `"routes":[`) ||
		!strings.Contains(rec.Body.String(), `"services":[`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"edges":`) {
		t.Fatalf("body unexpectedly includes legacy edges: %s", rec.Body.String())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected topology stream to stop after cancel")
	}
}

type testDeps struct {
	runs         store.RunStore
	snapshots    store.SnapshotStore
	edges        store.EdgeStore
	proxyConfigs store.ProxyConfigStore
	bus          *core.EventBus
}

func newTestDeps(t *testing.T) testDeps {
	t.Helper()

	path := filepath.Join(t.TempDir(), "tailflow-api.sqlite")
	db, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite(): %v", err)
	}
	return testDeps{
		runs:         db.Runs(),
		snapshots:    db.Snapshots(),
		edges:        db.Edges(),
		proxyConfigs: db.ProxyConfigs(),
		bus:          core.NewEventBus(),
	}
}

func newHandlerForTest(deps testDeps, fake fakeCollectorReader) http.Handler {
	fake.statuses = append([]collector.NodeStatus(nil), fake.statuses...)
	return NewHandler(
		deps.runs,
		deps.snapshots,
		deps.edges,
		deps.proxyConfigs,
		fake,
		triggerFunc(func() {}),
		deps.bus,
		parser.NewRegistry(),
	)
}

type fakeCollectorReader struct {
	statuses       []collector.NodeStatus
	previewContent map[core.NodeName]string
}

func (f fakeCollectorReader) GetStatus(context.Context) ([]collector.NodeStatus, error) {
	return f.statuses, nil
}

func (f fakeCollectorReader) PreviewProxyConfig(_ context.Context, nodeName core.NodeName, kind string, configPath string) (string, map[string]string, parser.ParseResult, error) {
	content := f.previewContent[nodeName]
	if strings.TrimSpace(content) == "" {
		return "", nil, parser.ParseResult{}, errors.New("missing preview content")
	}
	parsed, err := parser.NewRegistry().Parse(kind, content)
	return content, map[string]string{configPath: content}, parsed, err
}

type triggerFunc func()

func (f triggerFunc) Trigger() { f() }

type streamingRecorder struct {
	*httptest.ResponseRecorder
}

func newStreamingRecorder() *streamingRecorder {
	return &streamingRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (s *streamingRecorder) Flush() {}

func waitForBody(t *testing.T, rec *streamingRecorder, pattern string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.Body.String(), pattern) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in body %q", pattern, rec.Body.String())
}
