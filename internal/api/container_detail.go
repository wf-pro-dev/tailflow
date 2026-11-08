// Container drill-down endpoints: inspect, live stats (SSE), logs (SSE or
// one-shot JSON). Design §F7.
//
// Stats and logs use the collector's refcounted StreamManager: the upstream
// tailkitd stream opens when the first browser subscribes and closes 10s
// after the last unsubscribes.

package api

import (
        "encoding/json"
        "net/http"
        "regexp"
        "strings"
        "time"

        hubpkg "github.com/wf-pro-dev/tailflow/internal/hub"
)

// containerIDPattern enforces the OpenAPI spec's `^[a-f0-9]{12,64}$` rule.
var containerIDPattern = regexp.MustCompile(`^[a-f0-9]{12,64}$`)

// routeContainerPath dispatches /api/v1/hosts/{host}/docker/containers/{id}[/{action}].
func (h *Handler) routeContainerPath(w http.ResponseWriter, r *http.Request, host, rest string) {
        // rest is "{id}" or "{id}/stats" or "{id}/logs".
        parts := strings.SplitN(rest, "/", 2)
        containerID := parts[0]
        action := ""
        if len(parts) > 1 {
                action = parts[1]
        }

        if !containerIDPattern.MatchString(containerID) {
                h.writeProblem(w, r, http.StatusBadRequest, "invalid-parameter",
                        "Invalid container ID", "container ID must match ^[a-f0-9]{12,64}$", host)
                return
        }

        // Verify the host exists.
        hh, ok := h.store.Host(host)
        if !ok {
                h.writeProblem(w, r, http.StatusNotFound, "host-not-found",
                        "Host not found", "no host with that name", host)
                return
        }
        if !hh.Caps.Docker {
                reason := "not-configured"
                if r, ok := hh.Caps.Reasons["docker"]; ok {
                        reason = r
                }
                h.writeProblem(w, r, http.StatusServiceUnavailable, "capability-unavailable",
                        "Integration not configured", "Docker is not enabled on this host: "+reason, host)
                return
        }

        switch {
        case action == "" && r.Method == http.MethodGet:
                h.handleContainerInspect(w, r, host, containerID)
        case action == "stats" && r.Method == http.MethodGet:
                h.handleContainerStats(w, r, host, containerID)
        case action == "logs" && r.Method == http.MethodGet:
                h.handleContainerLogs(w, r, host, containerID)
        default:
                h.writeProblem(w, r, http.StatusNotFound, "not-found",
                        "Not found", "no such container endpoint", host)
        }
}

// handleContainerInspect returns the container's inspect summary from the
// store's inventory (the collector already fetched it).
func (h *Handler) handleContainerInspect(w http.ResponseWriter, r *http.Request, host, containerID string) {
        inv := h.store.Containers(host)
        for _, c := range inv.Containers {
                if c.ID == containerID || startsWith(c.ID, containerID) {
                        writeJSON(w, http.StatusOK, c)
                        return
                }
        }
        h.writeProblem(w, r, http.StatusNotFound, "not-found",
                "Container not found", "no container with that ID on this host", host)
}

