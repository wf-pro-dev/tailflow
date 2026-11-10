// Refcounted on-demand streams for container stats and logs (design §F7,
// §16.4 "On-demand streams").
//
// A stream to tailkitd opens when the first subscriber joins (browser SSE
// connection or the collector's own background subscription) and closes after
// a 10 s grace period when the last subscriber leaves. This bounds remote load
// to what is actually being consumed.
//
// In addition to on-demand browser subscriptions, the docker loop opens a
// background stats subscription for every running container after each
// inventory refetch (design §F7 "background subscriptions"). This keeps the
// Docker tab's CPU/memory columns and the fleet overview's Top Containers panel
// populated without requiring a user to open the container drawer first.
//
// The manager is keyed by (host, containerID, kind) where kind is "stats" or
// "logs". Each key has:
//   - a reference count (browser subs + background sub)
//   - a 10s close timer (started when refcount reaches 0)
//   - an upstream cancel function (to stop the tailkitd stream)
//   - a fan-out list of subscriber channels
//   - a reconnect loop with exponential backoff (stats only)
//
// For logs, a bounded ring buffer (last 5000 lines) is maintained while the
// upstream is open, so new subscribers get recent history immediately.

package collector

import (
        "context"
        "errors"
        "fmt"
        "log/slog"
        "sync"
        "time"

        "github.com/docker/docker/api/types/container"
        "github.com/wf-pro-dev/tailkit/client"

        "github.com/wf-pro-dev/tailflow/internal/state"
)

const (
        statsGraceClose = 10 * time.Second
        logBufferLines  = 5000
)

// StreamKind is "stats" or "logs".
type StreamKind string

const (
        StreamStats StreamKind = "stats"
        StreamLogs  StreamKind = "logs"
)

// streamKey identifies one upstream stream.
type streamKey struct {
        host        string
        containerID string
        kind        StreamKind
}

// streamEntry is the per-key state.
//
// mu is an RWMutex: write-locked for structural changes (sub add/remove,
// refcount, buffer append, latestStats update) and read-locked during fan-out
// so unsubscribe can't close a subscriber channel while the sender is in the
// middle of delivering to it (which would panic on send-to-closed-channel).
type streamEntry struct {
        mu             sync.RWMutex
        refCount       int
        upstreamCancel context.CancelFunc
        subs           []chan StreamEvent
        closeTimer     *time.Timer

        // Log buffer (only used for kind=="logs"). Bounded ring of last N lines.
        logBuf     []client.LogLine
        logBufIdx  int // ring write index
        logBufFull bool

        // Latest stats (only used for kind=="stats"). Replayed to new subscribers.
        latestStats *client.Event[container.StatsResponse]
}

// StreamEvent is one event delivered to a subscriber. For stats, Data is
// *client.Event[container.StatsResponse] (aliased as client.ContainerStats).
// For logs, Data is client.LogLine.
type StreamEvent struct {
        Kind StreamKind
        Data any
        Err  error
}

// Sub is one subscriber's handle. Events delivers events; Close unsubscribes.
type Sub struct {
        events chan StreamEvent
        close  func()
}

// Events returns the channel on which events are delivered.
func (s *Sub) Events() <-chan StreamEvent { return s.events }

// Close unsubscribes. Safe to call multiple times.
func (s *Sub) Close() {
        if s.close != nil {
                s.close()
        }
}

// StreamManager owns the refcounted on-demand streams.
type StreamManager struct {
        collector *Collector
        logger    *slog.Logger

        mu      sync.Mutex
        streams map[streamKey]*streamEntry

        // Background subs are opened by the docker loop for all running containers
        // so that container stats are continuously collected even when no browser
        // is watching. Keyed by (host, containerID).
        bgMu   sync.Mutex
        bgSubs map[bgSubKey]*Sub
}

// bgSubKey identifies one background stats subscription.
type bgSubKey struct {
        host        string
        containerID string
}

// NewStreamManager returns a StreamManager owned by the given collector.
func (c *Collector) NewStreamManager() *StreamManager {
        return &StreamManager{
                collector: c,
                logger:    c.logger.With("component", "stream-manager"),
                streams:   make(map[streamKey]*streamEntry),
                bgSubs:    make(map[bgSubKey]*Sub),
        }
}

