package api

import (
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "strings"
        "testing"
        "time"

        "github.com/wf-pro-dev/tailflow/internal/config"
        hubpkg "github.com/wf-pro-dev/tailflow/internal/hub"
        "github.com/wf-pro-dev/tailflow/internal/state"
)

// testHandler returns a Handler with a real state.Store, a real hub, and
// default config. Tests that need to populate the store can call
// h.store.UpsertHost etc.
func testHandler(t *testing.T, store *state.Store) *Handler {
        t.Helper()
        if store == nil {
                store = state.New(720)
        }
        cfg := config.Default()
        hub := hubpkg.NewHub()
        return NewHandler("/", "1.0.0", "v0.5.4", cfg, store, hub, BufferConfig{
                HistoryWindow:  "1h",
                HistoryStep:    "5s",
                StreamInterval: "2s",
                StaleFactor:    3,
        }, nil)
}

func TestHealthz(t *testing.T) {
        h := testHandler(t, nil)
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/v1/healthz", nil)
        h.ServeHTTP(rec, req)

        if rec.Code != http.StatusOK {
                t.Fatalf("status = %d, want 200", rec.Code)
        }
        var body map[string]string
        if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
                t.Fatalf("Decode: %v", err)
        }
        if body["status"] != "ok" {
                t.Errorf("status = %q, want ok", body["status"])
        }
        if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
                t.Errorf("Content-Type = %q, want application/json", ct)
        }
}

func TestMeta(t *testing.T) {
        h := testHandler(t, nil)
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
        h.ServeHTTP(rec, req)

        if rec.Code != http.StatusOK {
                t.Fatalf("status = %d, want 200", rec.Code)
        }
        var body map[string]any
        if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
                t.Fatalf("Decode: %v", err)
        }
        if body["version"] != "1.0.0" {
                t.Errorf("version = %v, want 1.0.0", body["version"])
        }
        if body["tailkit_version"] != "v0.5.4" {
                t.Errorf("tailkit_version = %v, want v0.5.4", body["tailkit_version"])
        }
}

func TestListHostsEmpty(t *testing.T) {
        h := testHandler(t, nil)
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/v1/hosts", nil)
        h.ServeHTTP(rec, req)
        if rec.Code != http.StatusOK {
                t.Fatalf("status = %d, want 200", rec.Code)
        }
        var hosts []any
        if err := json.NewDecoder(rec.Body).Decode(&hosts); err != nil {
                t.Fatalf("Decode: %v", err)
        }
        if len(hosts) != 0 {
                t.Errorf("len(hosts) = %d, want 0 for empty store", len(hosts))
        }
}

func TestListHostsPopulated(t *testing.T) {
        store := state.New(720)
        store.UpsertHost(state.Host{Name: "vps-2", Online: true, Status: state.StatusHealthy, StreamInterval: 2 * time.Second})
        store.UpsertHost(state.Host{Name: "vps-1", Online: true, Status: state.StatusHealthy, StreamInterval: 2 * time.Second})
        store.UpsertHost(state.Host{Name: "vps-3", Online: false, Status: state.StatusOffline, StreamInterval: 2 * time.Second})

        h := testHandler(t, store)
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/v1/hosts", nil)
        h.ServeHTTP(rec, req)

        if rec.Code != http.StatusOK {
                t.Fatalf("status = %d, want 200", rec.Code)
        }
        var hosts []map[string]any
        if err := json.NewDecoder(rec.Body).Decode(&hosts); err != nil {
                t.Fatalf("Decode: %v", err)
        }
        if len(hosts) != 3 {
                t.Fatalf("len(hosts) = %d, want 3", len(hosts))
        }
        // Default sort is name asc.
        if hosts[0]["name"] != "vps-1" {
                t.Errorf("hosts[0].name = %v, want vps-1", hosts[0]["name"])
        }
        if hosts[1]["name"] != "vps-2" {
                t.Errorf("hosts[1].name = %v, want vps-2", hosts[1]["name"])
        }
}

func TestListHostsFilterByStatus(t *testing.T) {
        store := state.New(720)
        store.UpsertHost(state.Host{Name: "vps-1", Online: true, Status: state.StatusHealthy, StreamInterval: 2 * time.Second})
        store.UpsertHost(state.Host{Name: "vps-2", Online: false, Status: state.StatusOffline, StreamInterval: 2 * time.Second})

        h := testHandler(t, store)
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/v1/hosts?status=offline", nil)
        h.ServeHTTP(rec, req)

        var hosts []map[string]any
        _ = json.NewDecoder(rec.Body).Decode(&hosts)
        if len(hosts) != 1 || hosts[0]["name"] != "vps-2" {
                t.Errorf("filter status=offline: got %v, want [vps-2]", hosts)
        }
}

func TestGetHostNotFound(t *testing.T) {
        h := testHandler(t, nil)
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/v1/hosts/nonexistent", nil)
        h.ServeHTTP(rec, req)

        if rec.Code != http.StatusNotFound {
                t.Fatalf("status = %d, want 404", rec.Code)
        }
        ct := rec.Header().Get("Content-Type")
        if ct != "application/problem+json" {
                t.Errorf("Content-Type = %q, want application/problem+json", ct)
        }
        var body map[string]any
        _ = json.NewDecoder(rec.Body).Decode(&body)
        if body["type"] != "host-not-found" {
                t.Errorf("type = %v, want host-not-found", body["type"])
        }
        if body["host"] != "nonexistent" {
                t.Errorf("host = %v, want nonexistent", body["host"])
        }
}

