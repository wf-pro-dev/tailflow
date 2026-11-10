// /api/v1/hosts/* handlers. The Handler now holds a *state.Store so these
// can read live data; writes still come only from the collector.

package api

import (
        "encoding/json"
        "net/http"
        "sort"
        "strconv"
        "strings"
        "time"

        "github.com/wf-pro-dev/tailflow/internal/state"
)

// --- /api/v1/fleet/summary -------------------------------------------------

func (h *Handler) handleFleetSummary(w http.ResponseWriter, r *http.Request) {
        counts, meanCPU, meanMem, containersRunning := h.store.FleetSummary()
        resourceSummary := h.store.FleetResourceSummary()
        resp := map[string]any{
                "counts": map[string]int{
                        "healthy":  counts[state.StatusHealthy],
                        "degraded": counts[state.StatusDegraded],
                        "stale":    counts[state.StatusStale],
                        "no_agent": counts[state.StatusNoAgent],
                        "offline":  counts[state.StatusOffline],
                },
                "containers_running": containersRunning,
                "total_mem_bytes":    resourceSummary.TotalMemBytes,
                "total_mem_used":     resourceSummary.TotalMemUsed,
                "total_disk_bytes":   resourceSummary.TotalDiskBytes,
                "total_disk_used":    resourceSummary.TotalDiskUsed,
                "host_count":         resourceSummary.HostCount,
                "healthy_count":      resourceSummary.HealthyCount,
                "top_cpu_hosts":      resourceSummary.TopCPUHosts,
                "top_mem_hosts":      resourceSummary.TopMemHosts,
                "top_disk_hosts":     resourceSummary.TopDiskHosts,
        }
        if meanCPU == 0 && meanMem == 0 {
                resp["mean_cpu_pct"] = nil
                resp["mean_mem_pct"] = nil
        } else {
                resp["mean_cpu_pct"] = meanCPU
                resp["mean_mem_pct"] = meanMem
        }
        writeJSON(w, http.StatusOK, resp)
}

// --- /api/v1/hosts ----------------------------------------------------------

func (h *Handler) handleListHosts(w http.ResponseWriter, r *http.Request) {
        q := r.URL.Query()
        hosts := h.store.Hosts()

        // Filter
        hosts = filterHosts(hosts, q)

        // Sort
        hosts = sortHosts(hosts, q.Get("sort"), q.Get("order"))

        writeJSON(w, http.StatusOK, hosts)
}

// filterHosts applies the status/role/environment/tag/q query params.
func filterHosts(hosts []state.Host, q map[string][]string) []state.Host {
        status := getOne(q, "status")
        role := getOne(q, "role")
        environment := getOne(q, "environment")
        tag := getOne(q, "tag")
        search := strings.ToLower(getOne(q, "q"))

        out := hosts[:0]
        for _, h := range hosts {
                if status != "" && string(h.Status) != status {
                        continue
                }
                if role != "" && h.Role != role {
                        continue
                }
                if environment != "" && h.Environment != environment {
                        continue
                }
                if tag != "" && !containsString(h.Tags, tag) {
                        continue
                }
                if search != "" {
                        hay := strings.ToLower(h.Name + " " + h.DNSName + " " + strings.Join(h.Tags, " "))
                        if !strings.Contains(hay, search) {
                                continue
                        }
                }
                out = append(out, h)
        }
        return out
}

// sortHosts sorts by the given column. Default: name asc.
func sortHosts(hosts []state.Host, sortKey, order string) []state.Host {
        if sortKey == "" {
                sortKey = "name"
        }
        asc := order != "desc"

        less := func(i, j int) bool {
                a, b := hosts[i], hosts[j]
                var av, bv any
                switch sortKey {
                case "name":
                        av, bv = a.Name, b.Name
                case "status":
                        av, bv = statusRank(a.Status), statusRank(b.Status)
                case "cpu", "mem", "net", "containers", "last_seen":
                        // These require the latest sample; for the list view we use 0
                        // when no sample is available. A real implementation would join
                        // the latest sample here; for M1 we fall back to name sort.
                        av, bv = a.Name, b.Name
                default:
                        av, bv = a.Name, b.Name
                }
                switch va := av.(type) {
                case string:
                        vb := bv.(string)
                        if asc {
                                return va < vb
                        }
                        return va > vb
                case int:
                        vb := bv.(int)
                        if asc {
                                return va < vb
                        }
                        return va > vb
                }
                return false
        }
        sort.SliceStable(hosts, less)
        return hosts
}

// statusRank gives a stable ordering of statuses for sort.
func statusRank(s state.Status) int {
        switch s {
        case state.StatusHealthy:
                return 0
        case state.StatusDegraded:
                return 1
        case state.StatusStale:
                return 2
        case state.StatusNoAgent:
                return 3
        case state.StatusOffline:
                return 4
        }
        return 5
}

// --- /api/v1/hosts/{host} --------------------------------------------------

func (h *Handler) handleGetHost(w http.ResponseWriter, r *http.Request) {
        host := r.PathValue("host")
        if host == "" {
                // Fallback for older Go versions without PathValue; parse from URL.
                host = pathLastSegment(r.URL.Path)
        }
        hh, ok := h.store.Host(host)
        if !ok {
                h.writeProblem(w, r, http.StatusNotFound, "host-not-found",
                        "Host not found", "no host with that name in the current discovered set", host)
                return
        }
        writeJSON(w, http.StatusOK, hostToAPI(hh))
}

