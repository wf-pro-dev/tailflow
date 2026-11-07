// Package hub owns the SSE fan-out for Tailflow. It is the only package that
// writes SSE frames to browsers.
//
// The Writer in this file mirrors tailkitd's internal/sse/writer.go API
// exactly and uses the shared client.Event[T] type and event-name constants
// from tailkit/client, so Tailflow's SSE wire format is identical to
// tailkitd's. This makes a future "transparent passthrough" mode (where
// Tailflow forwards tailkitd events unchanged) mechanical, and means the
// browser-side decoder is the same code whether events originate from
// tailkitd or from Tailflow's own hub.
//
// Design refs: §F8 (SSE delivery), §16.6 (Hub contract). The original §F10
// rule said hub must not import tailkit/client; that rule is relaxed for hub
// specifically (see .golangci.yml) because sharing the Event[T] type is the
// cleanest way to keep the wire format in lock-step with tailkitd.
package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/wf-pro-dev/tailkit/client"
)

// ErrClientGone is returned (or wrapped) by Send when the browser has closed
// the connection. SSE handlers treat this as a clean end-of-stream.
var ErrClientGone = errors.New("hub: sse client gone")

// Event is the shared typed SSE event envelope, aliased from tailkit/client
// so Tailflow and tailkitd use the exact same type.
type Event[T any] = client.Event[T]

// Event-name constants for Tailflow-originated events. The names that overlap
// with tailkitd (port.bound, port.released, error) are re-exported from
// tailkit/client; the Tailflow-only names (host.updated, host.removed,
// metrics.sample, docker.container.changed, docker.inventory.replaced,
// resync) are defined here.
const (
	// EventError mirrors tailkit/client.EventError so callers can use either.
	EventError = client.EventError

	// Re-export the tailkitd-shared event names so hub callers don't have to
	// import tailkit/client just for the constants.
	EventPortBound     = client.EventPortBound
	EventPortReleased  = client.EventPortReleased
	EventDockerAll     = client.EventDockerAll
	EventLogLine       = client.EventLogLine
	EventStatsSnapshot = client.EventStatsSnapshot

	// Tailflow-only event names (no upstream equivalent).
	EventHostUpdated            = "host.updated"
	EventHostRemoved            = "host.removed"
	EventMetricsSample          = "metrics.sample"
	EventDockerContainerChanged = "docker.container.changed"
	EventDockerInventoryReplaced = "docker.inventory.replaced"
	EventResync                 = "resync"
)

// Writer emits server-sent events on a single HTTP response stream. Mirrors
// tailkitd's internal/sse.Writer so the wire format and API are identical.
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
	seq     atomic.Int64
	done    <-chan struct{}
	mu      sync.Mutex
}

// NewWriter sets SSE headers on w and returns a Writer. The caller is expected
// to have already verified that the request is authorised.
//
// Unlike tailkitd's version, we do NOT set Access-Control-Allow-Origin: *
// (design §9: "no CORS by default"). Tailflow's auth middleware sets any
// required headers.
func NewWriter(w http.ResponseWriter, r *http.Request) (*Writer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("hub: response writer does not support flushing")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &Writer{
		w:       w,
		flusher: flusher,
		done:    r.Context().Done(),
	}, nil
}

// Write emits a typed SSE event. The event's ID field is ignored; the writer
// assigns a new monotonic ID from its own counter. Use WriteWithID to forward
// an event with a pre-assigned ID (e.g. from the Hub's replay buffer).
func Write[T any](sw *Writer, event Event[T]) error {
	return sw.write(event.Name, event.Data, 0)
}

// WriteWithID emits a typed SSE event with a specific ID (used when replaying
// events from the Hub's buffer so the browser sees the original sequence
// number and can track Last-Event-ID correctly).
func WriteWithID[T any](sw *Writer, event Event[T]) error {
	return sw.write(event.Name, event.Data, event.ID)
}

// Send emits an untyped SSE event. Convenience wrapper around Write.
func (sw *Writer) Send(name string, data any) error {
	return sw.write(name, data, 0)
}

// SendWithID emits an untyped SSE event with a specific ID.
func (sw *Writer) SendWithID(name string, id int64, data any) error {
	return sw.write(name, data, id)
}

func (sw *Writer) write(name string, data any, id int64) error {
	select {
	case <-sw.done:
		return ErrClientGone
	default:
	}

	var raw []byte
	switch v := data.(type) {
	case nil:
		raw = []byte("{}")
	case []byte:
		raw = v
	case json.RawMessage:
		raw = v
	case string:
		raw = []byte(v)
	default:
		var err error
		raw, err = json.Marshal(data)
		if err != nil {
			return fmt.Errorf("hub: marshal sse data: %w", err)
		}
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	select {
	case <-sw.done:
		return ErrClientGone
	default:
	}

	if id == 0 {
		id = sw.seq.Add(1)
	} else if id > sw.seq.Load() {
		// Prime the counter so subsequent auto-numbered events continue past
		// the replayed ID (matches tailkitd's SetSequence semantics).
		sw.seq.Store(id)
	}

	if _, err := fmt.Fprintf(sw.w, "event: %s\nid: %d\ndata: %s\n\n", name, id, raw); err != nil {
		return fmt.Errorf("%w: write failed: %v", ErrClientGone, err)
	}
	sw.flusher.Flush()
	return nil
}

// Heartbeat sends a comment line that keeps the connection alive through
// idle-timeout proxies. The format matches tailkitd's `: heartbeat\n\n`.
func (sw *Writer) Heartbeat() error {
	select {
	case <-sw.done:
		return ErrClientGone
	default:
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	select {
	case <-sw.done:
		return ErrClientGone
	default:
	}

	if _, err := sw.w.Write([]byte(": heartbeat\n\n")); err != nil {
		return fmt.Errorf("%w: heartbeat write failed: %v", ErrClientGone, err)
	}
	sw.flusher.Flush()
	return nil
}

// Error emits an `error` event with the given message. Used by handlers when
// an upstream failure happens mid-stream.
func (sw *Writer) Error(msg string) {
	_ = sw.Send(EventError, map[string]string{"error": msg})
}

// SetSequence primes the writer's sequence counter so subsequent events
// continue from a known ID (used on reconnect with Last-Event-ID).
func (sw *Writer) SetSequence(seq int64) {
	sw.seq.Store(seq)
}

// Closed returns whether the underlying request context has been cancelled.
func (sw *Writer) Closed() bool {
	select {
	case <-sw.done:
		return true
	default:
		return false
	}
}

// ResumeFrom parses the Last-Event-ID header from r. Returns 0 if absent or
// unparseable. Mirrors tailkitd's sse.ResumeFrom so the API handler can use
// either.
func ResumeFrom(r *http.Request) int64 {
	value := r.Header.Get("Last-Event-ID")
	if value == "" {
		return 0
	}
	seq, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return seq
}
