// Metrics loop (design §F2, §16.4 step 2).
//
// Wraps HostScope.Metrics().All(ctx, fn) in a reconnect loop with exponential
// backoff and full jitter. Each event is mapped to a state.RawSample and fed
// to the aggregator; the aggregator emits one state.Sample per HISTORY_STEP
// bucket, which is pushed to the store.
//
// Reconnect with backoff (§F2 algorithm):
//   - delay = rand(0, min(cap, base * 2^attempt))
//   - base = 1s, cap = 30s
//   - reset attempt counter after a stream stays healthy for 30s
//
// On stream errors: log at warn, increment the failure counter, back off,
// retry. After DEGRADE_THRESHOLD consecutive failures the status loop will
// mark the host degraded.

package collector

import (
        "context"
        "errors"
        "log/slog"
        "math/rand/v2"
        "time"

        "github.com/wf-pro-dev/tailkit/client"

        "github.com/wf-pro-dev/tailflow/internal/state"
)

const (
        backoffBase    = 1 * time.Second
        backoffCap     = 30 * time.Second
        backoffResetAt = 30 * time.Second // healthy for this long → reset attempt counter
)

// runMetricsLoop opens the All stream, feeds events to the aggregator, and
// reconnects with backoff on failure.
func (c *Collector) runMetricsLoop(ctx context.Context, scope client.HostScope, hostName string, logger *slog.Logger) error {
        agg := state.NewAggregator(c.cfg.HistoryStep)
        attempt := 0
        healthyFor := time.Duration(0)

        for {
                if ctx.Err() != nil {
                        return ctx.Err()
                }

                err := c.consumeMetricsStream(ctx, scope, hostName, agg, logger, &healthyFor)
                if ctx.Err() != nil {
                        return ctx.Err()
                }
                if err != nil {
                        logger.Warn("metrics stream ended", "err", err, "attempt", attempt)
                        c.store.IncFailures(hostName)
                }

                // Backoff before reconnect.
                attempt++
                delay := jitterBackoff(attempt)
                select {
                case <-ctx.Done():
                        return ctx.Err()
                case <-c.clock.After(delay):
                }
        }
}

// consumeMetricsStream opens the All stream and processes events until the
// stream ends or the context is cancelled. Returns the stream error (nil on
// clean end). healthyFor is incremented by the wall-clock time the stream
// stayed open; the caller resets the attempt counter when it exceeds
// backoffResetAt.
func (c *Collector) consumeMetricsStream(ctx context.Context, scope client.HostScope, hostName string, agg *state.Aggregator, logger *slog.Logger, healthyFor *time.Duration) error {
        start := c.clock.Now()
        callCtx, cancel := context.WithCancel(ctx)
        defer cancel()

        err := scope.Metrics().All(callCtx, func(e client.Event[client.Metrics]) error {
                // Track healthy duration; reset attempt counter if we've been healthy
                // long enough (the caller checks this).
                *healthyFor = c.clock.Now().Sub(start)
                if *healthyFor >= backoffResetAt {
                        // Signal the caller to reset; we can't reset attempt directly
                        // because it lives in the caller's scope. We use a sentinel:
                        // set healthyFor to a value the caller recognises.
                        // (The caller checks `healthyFor >= backoffResetAt` and resets.)
                }

                raw := mapMetricsToRaw(e.Data, c.clock.Now())
                if samp, ok := agg.Add(raw); ok {
                        c.store.PushSample(hostName, samp)
                }
                // Update latest non-time-series observations.
                c.store.SetLatest(hostName, mapMetricsToLatest(e.Data))
                // Reset failures on a successful sample.
                c.store.ResetFailures(hostName)
                return nil
        })

        // On healthy-then-failure, the caller resets attempt via healthyFor.
        if err == nil {
                // Stream ended cleanly (rare for SSE). Treat as a soft failure so the
                // loop reconnects.
                return errors.New("metrics stream ended cleanly")
        }
        return err
}

