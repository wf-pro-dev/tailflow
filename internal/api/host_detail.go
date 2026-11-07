// Remaining per-host endpoints: /processes, /ports, /docker/containers,
// /docker/swarm/services, /docker/compose/projects. The store already holds
// all this data (populated by the collector in M1); these handlers just
// read it out and shape it for the API.
//
// Container drill-down (stats + logs SSE) lands in M4 per the design.

package api

import (
        "errors"
        "net/http"
        "sort"
)

var errInvalid = errors.New("invalid integer")

// --- /api/v1/hosts/{host}/processes ----------------------------------------

func (h *Handler) handleHostProcesses(w http.ResponseWriter, r *http.Request) {
        host := r.PathValue("host")
        if host == "" {
                host = pathLastSegment(r.URL.Path)
        }
        hh, ok := h.store.Host(host)
        if !ok {
                h.writeProblem(w, r, http.StatusNotFound, "host-not-found",
                        "Host not found", "no host with that name", host)
                return
        }
        if status := hostStatusToHTTP(hh.Status); status != http.StatusOK {
                h.writeProblem(w, r, status, "host-unreachable",
                        "Host is unreachable", "tailkitd peer is offline or absent", host)
                return
        }
        _, latest, ok := h.store.Latest(host)
        if !ok {
                h.writeProblem(w, r, http.StatusNotFound, "host-not-found",
                        "Host not found", "no sample available", host)
                return
        }

        procs := latest.Processes
        sortField := getOne(r.URL.Query(), "sort")
        if sortField == "mem" {
                sort.SliceStable(procs, func(i, j int) bool {
                        return procs[i].MemoryRSS > procs[j].MemoryRSS
                })
        } else {
                // Default: CPU desc (tailkitd already sorts by CPU, but re-sort to
                // be safe in case the host sent them out of order).
                sort.SliceStable(procs, func(i, j int) bool {
                        return procs[i].CPUPercent > procs[j].CPUPercent
                })
        }

        // Apply limit (default = all; tailkitd already capped at 20-100).
        limit := len(procs)
        if l := getOne(r.URL.Query(), "limit"); l != "" {
                if n, err := atoiPositive(l); err == nil && n < limit {
                        limit = n
                }
        }
        if limit > len(procs) {
                limit = len(procs)
        }

        resp := map[string]any{
                "host":          host,
                "sort":          sortField,
                "truncated_to":  len(procs), // tailkitd's limit (design D5)
                "processes":     procs[:limit],
        }
        writeJSON(w, http.StatusOK, resp)
}

// --- /api/v1/hosts/{host}/ports --------------------------------------------

func (h *Handler) handleHostPorts(w http.ResponseWriter, r *http.Request) {
        host := r.PathValue("host")
        if host == "" {
                host = pathLastSegment(r.URL.Path)
        }
        hh, ok := h.store.Host(host)
        if !ok {
                h.writeProblem(w, r, http.StatusNotFound, "host-not-found",
                        "Host not found", "no host with that name", host)
                return
        }
        if status := hostStatusToHTTP(hh.Status); status != http.StatusOK {
                h.writeProblem(w, r, status, "host-unreachable",
                        "Host is unreachable", "tailkitd peer is offline or absent", host)
                return
        }
        ports := h.store.Ports(host)
        resp := map[string]any{
                "host":  host,
                "ports": ports,
        }
        writeJSON(w, http.StatusOK, resp)
}

// --- /api/v1/hosts/{host}/docker/containers --------------------------------