// Subscribe opens (or joins) the upstream stream for (host, containerID, kind)
// and returns a subscriber handle. The upstream opens on the first subscriber
// and closes 10s after the last unsubscribe.
func (m *StreamManager) Subscribe(ctx context.Context, host, containerID string, kind StreamKind) (*Sub, error) {
        key := streamKey{host: host, containerID: containerID, kind: kind}

        m.mu.Lock()
        entry, exists := m.streams[key]
        if !exists {
                entry = &streamEntry{}
                m.streams[key] = entry
        }
        m.mu.Unlock()

        entry.mu.Lock()
        defer entry.mu.Unlock()

        // Cancel any pending close timer (we have a new subscriber).
        if entry.closeTimer != nil {
                entry.closeTimer.Stop()
                entry.closeTimer = nil
        }

        // Open upstream if this is the first subscriber.
        if entry.refCount == 0 {
                if err := m.openUpstream(ctx, key, entry); err != nil {
                        // Clean up the empty entry.
                        m.mu.Lock()
                        delete(m.streams, key)
                        m.mu.Unlock()
                        return nil, err
                }
        }

        // Create the subscriber channel.
        subCh := make(chan StreamEvent, 64)
        entry.subs = append(entry.subs, subCh)
        entry.refCount++

        // Replay buffered data to the new subscriber. We snapshot the data
        // under the write lock and send from the copy in a goroutine so the
        // sender doesn't race with runStatsStream/runLogsStream mutating the
        // buffer or latestStats.
        if kind == StreamLogs && entry.logBufFull {
                buf := make([]client.LogLine, len(entry.logBuf))
                copy(buf, entry.logBuf)
                startIdx := entry.logBufIdx
                go func() {
                        n := len(buf)
                        for i := 0; i < n; i++ {
                                idx := (startIdx + i) % n
                                select {
                                case subCh <- StreamEvent{Kind: StreamLogs, Data: buf[idx]}:
                                case <-ctx.Done():
                                        return
                                }
                        }
                }()
        }
        if kind == StreamStats && entry.latestStats != nil {
                snap := *entry.latestStats
                go func() {
                        select {
                        case subCh <- StreamEvent{Kind: StreamStats, Data: &snap}:
                        case <-ctx.Done():
                        }
                }()
        }

        sub := &Sub{
                events: subCh,
                close: func() {
                        m.unsubscribe(key, subCh)
                },
        }
        return sub, nil
}

// unsubscribe decrements the refcount and starts the close timer if it
// reaches 0.
func (m *StreamManager) unsubscribe(key streamKey, subCh chan StreamEvent) {
        m.mu.Lock()
        entry, ok := m.streams[key]
        m.mu.Unlock()
        if !ok {
                return
        }

        entry.mu.Lock()
        defer entry.mu.Unlock()

        // Remove the subscriber channel from the list.
        for i, ch := range entry.subs {
                if ch == subCh {
                        entry.subs = append(entry.subs[:i], entry.subs[i+1:]...)
                        close(ch)
                        break
                }
        }
        entry.refCount--

        if entry.refCount <= 0 {
                entry.refCount = 0
                // Start the grace-close timer.
                if entry.closeTimer != nil {
                        entry.closeTimer.Stop()
                }
                entry.closeTimer = time.AfterFunc(statsGraceClose, func() {
                        m.closeUpstream(key)
                })
        }
}

// openUpstream starts the tailkitd stream for this key. Runs in the caller's
// goroutine; the stream itself runs in a background goroutine.
func (m *StreamManager) openUpstream(ctx context.Context, key streamKey, entry *streamEntry) error {
        scope := m.collector.fleet.Host(key.host)

        upstreamCtx, cancel := context.WithCancel(ctx)
        entry.upstreamCancel = cancel

        switch key.kind {
        case StreamStats:
                go m.runStatsStream(upstreamCtx, scope, key, entry)
        case StreamLogs:
                go m.runLogsStream(upstreamCtx, scope, key, entry)
        default:
                cancel()
                return fmt.Errorf("unknown stream kind: %s", key.kind)
        }
        return nil
}

// closeUpstream stops the tailkitd stream and removes the entry. Called when
// the grace-close timer fires.
func (m *StreamManager) closeUpstream(key streamKey) {
        m.mu.Lock()
        entry, ok := m.streams[key]
        m.mu.Unlock()
        if !ok {
                return
        }

        entry.mu.Lock()
        if entry.upstreamCancel != nil {
                entry.upstreamCancel()
                entry.upstreamCancel = nil
        }
        entry.mu.Unlock()

        m.mu.Lock()
        // Only delete if there are still no subscribers (someone might have
        // subscribed between the timer firing and now).
        if entry.refCount == 0 {
                delete(m.streams, key)
        }
        m.mu.Unlock()

        m.logger.Debug("upstream stream closed", "host", key.host, "container", key.containerID, "kind", key.kind)
}

