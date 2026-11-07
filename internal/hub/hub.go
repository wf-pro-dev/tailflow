// Hub is Tailflow's SSE fan-out with a bounded replay buffer. It is the
// single point that collects events from all collector supervisors and
// delivers them to connected browsers.
//
// Design refs: §F8 (SSE delivery), §16.6 (Hub contract).
//
// Contract (§16.6):
//
//      type HubEvent struct { ID uint64; Topic, Name string; Data any }
//      func (h *Hub) Publish(e HubEvent)                                   // assigns ID, appends to replay ring
//      func (h *Hub) Subscribe(topics []string, lastID uint64) (*Sub, error) // ErrGapTooOld -> caller sends resync
//      func (s *Sub) C() <-chan HubEvent
//      func (s *Sub) Close()
//
// Backpressure (§F8):
//   - Each subscriber has a buffered channel of 256.
//   - When full: if the pending event is metrics.sample, replace the newest
//     queued sample for that host (idempotent); otherwise close the
//     subscription with reason "slow-consumer" so the handler emits resync.
//   - Structural events (host.removed, port.bound, etc.) are never dropped.
//
// Resume (§F8):
//   - Every event carries a monotonic ID assigned by the Hub.
//   - The Hub retains the last `replaySize` events (default 1,000).
//   - On reconnect with Last-Event-ID, the server replays the gap, or sends
//     `resync` if the gap is too old (ID not in the replay ring).
package hub

import (
        "errors"
        "sync"
        "sync/atomic"
)

// DefaultReplaySize is the default capacity of the replay ring (design §F8).
const DefaultReplaySize = 1000

// DefaultSubBuffer is the default per-subscriber channel buffer (§16.6).
const DefaultSubBuffer = 256

// ErrGapTooOld is returned by Subscribe when the requested lastID is older
// than the oldest event still in the replay ring. The handler should emit a
// `resync` event and end the response.
var ErrGapTooOld = errors.New("hub: replay gap too old; client must resync")

// HubEvent is one Tailflow-internal SSE event. It carries the Topic (for
// routing to subscribers), the Name (wire-format event name), the Data
// (JSON-marshalled payload), and the ID (assigned by Publish).
//
// This is distinct from client.Event[T] (the typed envelope shared with
// tailkitd, aliased as Event[T] in sse.go) because the Hub deals in untyped
// payloads and needs a Topic field for routing — client.Event[T] has no
// Topic. The API handler converts HubEvent to client.Event[T] at the wire
// boundary if needed.
type HubEvent struct {
        ID    uint64
        Topic string // "hosts", "metrics:vps-1", "docker:vps-2", etc.
        Name  string // wire-format event name: host.updated, metrics.sample, ...
        Data  any    // JSON-marshalled at write time
}

// Hub collects events from collectors and fans them out to subscribers.
type Hub struct {
        mu          sync.RWMutex
        seq         atomic.Uint64
        replay      []HubEvent
        replaySize  int
        replayStart int // index of the oldest event in `replay`
        replayLen   int // number of valid events in `replay`

        subs map[int64]*Sub
        nextSubID atomic.Int64

        subBuffer int
}

// NewHub returns a Hub with the default replay size (1,000) and sub buffer
// (256). Use NewHubWithSizes to override.
func NewHub() *Hub {
        return NewHubWithSizes(DefaultReplaySize, DefaultSubBuffer)
}

// NewHubWithSizes returns a Hub with explicit replay and sub-buffer sizes.
// Useful for tests that want a small replay ring to exercise the
// ErrGapTooOld path.
func NewHubWithSizes(replaySize, subBuffer int) *Hub {
        if replaySize < 1 {
                replaySize = DefaultReplaySize
        }
        if subBuffer < 1 {
                subBuffer = DefaultSubBuffer
        }
        return &Hub{
                replay:    make([]HubEvent, replaySize),
                replaySize: replaySize,
                subs:      make(map[int64]*Sub),
                subBuffer: subBuffer,
        }
}