func (h *Handler) handleHostContainers(w http.ResponseWriter, r *http.Request) {
        host := r.PathValue("host")
        if host == "" {
                host = pathLastSegment(r.URL.Path)
        }
        hh, ok := h.store.Host(host)
        if !ok {
                h.writeProblem(w, r, http.StatusNotFound, "host-not-found",
                        "Host not found", "no host with that name", host)
                return
        }
        if !hh.Caps.Docker {
                // Map to 503 capability-unavailable per gap #2 (not 409).
                reason := "not-configured"
                if r, ok := hh.Caps.Reasons["docker"]; ok {
                        reason = r
                }
                h.writeProblem(w, r, http.StatusServiceUnavailable, "capability-unavailable",
                        "Integration not configured", "Docker is not enabled on this host: "+reason, host)
                return
        }
        inv := h.store.Containers(host)
        containers := inv.Containers

        // Filter by state.
        if state := getOne(r.URL.Query(), "state"); state != "" {
                filtered := containers[:0]
                for _, c := range containers {
                        if c.State == state {
                                filtered = append(filtered, c)
                        }
                }
                containers = filtered
        }
        // Filter by free-text q (case-insensitive over name + image).
        if q := getOne(r.URL.Query(), "q"); q != "" {
                qLower := toLower(q)
                filtered := containers[:0]
                for _, c := range containers {
                        hay := toLower(c.Name + " " + c.Image)
                        if contains(hay, qLower) {
                                filtered = append(filtered, c)
                        }
                }
                containers = filtered
        }

        resp := map[string]any{
                "host":       host,
                "containers": containers,
        }
        writeJSON(w, http.StatusOK, resp)
}

// --- /api/v1/hosts/{host}/docker/swarm/services ----------------------------

func (h *Handler) handleHostSwarmServices(w http.ResponseWriter, r *http.Request) {
        host := r.PathValue("host")
        if host == "" {
                host = pathLastSegment(r.URL.Path)
        }
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
        if !hh.Caps.Swarm {
                reason := "not-configured"
                if r, ok := hh.Caps.Reasons["swarm"]; ok {
                        reason = r
                }
                // Per gap #2: 403 if disabled in config, 503 if not configured.
                status := http.StatusServiceUnavailable
                typ := "capability-unavailable"
                if reason == "permission-denied" {
                        status = http.StatusForbidden
                        typ = "permission-denied"
                }
                h.writeProblem(w, r, status, typ,
                        "Swarm not available", "Swarm is not enabled on this host: "+reason, host)
                return
        }
        inv := h.store.Containers(host)
        resp := map[string]any{
                "host":     host,
                "services": inv.SwarmServices,
        }
        writeJSON(w, http.StatusOK, resp)
}

// --- /api/v1/hosts/{host}/docker/compose/projects --------------------------

func (h *Handler) handleHostComposeProjects(w http.ResponseWriter, r *http.Request) {
        host := r.PathValue("host")
        if host == "" {
                host = pathLastSegment(r.URL.Path)
        }
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
        if !hh.Caps.Compose {
                reason := "not-configured"
                if r, ok := hh.Caps.Reasons["compose"]; ok {
                        reason = r
                }
                status := http.StatusServiceUnavailable
                typ := "capability-unavailable"
                if reason == "permission-denied" {
                        status = http.StatusForbidden
                        typ = "permission-denied"
                }
                h.writeProblem(w, r, status, typ,
                        "Compose not available", "Compose is not enabled on this host: "+reason, host)
                return
        }
        inv := h.store.Containers(host)
        resp := map[string]any{
                "host":     host,
                "projects": inv.ComposeProjects,
        }
        writeJSON(w, http.StatusOK, resp)
}

// --- helpers ----------------------------------------------------------------

func atoiPositive(s string) (int, error) {
        var n int
        if _, err := parseInt(s, &n); err != nil {
                return 0, err
        }
        if n < 1 {
                return 0, errInvalid
        }
        return n, nil
}

func parseInt(s string, n *int) (int, error) {
        for _, c := range s {
                if c < '0' || c > '9' {
                        return 0, errInvalid
                }
                *n = *n*10 + int(c-'0')
        }
        return *n, nil
}

func toLower(s string) string {
        out := make([]byte, len(s))
        for i := 0; i < len(s); i++ {
                c := s[i]
                if c >= 'A' && c <= 'Z' {
                        c += 'a' - 'A'
                }
                out[i] = c
        }
        return string(out)
}

func contains(hay, needle string) bool {
        if len(needle) == 0 {
                return true
        }
        if len(hay) < len(needle) {
                return false
        }
        for i := 0; i <= len(hay)-len(needle); i++ {
                if hay[i:i+len(needle)] == needle {
                        return true
                }
        }
        return false
}
