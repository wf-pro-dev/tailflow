package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthz(t *testing.T) {
	h := NewHandler("/", "dev", "v0.5.4", BufferConfig{HistoryWindow: "1h", HistoryStep: "5s", StreamInterval: "2s", StaleFactor: 3}, nil)
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
	h := NewHandler("/", "1.0.0", "v0.5.4", BufferConfig{
		HistoryWindow:  "1h",
		HistoryStep:    "5s",
		StreamInterval: "2s",
		StaleFactor:    3,
	}, nil)
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

func TestUnimplementedReturnsProblemPlusJSON(t *testing.T) {
	h := NewHandler("/", "dev", "v0.5.4", BufferConfig{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/hosts", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if body["type"] != "not-implemented" {
		t.Errorf("type = %v, want not-implemented", body["type"])
	}
}

func TestBasePath(t *testing.T) {
	h := NewHandler("/ui", "dev", "v0.5.4", BufferConfig{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ui/api/v1/healthz", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (base path /ui should be stripped)", rec.Code)
	}
}

func TestSPAFallback(t *testing.T) {
	h := NewHandler("/", "dev", "v0.5.4", BufferConfig{}, nil)
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