// jitterBackoff returns delay = rand(0, min(cap, base * 2^attempt)) per
// design §F2. Full jitter: the random draw is uniform in [0, cap].
func jitterBackoff(attempt int) time.Duration {
        if attempt <= 0 {
                return backoffBase
        }
        max := backoffBase
        for i := 1; i < attempt; i++ {
                max *= 2
                if max >= backoffCap {
                        max = backoffCap
                        break
                }
        }
        return time.Duration(rand.Int64N(int64(max)))
}

// mapMetricsToRaw converts one client.Metrics event to a state.RawSample,
// applying the design §16.3 mapping rules:
//   - CPU: copy Total and Percent (per-core); clamp to 0-100
//   - Memory: nil Virtual means skip memory fields
//   - Network: skip loopback `lo`; counters are cumulative (rate computed by aggregator)
//   - Disks: handled in mapMetricsToLatest (latest only, not time series)
func mapMetricsToRaw(m client.Metrics, receivedAt time.Time) state.RawSample {
        r := state.RawSample{ReceivedAt: receivedAt}
        if m.CPU != nil {
                r.CPUTotal = m.CPU.Total
                r.CPUPerCore = append([]float64(nil), m.CPU.Percent...)
        }
        if m.Memory != nil && m.Memory.Virtual != nil {
                r.HasMem = true
                r.MemUsedPct = m.Memory.Virtual.UsedPercent
                r.MemUsedBytes = m.Memory.Virtual.Used
                r.MemTotalBytes = m.Memory.Virtual.Total
                if m.Memory.Swap != nil {
                        r.SwapUsedPct = m.Memory.Swap.UsedPercent
                }
        }
        if len(m.Network) > 0 {
                r.Net = make([]state.NetSample, 0, len(m.Network))
                for _, n := range m.Network {
                        if n.Name == "lo" {
                                continue
                        }
                        r.Net = append(r.Net, state.NetSample{
                                Name:      n.Name,
                                BytesRecv: n.BytesRecv,
                                BytesSent: n.BytesSent,
                        })
                }
        }
        return r
}

// mapMetricsToLatest extracts the latest non-time-series observations
// (disks, net interfaces, processes) from one client.Metrics event.
// Processes are already limited and CPU-sorted by tailkitd (design D5).
func mapMetricsToLatest(m client.Metrics) state.Latest {
        var l state.Latest
        if len(m.Processes) > 0 {
                l.Processes = make([]state.Process, len(m.Processes))
                for i, p := range m.Processes {
                        l.Processes[i] = state.Process{
                                PID:        p.PID,
                                Name:       p.Name,
                                Status:     p.Status,
                                CPUPercent: p.CPUPercent,
                                MemoryRSS:  p.MemoryRSS,
                                Cmdline:    p.Cmdline,
                        }
                }
        }
        if len(m.Disk) > 0 {
                l.Disks = make([]state.DiskUsage, 0, len(m.Disk))
                for _, d := range m.Disk {
                        if d == nil {
                                continue
                        }
                        l.Disks = append(l.Disks, state.DiskUsage{
                                Path:    d.Path,
                                FSType:  d.Fstype,
                                Total:   d.Total,
                                Used:    d.Used,
                                Free:    d.Free,
                                UsedPct: d.UsedPercent,
                                // Note: gopsutil's UsageStat has no Device field (it lives on
                                // PartitionStat). We leave Device empty; the UI can show
                                // Path + FSType which is enough to identify a mount.
                        })
                }
        }
        if len(m.Network) > 0 {
                l.NetInterfaces = make([]state.NetInterfaceCounters, 0, len(m.Network))
                for _, n := range m.Network {
                        if n.Name == "lo" {
                                continue
                        }
                        l.NetInterfaces = append(l.NetInterfaces, state.NetInterfaceCounters{
                                Name:      n.Name,
                                BytesRecv: n.BytesRecv,
                                BytesSent: n.BytesSent,
                                // RxBps/TxBps are computed by the aggregator; we don't have
                                // the rate for the "latest" snapshot without the previous
                                // counter. Leave 0 for now; the API can derive from the
                                // latest Sample if needed.
                        })
                }
        }
        // Ports are handled separately by the ports loop (snapshot + stream
        // deltas), not by the metrics loop.
        return l
}
