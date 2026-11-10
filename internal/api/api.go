// Package api owns Tailflow's HTTP handlers, middleware, and generated
// OpenAPI types. M1 wires /api/v1/hosts/* and /api/v1/fleet/summary; the
// remaining endpoints (processes, ports, docker, stream) land in M2-M4.
//
// Import rules (enforced by .golangci.yml depguard):
//   - This package MUST NOT import tailkit or tailkit/client. It uses only
//     Tailflow's own types (design §F10).
//   - Generated OpenAPI types live in gen.go (excluded from linting).
package api

import (
        "encoding/json"
        "net/http"
        "strings"
        "time"

        "github.com/wf-pro-dev/tailflow/internal/collector"
        "github.com/wf-pro-dev/tailflow/internal/config"
        hubpkg "github.com/wf-pro-dev/tailflow/internal/hub"
        "github.com/wf-pro-dev/tailflow/internal/state"
)

// Handler is the Tailflow HTTP handler. It routes /api/v1/* to the
// appropriate handler and serves the SPA for any other path.
//
// The handler is mounted under a configurable base path (default /), so the
// same handler can later be mounted at /ui inside tailkitd (design §F10).
type Handler struct {
        basePath string
        version  string
        startedAt time.Time
        tailkitVersion string
        cfg      config.Config
        store    *state.Store
        hub      *hubpkg.Hub
        coll     *collector.Collector
        bufferCfg BufferConfig
        // spaFS is the embed.FS for the SPA. nil until M2; when set, unknown
        // non-API paths fall back to index.html.
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
func NewHandler(basepath, version, tailkitVersion string, cfg config.Config, store *state.Store, hub *hubpkg.Hub, coll *collector.Collector, bufferCfg BufferConfig, spaFS http.FileSystem) *Handler {
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
                cfg:            cfg,
                store:          store,
                hub:            hub,
                coll:           coll,
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
        case path == "/api/v1/fleet/summary":
                h.handleFleetSummary(w, r)
                return
        case path == "/api/v1/hosts":
                h.handleListHosts(w, r)
                return
        case path == "/api/v1/stream":
                h.handleStream(w, r)
                return
        case isHostPath(path):
                h.routeHostPath(w, r, path)
                return
        case strings.HasPrefix(path, "/api/v1/"):
                // Endpoint exists in the OpenAPI spec but not yet wired (M2-M4).
                h.writeProblem(w, r, http.StatusNotImplemented, "not-implemented",
                        "Not implemented in this milestone", path+" is not wired yet", "")
                return
        default:
                // SPA fallback.
                h.spaFallback(w, r, path)
                return
        }
}

// isHostPath returns true for any /api/v1/hosts/{host}[/...] path.
func isHostPath(path string) bool {
        return strings.HasPrefix(path, "/api/v1/hosts/") && len(path) > len("/api/v1/hosts/")
}

// routeHostPath dispatches the per-host endpoints.
func (h *Handler) routeHostPath(w http.ResponseWriter, r *http.Request, path string) {
        rest := strings.TrimPrefix(path, "/api/v1/hosts/")
        parts := strings.SplitN(rest, "/", 2)
        host := parts[0]
        sub := ""
        if len(parts) > 1 {
                sub = parts[1]
        }
        r.SetPathValue("host", host)

        switch {
        case sub == "" && r.Method == http.MethodGet:
                h.handleGetHost(w, r)
        case sub == "metrics" && r.Method == http.MethodGet:
                h.handleHostMetrics(w, r)
        case sub == "metrics/latest" && r.Method == http.MethodGet:
                h.handleHostMetricsLatest(w, r)
        case sub == "processes" && r.Method == http.MethodGet:
                h.handleHostProcesses(w, r)
        case sub == "ports" && r.Method == http.MethodGet:
                h.handleHostPorts(w, r)
        case sub == "docker/containers" && r.Method == http.MethodGet:
                h.handleHostContainers(w, r)
        case sub == "docker/containers/stats" && r.Method == http.MethodGet:
                h.handleHostAllContainerStats(w, r)
        case strings.HasPrefix(sub, "docker/containers/") && r.Method == http.MethodGet:
                h.routeContainerPath(w, r, host, strings.TrimPrefix(sub, "docker/containers/"))
        case sub == "docker/swarm/services" && r.Method == http.MethodGet:
                h.handleHostSwarmServices(w, r)
        case sub == "docker/compose/projects" && r.Method == http.MethodGet:
                h.handleHostComposeProjects(w, r)
        case sub == "docker/images" && r.Method == http.MethodGet:
                h.handleHostDockerImages(w, r)
        case sub == "systemd/units" && r.Method == http.MethodGet:
                h.handleHostSystemdUnits(w, r)
        default:
                h.writeProblem(w, r, http.StatusNotFound, "not-found",
                        "Not found", "no such endpoint: "+path, host)
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

// spaFallback serves the SPA. For any non-API path, it tries the exact file
// first, then falls back to index.html (for client-side routing). When no SPA
// is embedded, serves a placeholder.
func (h *Handler) spaFallback(w http.ResponseWriter, r *http.Request, path string) {
        if h.spaFS != nil {
                // Try the exact path first.
                if f, err := h.spaFS.Open(path); err == nil {
                        stat, _ := f.Stat()
                        if !stat.IsDir() {
                                defer f.Close()
                                http.ServeContent(w, r, path, stat.ModTime(), readSeeker{f})
                                return
                        }
                        f.Close()
                }
                // Fall back to index.html for client-side routing.
                if f, err := h.spaFS.Open("/index.html"); err == nil {
                        defer f.Close()
                        stat, _ := f.Stat()
                        http.ServeContent(w, r, "index.html", stat.ModTime(), readSeeker{f})
                        return
                }
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.Header().Set("Cache-Control", "no-store")
        _, _ = w.Write([]byte(m0PlaceholderHTML))
}

// readSeeker adapts an http.File (which implements ReadSeeker via io.ReadSeekCloser)
// for use with http.ServeContent. The type assertion is safe because
// http.File embeds io.ReadSeekCloser.
type readSeeker struct{ f http.File }

func (rs readSeeker) Read(p []byte) (int, error)         { return rs.f.Read(p) }
func (rs readSeeker) Seek(off int64, whence int) (int64, error) { return rs.f.Seek(off, whence) }

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


