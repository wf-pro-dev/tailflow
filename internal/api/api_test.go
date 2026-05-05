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
	"github.com/wf-pro-dev/tailflow/internal/store"
	"github.com/wf-pro-dev/tailflow/internal/topology"
)

func TestListNodes(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	snapshot := store.NodeSnapshot{
		ID:          "snap-1",
		RunID:       "run-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 80}},
	}

	handler := newHandlerForTest(deps, fakeCollectorReader{
		statuses: []collector.NodeStatus{{
			NodeName:   "node-a",
			Online:     true,
			LastSeenAt: core.NowTimestamp(),
		}},
		snapshots: []store.NodeSnapshot{snapshot},
	})

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
	snapshot := store.NodeSnapshot{
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
	}

	handler := newHandlerForTest(deps, fakeCollectorReader{
		statuses: []collector.NodeStatus{{
			NodeName:   "node-a",
			Online:     true,
			LastSeenAt: core.NowTimestamp(),
		}},
		snapshots: []store.NodeSnapshot{snapshot},
	})

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

func TestSetProxyConfig(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	refresher := &fakeRefreshTrigger{}
	handler := newHandlerForTestWithRefresh(deps, fakeCollectorReader{
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
	}, refresher)

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
	if refresher.called != 1 {
		t.Fatalf("RefreshNow() calls = %d, want 1", refresher.called)
	}
}

func TestDeleteProxyConfigTriggersRefresh(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	config := parser.ProxyConfigInput{
		ID:         "cfg-delete",
		NodeName:   "node-a",
		Kind:       "nginx",
		ConfigPath: "/etc/nginx/nginx.conf",
		Content:    "server {}",
		UpdatedAt:  core.NowTimestamp(),
	}
	if err := deps.proxyConfigs.Save(context.Background(), config); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	refresher := &fakeRefreshTrigger{}
	handler := newHandlerForTestWithRefresh(deps, fakeCollectorReader{}, refresher)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/configs/cfg-delete", nil)
	req.SetPathValue("id", "cfg-delete")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if refresher.called != 1 {
		t.Fatalf("RefreshNow() calls = %d, want 1", refresher.called)
	}
	if _, err := deps.proxyConfigs.Get(context.Background(), "cfg-delete"); err == nil {
		t.Fatal("config still present after delete")
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
	snapA := store.NodeSnapshot{
		ID:          "snap-a",
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
		NodeName:    "node-b",
		TailscaleIP: "100.64.0.2",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 8080, Process: "app"}},
	}
	handler := newHandlerForTest(deps, fakeCollectorReader{
		statuses: []collector.NodeStatus{
			{NodeName: "node-a", Online: true},
			{NodeName: "node-b", Online: true},
		},
		snapshots: []store.NodeSnapshot{snapA, snapB},
	})
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
	if topology.Version == 0 {
		t.Fatalf("topology.Version = %d, want > 0", topology.Version)
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
	handler := newHandlerForTest(deps, fakeCollectorReader{
		statuses: []collector.NodeStatus{{NodeName: "node-a", Degraded: true}},
		localIP:  "100.64.0.10",
	})

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
	if health.TailnetIP != "100.64.0.10" {
		t.Fatalf("health.TailnetIP = %q, want %q", health.TailnetIP, "100.64.0.10")
	}
}

func TestHealthDegradesForWorkloadIssues(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	snapshot := store.NodeSnapshot{
		ID:          "snap-1",
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
	}
	handler := newHandlerForTest(deps, fakeCollectorReader{
		statuses:  []collector.NodeStatus{{NodeName: "node-a"}},
		snapshots: []store.NodeSnapshot{snapshot},
	})

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
	snapshot := store.NodeSnapshot{
		ID:          "snap-1",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 80}},
	}

	handler := newHandlerForTest(deps, fakeCollectorReader{
		statuses: []collector.NodeStatus{{
			NodeName:   "node-a",
			Online:     true,
			LastSeenAt: core.NowTimestamp(),
		}},
		snapshots: []store.NodeSnapshot{snapshot},
	})

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