// hostToAPI converts a state.Host to the OpenAPI Host JSON shape. The main
// difference is stream_interval (Duration) → stream_interval_s (int).
func hostToAPI(h state.Host) map[string]any {
        lastSample := (*time.Time)(nil)
        if h.LastSampleAt != nil {
                lastSample = h.LastSampleAt
        }
        resp := map[string]any{
                "name":              h.Name,
                "role":              h.Role,
                "environment":       h.Environment,
                "tags":              h.Tags,
                "ts_ips":            h.TSIPs,
                "ts_dns_name":       h.DNSName,
                "os":                h.OS,
                "online":            h.Online,
                "status":            string(h.Status),
                "capabilities":      capsToAPI(h.Caps),
                "stream_interval_s": h.StreamIntervalSeconds(),
        }
        if lastSample != nil {
                resp["last_sample_at"] = lastSample.UTC().Format(time.RFC3339)
        } else {
                resp["last_sample_at"] = nil
        }
        if h.UptimeSeconds > 0 {
                resp["uptime_seconds"] = h.UptimeSeconds
        }
        return resp
}

func capsToAPI(c state.Capabilities) map[string]any {
        out := map[string]any{
                "metrics": c.Metrics,
                "docker":  c.Docker,
                "swarm":   c.Swarm,
                "compose": c.Compose,
        }
        if len(c.Reasons) > 0 {
                out["reasons"] = c.Reasons
        }
        return out
}

// --- /api/v1/hosts/{host}/metrics ------------------------------------------

func (h *Handler) handleHostMetrics(w http.ResponseWriter, r *http.Request) {
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

        window := parseWindow(r.URL.Query().Get("window"))
        samples := h.store.Series(host, window)

        // LTTB downsampling to at most 300 points (design §F4). Preserves peaks
        // far better than averaging.
        if len(samples) > 300 {
                samples = lttbDownsample(samples, 300,
                        func(s state.Sample) float64 { return float64(s.T.Unix()) },
                        func(s state.Sample) float64 { return s.CPUTotal })
        }

        resp := metricsResponse(host, samples, window, h.cfg.HistoryStep)
        writeJSON(w, http.StatusOK, resp)
}

// --- /api/v1/hosts/{host}/metrics/latest -----------------------------------

func (h *Handler) handleHostMetricsLatest(w http.ResponseWriter, r *http.Request) {
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
        samp, latest, ok := h.store.Latest(host)
        if !ok {
                h.writeProblem(w, r, http.StatusNotFound, "host-not-found",
                        "Host not found", "no sample available", host)
                return
        }
        resp := map[string]any{
                "host":           host,
                "t":              samp.T.Unix(),
                "cpu_total":      samp.CPUTotal,
                "mem_used_pct":   samp.MemUsedPct,
                "mem_used_bytes": samp.MemUsedBytes,
                "mem_total_bytes": samp.MemTotalBytes,
                "net_rx_bps":     samp.NetRxBps,
                "net_tx_bps":     samp.NetTxBps,
                "disks":          latest.Disks,
                "net_interfaces": latest.NetInterfaces,
        }
        writeJSON(w, http.StatusOK, resp)
}

// --- helpers ----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Cache-Control", "no-store")
        w.WriteHeader(status)
        _ = json.NewEncoder(w).Encode(v)
}

func getOne(q map[string][]string, key string) string {
        v := q[key]
        if len(v) == 0 {
                return ""
        }
        return v[0]
}

func containsString(s []string, v string) bool {
        for _, x := range s {
                if x == v {
                        return true
                }
        }
        return false
}

func parseWindow(s string) time.Duration {
        switch s {
        case "5m":
                return 5 * time.Minute
        case "1h":
                return time.Hour
        default:
                return 15 * time.Minute
        }
}

// (LTTB downsampling is in lttb.go; the old even-decimation helper was
// replaced in M2.)

func metricsResponse(host string, samples []state.Sample, window, step time.Duration) map[string]any {
        t := make([]int64, len(samples))
        cpuTotal := make([]any, len(samples))
        memUsedPct := make([]any, len(samples))
        netRx := make([]any, len(samples))
        netTx := make([]any, len(samples))
        memUsed := make([]any, len(samples))
        memTotal := make([]any, len(samples))
        for i, s := range samples {
                t[i] = s.T.Unix()
                cpuTotal[i] = s.CPUTotal
                memUsedPct[i] = s.MemUsedPct
                netRx[i] = s.NetRxBps
                netTx[i] = s.NetTxBps
                memUsed[i] = s.MemUsedBytes
                memTotal[i] = s.MemTotalBytes
        }
        return map[string]any{
                "host":     host,
                "window_s": int(window.Seconds()),
                "step_s":   int(step.Seconds()),
                "t":        t,
                "series": map[string]any{
                        "cpu_total":       cpuTotal,
                        "mem_used_pct":    memUsedPct,
                        "net_rx_bps":      netRx,
                        "net_tx_bps":      netTx,
                        "mem_used_bytes":  memUsed,
                        "mem_total_bytes": memTotal,
                },
        }
}

// pathLastSegment returns the part after the last "/" in path. Used as a
// fallback when r.PathValue is empty (Go 1.22+ pattern routing sets it, but
// our base-path-stripping logic means the router sees the stripped path).
func pathLastSegment(path string) string {
        if i := strings.LastIndex(path, "/"); i >= 0 {
                return path[i+1:]
        }
        return path
}

// Used by strconv import to avoid unused import when no int parsing happens
// in this file yet.
var _ = strconv.Atoi
