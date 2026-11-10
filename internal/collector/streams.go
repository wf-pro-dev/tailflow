// Refcounted on-demand streams for container stats and logs (design §F7,
// §16.4 "On-demand streams").
//
// A stream to tailkitd opens when the first browser subscribes to a container
// and closes (after a 10 s grace period) when the last unsubscribes. This
// bounds remote load to what people are actually watching.
//
// The manager is keyed by (host, containerID, kind) where kind is "stats" or
// "logs". Each key has:
//   - a reference count
//   - a 10s close timer (started when refcount reaches 0)
//   - an upstream cancel function (to stop the tailkitd stream)
//   - a fan-out list of subscriber channels
//
// For logs, a bounded ring buffer (last 5000 lines) is maintained while the
// upstream is open, so new subscribers get recent history immediately.

package collector

import (
        "context"
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
type streamEntry struct {
        mu          sync.Mutex
        refCount    int
        upstreamCancel context.CancelFunc
        subs        []chan StreamEvent
        closeTimer  *time.Timer

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
}

// NewStreamManager returns a StreamManager owned by the given collector.
func (c *Collector) NewStreamManager() *StreamManager {
        return &StreamManager{
                collector: c,
                logger:    c.logger.With("component", "stream-manager"),
                streams:   make(map[streamKey]*streamEntry),
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

        // Replay buffered data to the new subscriber.
        if kind == StreamLogs && entry.logBufFull {
                go func() {
                        // Send buffered logs in order.
                        n := len(entry.logBuf)
                        for i := 0; i < n; i++ {
                                idx := (entry.logBufIdx + i) % n
                                select {
                                case subCh <- StreamEvent{Kind: StreamLogs, Data: entry.logBuf[idx]}:
                                case <-ctx.Done():
                                        return
                                }
                        }
                }()
        }
        if kind == StreamStats && entry.latestStats != nil {
                go func() {
                        select {
                        case subCh <- StreamEvent{Kind: StreamStats, Data: entry.latestStats}:
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
func (m *StreamManager) runStatsStream(ctx context.Context, scope client.HostScope, key streamKey, entry *streamEntry) {
        logger := m.logger.With("host", key.host, "container", key.containerID, "stream", "stats")
        var lastStatsPublish time.Time
        err := scope.Docker().Container(key.containerID).StreamStats(ctx, func(e client.Event[container.StatsResponse]) error {
                entry.mu.Lock()
                // Store latest for replay to new subscribers.
                stats := e
                entry.latestStats = &stats
                subs := make([]chan StreamEvent, len(entry.subs))
                copy(subs, entry.subs)
                entry.mu.Unlock()

                // Extract and store per-container stats (throttled to 2s)
                if time.Since(lastStatsPublish) > 2*time.Second {
                        lastStatsPublish = time.Now()
                        extracted := extractContainerStats(e.Data)
                        m.collector.store.SetContainerStats(key.host, key.containerID, extracted)
                        m.collector.store.PublishContainerStats(key.host, key.containerID, extracted)
                }

                for _, ch := range subs {
                        select {
                        case ch <- StreamEvent{Kind: StreamStats, Data: &stats}:
                        default:
                        }
                }
                return nil
        })
        if err != nil && ctx.Err() == nil {
                logger.Warn("stats stream ended", "err", err)
        }
}

// runLogsStream opens the tailkitd logs stream and fans out events. Also
// accumulates the last 5000 lines in a ring buffer for replay to new
// subscribers.
func (m *StreamManager) runLogsStream(ctx context.Context, scope client.HostScope, key streamKey, entry *streamEntry) {
        logger := m.logger.With("host", key.host, "container", key.containerID, "stream", "logs")
        // Default tail: 200 lines (the API spec says default 200, max 5000).
        // We request 5000 from the upstream so the buffer fills immediately.
        err := scope.Docker().Container(key.containerID).StreamLogs(ctx, logBufferLines, func(e client.Event[client.LogLine]) error {
                entry.mu.Lock()
                // Append to the ring buffer.
                if len(entry.logBuf) < logBufferLines {
                        entry.logBuf = append(entry.logBuf, e.Data)
                } else {
                        entry.logBuf[entry.logBufIdx] = e.Data
                        entry.logBufIdx = (entry.logBufIdx + 1) % logBufferLines
                        entry.logBufFull = true
                }
                subs := make([]chan StreamEvent, len(entry.subs))
                copy(subs, entry.subs)
                entry.mu.Unlock()

                for _, ch := range subs {
                        select {
                        case ch <- StreamEvent{Kind: StreamLogs, Data: e.Data}:
                        default:
                                // Drop if subscriber is slow.
                        }
                }
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