func TestWatchNodesStreamsGranularNodeEvents(t *testing.T) {
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
	core.BroadcastEvent(deps.bus, core.EventNodePortsReplaced.String(), collector.NodePortsReplacedEvent{
		NodeName: "node-a",
		Ports:    []store.ListenPort{{Port: 80}},
	})
	waitForBody(t, rec, "event: "+core.EventNodePortsReplaced.String())
	if strings.Contains(rec.Body.String(), "event: "+core.EventSnapshotUpdated.String()) {
		t.Fatalf("nodes stream unexpectedly emitted snapshot.updated: %s", rec.Body.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected nodes stream to stop after cancel")
	}
}

func TestWatchNodesDoesNotStreamSnapshotUpdates(t *testing.T) {
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
	core.BroadcastEvent(deps.bus, core.EventSnapshotUpdated.String(), collector.SnapshotEvent{
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
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(rec.Body.String(), "event: "+core.EventSnapshotUpdated.String()) {
		t.Fatalf("nodes stream unexpectedly emitted snapshot.updated: %s", rec.Body.String())
	}

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
	snapA := store.NodeSnapshot{
		ID:          "snap-a",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NowTimestamp(),
		Ports:       []store.ListenPort{{Port: 80, Process: "nginx"}},
	}
	handler := newHandlerForTest(deps, fakeCollectorReader{
		statuses:  []collector.NodeStatus{{NodeName: "node-a", Online: true}},
		snapshots: []store.NodeSnapshot{snapA},
	})
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
	if !strings.Contains(rec.Body.String(), `"version":`) ||
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

func TestWatchTopologyStreamsNamedPatchEvents(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	handler := newHandlerForTest(deps, fakeCollectorReader{})

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
	core.BroadcastEvent(deps.bus, core.EventTopologyPatch.String(), topology.Patch{
		Version:      2,
		ChangedNodes: []core.NodeName{"node-a"},
		Summary:      store.TopologySummary{NodeCount: 1},
	})
	waitForBody(t, rec, "event: topology.patch")
	if strings.Contains(rec.Body.String(), `event: message`) && strings.Contains(rec.Body.String(), `"event":"topology.patch"`) {
		t.Fatalf("topology patch unexpectedly emitted as generic message: %s", rec.Body.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected topology stream to stop after cancel")
	}
}

func TestWatchTopologyStreamsNamedResetEvents(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	handler := newHandlerForTest(deps, fakeCollectorReader{})

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
	core.BroadcastEvent(deps.bus, core.EventTopologyReset.String(), topology.Reset{
		Reason:   "test",
		Snapshot: topology.Snapshot{Version: 2},
	})
	waitForBody(t, rec, "event: topology.reset")
	if strings.Contains(rec.Body.String(), `event: message`) && strings.Contains(rec.Body.String(), `"event":"topology.reset"`) {
		t.Fatalf("topology reset unexpectedly emitted as generic message: %s", rec.Body.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected topology stream to stop after cancel")
	}
}

func TestWatchTopologyDoesNotStreamSnapshotUpdates(t *testing.T) {
	t.Parallel()

	deps := newTestDeps(t)
	handler := newHandlerForTest(deps, fakeCollectorReader{})

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
	core.BroadcastEvent(deps.bus, core.EventSnapshotUpdated.String(), collector.SnapshotEvent{
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
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(rec.Body.String(), "event: "+core.EventSnapshotUpdated.String()) {
		t.Fatalf("topology stream unexpectedly emitted snapshot.updated: %s", rec.Body.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected topology stream to stop after cancel")
	}
}

type testDeps struct {
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
		proxyConfigs: db.ProxyConfigs(),
		bus:          core.NewEventBus(),
	}
}

func newHandlerForTest(deps testDeps, fake fakeCollectorReader) http.Handler {
	return newHandlerForTestWithRefresh(deps, fake, nil)
}

func newHandlerForTestWithRefresh(deps testDeps, fake fakeCollectorReader, refresher refreshTrigger) http.Handler {
	fake.statuses = append([]collector.NodeStatus(nil), fake.statuses...)
	topologyManager := topology.NewManager()
	topologyManager.Reset(fake.snapshots, fake.statuses, "test")
	return NewHandler(
		deps.proxyConfigs,
		fake,
		topologyManager,
		refresher,
		deps.bus,
		parser.NewRegistry(),
	)
}

type fakeCollectorReader struct {
	statuses       []collector.NodeStatus
	snapshots      []store.NodeSnapshot
	previewContent map[core.NodeName]string
	localIP        string
}

type fakeRefreshTrigger struct {
	called int
	err    error
}

func (f *fakeRefreshTrigger) RefreshNow(context.Context) error {
	f.called++
	return f.err
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

func (f fakeCollectorReader) LatestSnapshot(nodeName core.NodeName) (store.NodeSnapshot, bool) {
	for _, snapshot := range f.snapshots {
		if snapshot.NodeName == nodeName {
			return snapshot, true
		}
	}
	return store.NodeSnapshot{}, false
}

func (f fakeCollectorReader) Snapshots() []store.NodeSnapshot {
	return append([]store.NodeSnapshot(nil), f.snapshots...)
}

func (f fakeCollectorReader) LocalTailscaleIP(context.Context) (string, error) {
	return f.localIP, nil
}

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