// Publish assigns a monotonic ID to e, appends it to the replay ring, and
// fans it out to every matching subscriber. It never blocks; subscribers that
// cannot keep up are closed (see §F8 backpressure rules).
func (h *Hub) Publish(e HubEvent) {
        e.ID = h.seq.Add(1)

        h.mu.Lock()
        // Append to the replay ring (circular buffer).
        if h.replayLen < h.replaySize {
                idx := (h.replayStart + h.replayLen) % h.replaySize
                h.replay[idx] = e
                h.replayLen++
        } else {
                // Ring is full; overwrite the oldest.
                h.replay[h.replayStart] = e
                h.replayStart = (h.replayStart + 1) % h.replaySize
        }
        // Snapshot the subscriber list under the lock so we don't race with
        // Subscribe/Close.
        subs := make([]*Sub, 0, len(h.subs))
        for _, s := range h.subs {
                subs = append(subs, s)
        }
        h.mu.Unlock()

        for _, s := range subs {
                // Only deliver if the subscriber is interested in this topic.
                if _, ok := s.topics[e.Topic]; !ok {
                        continue
                }
                s.deliver(e)
        }
}

// Subscribe returns a new subscriber for the given topics. If lastID > 0, the
// Hub first replays every event with ID > lastID that is still in the replay
// ring and match one of the topics. If lastID is older than the oldest event
// in the ring, Subscribe returns ErrGapTooOld (the handler should emit a
// `resync` event and end the response).
//
// Topic syntax:
//   - "hosts"            — matches events with Topic == "hosts"
//   - "metrics:vps-1"    — matches events with Topic == "metrics:vps-1"
//   - "docker:vps-2"     — matches events with Topic == "docker:vps-2"
//
// An empty topics slice subscribes to nothing (useful for a "just keep the
// connection alive" mode).
func (h *Hub) Subscribe(topics []string, lastID uint64) (*Sub, error) {
        h.mu.Lock()
        defer h.mu.Unlock()

        // If a lastID was given, check it's within the replay window.
        if lastID > 0 {
                if h.replayLen == 0 {
                        // No events to replay; that's fine, just subscribe from now.
                } else {
                        oldest := h.replay[h.replayStart].ID
                        if lastID < oldest {
                                return nil, ErrGapTooOld
                        }
                }
        }

        topicSet := make(map[string]struct{}, len(topics))
        for _, t := range topics {
                topicSet[t] = struct{}{}
        }

        id := h.nextSubID.Add(1)
        s := &Sub{
                id:       id,
                hub:      h,
                topics:   topicSet,
                c:        make(chan HubEvent, h.subBuffer),
                closedCh: make(chan struct{}),
        }
        h.subs[id] = s

        // Replay matching events with ID > lastID, in order. When lastID is 0,
        // every event in the ring qualifies (the subscriber has seen nothing).
        if h.replayLen > 0 {
                for i := 0; i < h.replayLen; i++ {
                        idx := (h.replayStart + i) % h.replaySize
                        e := h.replay[idx]
                        if e.ID <= lastID {
                                continue
                        }
                        if _, ok := topicSet[e.Topic]; ok {
                                // Push to the subscriber's channel. The channel has `subBuffer`
                                // capacity; replay should always fit since we just created it.
                                select {
                                case s.c <- e:
                                default:
                                        // Channel filled during replay (unlikely with a fresh sub
                                        // unless replaySize > subBuffer). Close the sub and signal
                                        // resync.
                                        s.closeLocked()
                                        return nil, ErrGapTooOld
                                }
                        }
                }
        }

        return s, nil
}

// Unsubscribe removes s from the hub. Called by Sub.Close(); safe to call
// multiple times.
func (h *Hub) unsubscribe(s *Sub) {
        h.mu.Lock()
        defer h.mu.Unlock()
        delete(h.subs, s.id)
}

// Sub is one SSE subscriber. Events are delivered on C(); the handler reads
// them in a loop and writes to the SSE Writer. When the channel is closed
// (or C() returns nothing and Closed() is true), the handler should end the
// response.
//
// All access to the channel `c` is guarded by `mu` to prevent send-on-closed
// races between deliver() and Close().
type Sub struct {
        id       int64
        hub      *Hub
        topics   map[string]struct{}
        mu       sync.Mutex
        c        chan HubEvent
        closedCh chan struct{}
        reason   string
        once     sync.Once
}

// C returns the channel on which events are delivered. The channel is closed
// when the subscription is closed (either by the subscriber or by the hub
// due to backpressure).
func (s *Sub) C() <-chan HubEvent { return s.c }

