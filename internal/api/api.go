// Package api owns Tailflow's HTTP handlers, middleware, and generated
// OpenAPI types. In M0 only the healthz, meta, and SPA-fallback routes are
// wired; the full /api/v1/* contract lands in M1.
//
// Import rules (enforced by .golangci.yml depguard):
//   - This package MUST NOT import tailkit or tailkit/client. It uses only
//     Tailflow's own types (design §F10).
//   - Generated OpenAPI types live in gen.go (excluded from linting).
package api

import (
        "encoding/json"
        "io"
        "net/http"
        "strings"
        "time"
)

// Handler is the Tailflow HTTP handler. In M0 it routes only /api/v1/healthz
// and /api/v1/meta plus an SPA fallback; future milestones add the rest.
//
// The handler is mounted under a configurable base path (default /), so the
// same handler can later be mounted at /ui inside tailkitd (design §F10).
type Handler struct {
        basePath string
        version  string
        startedAt time.Time
        tailkitVersion string
        bufferCfg BufferConfig
        // spaFS is the embed.FS for the SPA. nil in M0 (no SPA yet); when set,
        // unknown non-API paths fall back to index.html.
        spaFS http.FileSystem
}

// BufferConfig is the buffer configuration surfaced via /api/v1/meta.
// Mirrors the OpenAPI BufferConfig schema.
type BufferConfig struct {
        HistoryWindow string `json:"history_window"`
        HistoryStep   string `json:"history_step"`
        StreamInterval string `json:"stream_interval"`
        StaleFactor   int    `json:"stale_factor"`
}

// NewHandler returns a Handler. basepath must start with "/" and end without
// one (e.g. "/", "/ui"); trailing slashes are stripped.
func NewHandler(basepath, version, tailkitVersion string, bufferCfg BufferConfig, spaFS http.FileSystem) *Handler {
        if !strings.HasPrefix(basepath, "/") {
                basepath = "/" + basepath
        }
        basepath = strings.TrimRight(basepath, "/")
        if basepath == "" {
                basepath = "/"
        }
        return &Handler{
                basePath:       basepath,
                version:        version,
                startedAt:      time.Now(),
                tailkitVersion: tailkitVersion,
                bufferCfg:      bufferCfg,
                spaFS:          spaFS,
        }
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
        path := r.URL.Path
        // Strip the base path so route matching works under /ui as well as /.
        if h.basePath != "/" {
                path = strings.TrimPrefix(path, h.basePath)
                if !strings.HasPrefix(path, "/") {
                        path = "/" + path
                }
        }

        switch {
        case path == "/api/v1/healthz":
                h.handleHealthz(w, r)
                return
        case path == "/api/v1/meta":
                h.handleMeta(w, r)
                return
        case strings.HasPrefix(path, "/api/v1/"):
                // Not yet implemented in M0 — return 501 so the contract is honest.
                h.writeProblem(w, r, http.StatusNotImplemented, "not-implemented",
                        "Not implemented in M0", path+" is not wired yet", "")
                return
        default:
                // SPA fallback. In M0 there is no SPA yet; serve a tiny placeholder.
                h.spaFallback(w, r, path)
                return
        }
}

// handleHealthz returns 200 immediately. Liveness does not wait for discovery
// (design §15 startup contract).
func (h *Handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Cache-Control", "no-store")
        _ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleMeta returns build version, tailkit version, uptime, and buffer config.
func (h *Handler) handleMeta(w http.ResponseWriter, r *http.Request) {
        resp := map[string]any{
                "version":         h.version,
                "tailkit_version": h.tailkitVersion,
                "uptime_seconds":  int64(time.Since(h.startedAt).Seconds()),
                "buffer_config":   h.bufferCfg,
        }
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Cache-Control", "no-store")
        _ = json.NewEncoder(w).Encode(resp)
}

// spaFallback serves the SPA index.html for any non-API path. In M0 there is
// no SPA yet, so we serve a tiny placeholder so `make dev` shows something.
func (h *Handler) spaFallback(w http.ResponseWriter, r *http.Request, path string) {
        if h.spaFS != nil {
                // Try the exact path first, then fall back to index.html.
                if f, err := h.spaFS.Open(path); err == nil {
                        defer f.Close()
                        stat, _ := f.Stat()
                        http.ServeContent(w, r, path, stat.ModTime(), f.(io.ReadSeeker))
                        return
                }
                if f, err := h.spaFS.Open("/index.html"); err == nil {
                        defer f.Close()
                        http.ServeContent(w, r, "index.html", time.Now(), f.(io.ReadSeeker))
                        return
                }
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.Header().Set("Cache-Control", "no-store")
        _, _ = w.Write([]byte(m0PlaceholderHTML))
}

// writeProblem emits an RFC 9457 problem+json response. host is optional.
// In M0 we don't yet have a request ID middleware; that lands with the full
// API in M1.
func (h *Handler) writeProblem(w http.ResponseWriter, r *http.Request, status int, typ, title, detail, host string) {
        if host != "" {
                // host is included only when set
        }
        w.Header().Set("Content-Type", "application/problem+json")
        w.Header().Set("Cache-Control", "no-store")
        w.WriteHeader(status)
        _ = json.NewEncoder(w).Encode(map[string]any{
                "type":    typ,
                "title":   title,
                "status":  status,
                "detail":  detail,
                "host":    host,
        })
}

const m0PlaceholderHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Tailflow v1 — M0</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif; max-width: 60ch; margin: 4em auto; padding: 0 1em; color: #1a1a1a; }
  code { background: #f4f4f4; padding: 0.1em 0.3em; border-radius: 3px; font-size: 0.9em; }
  h1 { margin-top: 0; }
</style>
</head>
<body>
<h1>Tailflow v1 — M0</h1>
<p>The Tailflow backend is running. The SPA is not built yet; this placeholder
appears for any non-API path.</p>
<p>Try:</p>
<ul>
<li><code>GET /api/v1/healthz</code> — liveness probe</li>
<li><code>GET /api/v1/meta</code> — build and buffer configuration</li>
</ul>
<p>Endpoints under <code>/api/v1/hosts/*</code>, <code>/api/v1/stream</code>
and the rest of the v1 contract land in milestone M1.</p>
</body>
</html>
`