// runStatsStream opens the tailkitd stats stream and fans out events.
// Also stores per-container stats in the store and publishes container.stats
// SSE events for reactive table cells.
//
// The stream reconnects with exponential backoff (full jitter) on transient
// errors, mirroring the metrics loop's reconnect strategy (design §F2). This
// keeps background subscriptions resilient to brief upstream interruptions.
// Permanent failures (docker unavailable, permission denied) cause the loop
// to exit so we don't spin forever on an unrecoverable condition.
func (m *StreamManager) runStatsStream(ctx context.Context, scope client.HostScope, key streamKey, entry *streamEntry) {
        logger := m.logger.With("host", key.host, "container", key.containerID, "stream", "stats")
        var lastStatsPublish time.Time
        attempt := 0
        healthyFor := time.Duration(0)

        for {
                if ctx.Err() != nil {
                        return
                }

                start := time.Now()
                err := scope.Docker().Container(key.containerID).StreamStats(ctx, func(e client.Event[container.StatsResponse]) error {
                        healthyFor = time.Since(start)

                        // Store latest for replay to new subscribers (write lock).
                        entry.mu.Lock()
                        stats := e
                        entry.latestStats = &stats
                        entry.mu.Unlock()

                        // Extract and store per-container stats (throttled to 2s).
                        if time.Since(lastStatsPublish) > 2*time.Second {
                                lastStatsPublish = time.Now()
                                extracted := extractContainerStats(e.Data)
                                m.collector.store.SetContainerStats(key.host, key.containerID, extracted)
                                m.collector.store.PublishContainerStats(key.host, key.containerID, extracted)
                        }

                        // Fan out under read lock so unsubscribe can't close a
                        // subscriber channel mid-send (send-to-closed-channel
                        // would panic). The non-blocking select means we never
                        // hold the lock for long.
                        entry.mu.RLock()
                        for _, ch := range entry.subs {
                                select {
                                case ch <- StreamEvent{Kind: StreamStats, Data: &stats}:
                                default:
                                }
                        }
                        entry.mu.RUnlock()
                        return nil
                })

                if ctx.Err() != nil {
                        return
                }

                if err != nil {
                        // Permanent failures: exit the reconnect loop.
                        if errors.Is(err, client.ErrDockerUnavailable) || errors.Is(err, client.ErrPermissionDenied) {
                                logger.Debug("stats stream unavailable, stopping", "err", err)
                                return
                        }
                        logger.Warn("stats stream ended, reconnecting", "err", err, "attempt", attempt)
                }

                // Reset attempt counter after a healthy run.
                if healthyFor > backoffResetAt {
                        attempt = 0
                }
                attempt++

                delay := jitterBackoff(attempt)
                select {
                case <-ctx.Done():
                        return
                case <-time.After(delay):
                }
        }
}

// runLogsStream opens the tailkitd logs stream and fans out events. Also
// accumulates the last 5000 lines in a ring buffer for replay to new
// subscribers.
//
// Unlike stats, logs do NOT reconnect on error: re-tailing would re-deliver
// old lines, duplicating them in the buffer and in any active browser. The
// stream stays closed until a new subscriber triggers a fresh open.
func (m *StreamManager) runLogsStream(ctx context.Context, scope client.HostScope, key streamKey, entry *streamEntry) {
        logger := m.logger.With("host", key.host, "container", key.containerID, "stream", "logs")
        // Default tail: 200 lines (the API spec says default 200, max 5000).
        // We request 5000 from the upstream so the buffer fills immediately.
        err := scope.Docker().Container(key.containerID).StreamLogs(ctx, logBufferLines, func(e client.Event[client.LogLine]) error {
                // Append to the ring buffer (write lock).
                entry.mu.Lock()
                if len(entry.logBuf) < logBufferLines {
                        entry.logBuf = append(entry.logBuf, e.Data)
                } else {
                        entry.logBuf[entry.logBufIdx] = e.Data
                        entry.logBufIdx = (entry.logBufIdx + 1) % logBufferLines
                        entry.logBufFull = true
                }
                entry.mu.Unlock()

                // Fan out under read lock (same race fix as runStatsStream).
                entry.mu.RLock()
                for _, ch := range entry.subs {
                        select {
                        case ch <- StreamEvent{Kind: StreamLogs, Data: e.Data}:
                        default:
                                // Drop if subscriber is slow.
                        }
                }
                entry.mu.RUnlock()
                return nil
        })
        if err != nil && ctx.Err() == nil {
                logger.Warn("logs stream ended", "err", err)
        }
}