// Close cancels the subscription. Safe to call multiple times.
func (s *Sub) Close() {
        s.once.Do(func() {
                // Acquire s.mu so we don't race with a concurrent deliver() that's
                // mid-send. If deliver() already closed the channels via the
                // backpressure path, once.Do is a no-op here.
                s.mu.Lock()
                close(s.closedCh)
                close(s.c)
                s.mu.Unlock()
                s.hub.unsubscribe(s)
        })
}

// Closed returns a channel that is closed when the subscription is closed.
func (s *Sub) Closed() <-chan struct{} { return s.closedCh }

// Reason returns why the subscription was closed. Empty for normal Close();
// "slow-consumer" for backpressure closure. Always safe to call after
// Closed() is signalled.
func (s *Sub) Reason() string {
        s.mu.Lock()
        defer s.mu.Unlock()
        return s.reason
}

// slowConsumerReason is the close reason set when the hub closes a sub due
// to backpressure (§F8). The handler reads Reason() and emits `resync`.
const slowConsumerReason = "slow-consumer"

// deliver pushes e into the subscriber's channel, applying the §F8
// backpressure rules:
//   - If the channel is full and e is a metrics.sample, replace the newest
//     queued sample for the same host (idempotent — the browser only needs
//     the latest).
//   - If the channel is full and e is structural, close the subscription
//     with reason "slow-consumer". The handler will emit `resync`.
//
// deliver is safe to call concurrently with Close() and with other deliver
// calls (the hub calls deliver from Publish, which may run on many goroutines
// simultaneously if collectors publish in parallel).
func (s *Sub) deliver(e HubEvent) {
        s.mu.Lock()
        defer s.mu.Unlock()

        // If already closed, drop the event.
        select {
        case <-s.closedCh:
                return
        default:
        }

        // Fast path: channel has room.
        select {
        case s.c <- e:
                return
        default:
        }

        // Slow path: channel is full.
        if e.Name == EventMetricsSample {
                // Coalesce: replace the newest queued sample for the same host.
                // The browser only needs the latest metric sample per host.
                s.replaceNewestSampleLocked(e)
                return
        }

        // Structural event with a full channel: close the sub via once.Do so
        // a concurrent Close() call doesn't double-close. We set the reason
        // before once.Do so it's visible to Reason() after Closed() fires.
        s.reason = slowConsumerReason
        s.once.Do(func() {
                close(s.closedCh)
                close(s.c)
        })
        // Remove from the hub asynchronously to avoid holding s.mu while
        // acquiring h.mu (lock-order inversion risk).
        go s.hub.unsubscribe(s)
}

// replaceNewestSampleLocked drains the channel, replaces the last event with
// the same Topic (host) with e, and re-queues everything. Caller must hold
// s.mu. If no matching event is found, e is appended (or dropped if the
// channel is still full after draining).
func (s *Sub) replaceNewestSampleLocked(e HubEvent) {
        // Drain everything currently in the channel.
        var queued []HubEvent
        for {
                select {
                case q := <-s.c:
                        queued = append(queued, q)
                default:
                        goto done
                }
        }
done:
        // Find the last event with the same topic and replace it.
        replaced := false
        for i := len(queued) - 1; i >= 0; i-- {
                if queued[i].Topic == e.Topic {
                        queued[i] = e
                        replaced = true
                        break
                }
        }
        if !replaced {
                // No matching sample; append e.
                queued = append(queued, e)
        }
        // Re-queue. If we can't fit everything (shouldn't happen since we just
        // drained the whole channel, but be defensive), drop the oldest.
        for _, q := range queued {
                select {
                case s.c <- q:
                default:
                        select {
                        case <-s.c:
                        default:
                        }
                        select {
                        case s.c <- q:
                        default:
                        }
                }
        }
}

// closeLocked is called by Subscribe when replay fills the channel. It closes
// the subscription without setting a "slow-consumer" reason (the caller will
// return ErrGapTooOld which the handler translates to a resync).
//
// Safe to call without s.mu because the sub has not been returned to the
// caller yet — no other goroutine has a reference to it.
func (s *Sub) closeLocked() {
        s.once.Do(func() {
                close(s.closedCh)
                close(s.c)
        })
}