// handleContainerStats streams live container stats via SSE. Subscribes to
// the collector's refcounted StreamManager.
func (h *Handler) handleContainerStats(w http.ResponseWriter, r *http.Request, host, containerID string) {
        if h.coll == nil {
                h.writeProblem(w, r, http.StatusServiceUnavailable, "internal-error",
                        "Collector not wired", "The collector is not available to proxy container stats", host)
                return
        }

        sub, err := h.coll.Streams().Subscribe(r.Context(), host, containerID, "stats")
        if err != nil {
                h.writeProblem(w, r, http.StatusBadGateway, "upstream-error",
                        "Failed to open stats stream", err.Error(), host)
                return
        }
        defer sub.Close()

        sw, err := hubpkg.NewWriter(w, r)
        if err != nil {
                h.writeProblem(w, r, http.StatusInternalServerError, "internal-error",
                        "Streaming not supported", err.Error(), host)
                return
        }

        heartbeat := time.NewTicker(15 * time.Second)
        defer heartbeat.Stop()

        for {
                select {
                case <-r.Context().Done():
                        return
                case evt, ok := <-sub.Events():
                        if !ok {
                                return
                        }
                        if evt.Err != nil {
                                _ = sw.Send("error", map[string]string{"error": evt.Err.Error()})
                                return
                        }
                        // evt.Data is *client.Event[container.StatsResponse]. The SSE
                        // writer will JSON-marshal it as {name, id, data: {...}}. The
                        // frontend's extractStats expects the raw StatsResponse fields
                        // at the top level, so we unwrap here.
                        // We use json.Marshal/Unmarshal to extract the .Data field.
                        rawBytes, _ := json.Marshal(evt.Data)
                        var wrapper struct {
                                Data json.RawMessage `json:"data"`
                        }
                        _ = json.Unmarshal(rawBytes, &wrapper)
                        if len(wrapper.Data) > 0 {
                                _ = sw.SendRaw("stats.snapshot", wrapper.Data)
                        } else {
                                _ = sw.Send("stats.snapshot", evt.Data)
                        }
                case <-heartbeat.C:
                        if err := sw.Heartbeat(); err != nil {
                                return
                        }
                }
        }
}

// handleContainerLogs returns container logs. Without
// Accept: text/event-stream, returns a JSON object {logs: "..."} with the
// last `tail` lines. With SSE, streams log.line events indefinitely.
func (h *Handler) handleContainerLogs(w http.ResponseWriter, r *http.Request, host, containerID string) {
        if h.coll == nil {
                h.writeProblem(w, r, http.StatusServiceUnavailable, "internal-error",
                        "Collector not wired", "The collector is not available to proxy container logs", host)
                return
        }

        // Check if the client wants SSE.
        wantSSE := strings.Contains(r.Header.Get("Accept"), "text/event-stream")

        if !wantSSE {
                // One-shot JSON: subscribe, drain the buffer, close.
                sub, err := h.coll.Streams().Subscribe(r.Context(), host, containerID, "logs")
                if err != nil {
                        h.writeProblem(w, r, http.StatusBadGateway, "upstream-error",
                                "Failed to open logs stream", err.Error(), host)
                        return
                }
                defer sub.Close()

                // Drain buffered lines with a short timeout.
                var lines []string
                timeout := time.After(2 * time.Second)
                for {
                        select {
                        case evt, ok := <-sub.Events():
                                if !ok {
                                        goto done
                                }
                                if evt.Err != nil {
                                        goto done
                                }
                                if line, ok := evt.Data.(string); ok {
                                        lines = append(lines, line)
                                }
                        case <-timeout:
                                goto done
                        }
                }
        done:
                writeJSON(w, http.StatusOK, map[string]any{"logs": strings.Join(lines, "\n")})
                return
        }

        // SSE mode.
        sub, err := h.coll.Streams().Subscribe(r.Context(), host, containerID, "logs")
        if err != nil {
                h.writeProblem(w, r, http.StatusBadGateway, "upstream-error",
                        "Failed to open logs stream", err.Error(), host)
                return
        }
        defer sub.Close()

        sw, err := hubpkg.NewWriter(w, r)
        if err != nil {
                h.writeProblem(w, r, http.StatusInternalServerError, "internal-error",
                        "Streaming not supported", err.Error(), host)
                return
        }

        heartbeat := time.NewTicker(15 * time.Second)
        defer heartbeat.Stop()

        for {
                select {
                case <-r.Context().Done():
                        return
                case evt, ok := <-sub.Events():
                        if !ok {
                                return
                        }
                        if evt.Err != nil {
                                _ = sw.Send("error", map[string]string{"error": evt.Err.Error()})
                                return
                        }
                        if err := sw.Send("log.line", evt.Data); err != nil {
                                return
                        }
                case <-heartbeat.C:
                        if err := sw.Heartbeat(); err != nil {
                                return
                        }
                }
        }
}

// startsWith reports whether s starts with prefix. Used for container ID
// prefix matching (Docker accepts short IDs).
func startsWith(s, prefix string) bool {
        return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// jsonEncode is a small helper to avoid importing json in every file.
func jsonEncode(v any) []byte {
        b, _ := json.Marshal(v)
        return b
}