// LogLine is an alias for client.LogLine, re-exported for convenience.
type LogLine = client.LogLine

// extractContainerStats pulls key fields from a Docker StatsResponse into
// state.ContainerStats for storage and SSE publishing.
func extractContainerStats(data container.StatsResponse) state.ContainerStats {
        cpu := data.CPUStats
        precpu := data.PreCPUStats
        mem := data.MemoryStats
        net := data.Networks

        var cpuPercent float64
        cpuTotal := cpu.CPUUsage.TotalUsage
        precpuTotal := precpu.CPUUsage.TotalUsage
        sysTotal := cpu.SystemUsage
        preSysTotal := precpu.SystemUsage
        onlineCpus := cpu.OnlineCPUs
        if onlineCpus == 0 {
                onlineCpus = 1
        }
        if sysTotal > preSysTotal && cpuTotal > precpuTotal {
                cpuPercent = float64(cpuTotal-precpuTotal) / float64(sysTotal-preSysTotal) * float64(onlineCpus) * 100
        }

        memUsed := mem.Usage
        memLimit := mem.Limit

        var netRx, netTx float64
        for _, n := range net {
                netRx += float64(n.RxBytes)
                netTx += float64(n.TxBytes)
        }

        return state.ContainerStats{
                CPUPercent:    cpuPercent,
                MemUsedBytes:  memUsed,
                MemLimitBytes: memLimit,
                NetRxBps:      netRx,
                NetTxBps:      netTx,
        }
}

// ReconcileBackgroundSubs ensures a background stats stream is open for every
// container ID in running on host, and closes background subs for containers
// no longer in the set. Called by the docker loop after each inventory refetch
// so the Docker tab and fleet overview show live container CPU/memory without
// requiring a user to open the container drawer.
//
// Background subs are regular Sub values whose context is the supervisor's
// context. They keep the upstream stream alive (refcount >= 1) even when no
// browser is watching. When a browser later subscribes to the same container,
// it joins the existing stream (refcount 2) and gets the latest stats replayed
// immediately.
func (m *StreamManager) ReconcileBackgroundSubs(ctx context.Context, host string, running []string) {
        m.bgMu.Lock()
        wanted := make(map[string]bool, len(running))
        for _, id := range running {
                wanted[id] = true
        }

        // Close subs for containers no longer running.
        var toClose []*Sub
        for key, sub := range m.bgSubs {
                if key.host != host {
                        continue
                }
                if !wanted[key.containerID] {
                        toClose = append(toClose, sub)
                        delete(m.bgSubs, key)
                }
        }

        // Collect containers that need a new sub.
        var toOpen []string
        for _, id := range running {
                key := bgSubKey{host: host, containerID: id}
                if _, exists := m.bgSubs[key]; !exists {
                        toOpen = append(toOpen, id)
                }
        }
        m.bgMu.Unlock()

        // Close outside the lock (Close acquires entry.mu).
        for _, sub := range toClose {
                sub.Close()
        }

        // Open outside the lock (Subscribe acquires m.mu and entry.mu).
        for _, id := range toOpen {
                sub, err := m.Subscribe(ctx, host, id, StreamStats)
                if err != nil {
                        m.logger.Warn("background stats sub failed",
                                "host", host, "container", id, "err", err)
                        continue
                }
                m.bgMu.Lock()
                key := bgSubKey{host: host, containerID: id}
                if _, exists := m.bgSubs[key]; exists {
                        // A concurrent reconcile already opened one — close the
                        // duplicate to avoid leaking the upstream.
                        m.bgMu.Unlock()
                        sub.Close()
                } else {
                        m.bgSubs[key] = sub
                        m.bgMu.Unlock()
                }
        }
}

// CloseBackgroundSubs closes all background stats subs for a host. Called by
// the docker loop when it exits (host removed or docker disabled) so the
// upstream streams don't linger until the grace-close timer fires.
func (m *StreamManager) CloseBackgroundSubs(host string) {
        m.bgMu.Lock()
        var toClose []*Sub
        for key, sub := range m.bgSubs {
                if key.host == host {
                        toClose = append(toClose, sub)
                        delete(m.bgSubs, key)
                }
        }
        m.bgMu.Unlock()
        for _, sub := range toClose {
                sub.Close()
        }
}