func TestGetHostFound(t *testing.T) {
        store := state.New(720)
        store.UpsertHost(state.Host{
                Name:           "vps-1",
                Role:           "web",
                Online:         true,
                Status:         state.StatusHealthy,
                StreamInterval: 2 * time.Second,
                Tags:           []string{"edge"},
        })

        h := testHandler(t, store)
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/v1/hosts/vps-1", nil)
        h.ServeHTTP(rec, req)

        if rec.Code != http.StatusOK {
                t.Fatalf("status = %d, want 200", rec.Code)
        }
        var body map[string]any
        _ = json.NewDecoder(rec.Body).Decode(&body)
        if body["name"] != "vps-1" {
                t.Errorf("name = %v, want vps-1", body["name"])
        }
        if body["role"] != "web" {
                t.Errorf("role = %v, want web", body["role"])
        }
        if body["stream_interval_s"] != float64(2) {
                t.Errorf("stream_interval_s = %v, want 2", body["stream_interval_s"])
        }
}

func TestGetHostUnreachableOffline(t *testing.T) {
        store := state.New(720)
        store.UpsertHost(state.Host{Name: "vps-1", Online: false, Status: state.StatusOffline, StreamInterval: 2 * time.Second})

        h := testHandler(t, store)
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/v1/hosts/vps-1/metrics", nil)
        h.ServeHTTP(rec, req)

        if rec.Code != http.StatusServiceUnavailable {
                t.Fatalf("status = %d, want 503 for offline host", rec.Code)
        }
        var body map[string]any
        _ = json.NewDecoder(rec.Body).Decode(&body)
        if body["type"] != "host-unreachable" {
                t.Errorf("type = %v, want host-unreachable", body["type"])
        }
}

func TestFleetSummary(t *testing.T) {
        store := state.New(720)
        store.UpsertHost(state.Host{Name: "vps-1", Online: true, Status: state.StatusHealthy, StreamInterval: 2 * time.Second})
        store.UpsertHost(state.Host{Name: "vps-2", Online: true, Status: state.StatusHealthy, StreamInterval: 2 * time.Second})
        store.UpsertHost(state.Host{Name: "vps-3", Online: false, Status: state.StatusOffline, StreamInterval: 2 * time.Second})

        h := testHandler(t, store)
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/v1/fleet/summary", nil)
        h.ServeHTTP(rec, req)

        if rec.Code != http.StatusOK {
                t.Fatalf("status = %d, want 200", rec.Code)
        }
        var body map[string]any
        _ = json.NewDecoder(rec.Body).Decode(&body)
        counts := body["counts"].(map[string]any)
        if counts["healthy"] != float64(2) {
                t.Errorf("counts.healthy = %v, want 2", counts["healthy"])
        }
        if counts["offline"] != float64(1) {
                t.Errorf("counts.offline = %v, want 1", counts["offline"])
        }
}

func TestHostMetrics(t *testing.T) {
        store := state.New(720)
        store.UpsertHost(state.Host{Name: "vps-1", Online: true, Status: state.StatusHealthy, StreamInterval: 2 * time.Second})
        now := time.Now()
        store.PushSample("vps-1", state.Sample{T: now.Add(-10 * time.Second), CPUTotal: 10, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000})
        store.PushSample("vps-1", state.Sample{T: now.Add(-5 * time.Second), CPUTotal: 20, MemUsedPct: 60, MemUsedBytes: 1200, MemTotalBytes: 2000})
        store.PushSample("vps-1", state.Sample{T: now, CPUTotal: 30, MemUsedPct: 70, MemUsedBytes: 1400, MemTotalBytes: 2000})

        h := testHandler(t, store)
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/v1/hosts/vps-1/metrics?window=1h", nil)
        h.ServeHTTP(rec, req)

        if rec.Code != http.StatusOK {
                t.Fatalf("status = %d, want 200", rec.Code)
        }
        var body map[string]any
        _ = json.NewDecoder(rec.Body).Decode(&body)
        if body["host"] != "vps-1" {
                t.Errorf("host = %v, want vps-1", body["host"])
        }
        series := body["series"].(map[string]any)
        cpuTotal := series["cpu_total"].([]any)
        if len(cpuTotal) != 3 {
                t.Errorf("len(cpu_total) = %d, want 3", len(cpuTotal))
        }
}

func TestBasePath(t *testing.T) {
        store := state.New(720)
        cfg := config.Default()
        h := NewHandler("/ui", "dev", "v0.5.4", cfg, store, nil, BufferConfig{}, nil)
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/ui/api/v1/healthz", nil)
        h.ServeHTTP(rec, req)
        if rec.Code != http.StatusOK {
                t.Fatalf("status = %d, want 200 (base path /ui should be stripped)", rec.Code)
        }
}

func TestSPAFallback(t *testing.T) {
        h := testHandler(t, nil)
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/some/random/path", nil)
        h.ServeHTTP(rec, req)
        if rec.Code != http.StatusOK {
                t.Fatalf("status = %d, want 200 (SPA fallback)", rec.Code)
        }
        if !strings.Contains(rec.Body.String(), "Tailflow v1") {
                t.Errorf("body does not contain placeholder title; got:\n%s", rec.Body.String())
        }
}

func TestStreamRequiresTopics(t *testing.T) {
        // /api/v1/stream is wired in M2; without the topics param it returns 400.
        h := testHandler(t, nil)
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/v1/stream", nil)
        h.ServeHTTP(rec, req)

        if rec.Code != http.StatusBadRequest {
                t.Fatalf("status = %d, want 400 (missing topics)", rec.Code)
        }
        ct := rec.Header().Get("Content-Type")
        if ct != "application/problem+json" {
                t.Errorf("Content-Type = %q, want application/problem+json", ct)
        }
}
