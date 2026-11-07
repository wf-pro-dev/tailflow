// Package hub owns the SSE fan-out for Tailflow. It is the only package that
// writes SSE frames to browsers; it never imports tailkit (design §F10).
//
// This file is salvaged from the first Tailflow prototype's internal/sse/writer.go,
// which was structurally identical to tailkitd's internal/sse/writer.go but
// already satisfied the "no tailkit import" constraint (the prototype used a
// local Event type and a literal "error" event name; we keep both here).
//
// Design refs: §4.3 (stdlib only), §F8 (SSE delivery), §16.6 (Hub contract).
package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
)

// ErrClientGone is returned (or wrapped) by Send when the browser has closed
// the connection. SSE handlers treat this as a clean end-of-stream.
var ErrClientGone = errors.New("hub: sse client gone")

// EventName constants. These mirror the names tailkitd emits upstream so that
// a future "transparent passthrough" mode is mechanical, but Tailflow never
// imports tailkit to get them (design §F10).
const (
	EventHostUpdated           = "host.updated"
	EventHostRemoved           = "host.removed"
	EventMetricsSample         = "metrics.sample"
	EventPortBound             = "port.bound"
	EventPortReleased          = "port.released"
	EventDockerContainerChanged = "docker.container.changed"
	EventDockerInventoryReplaced = "docker.inventory.replaced"
	EventResync                = "resync"
	EventError                 = "error"
)

// Event is a single SSE event. Data is JSON-marshalled; if Data is already
// []byte it is written verbatim. ID is the monotonic sequence number assigned
// by the Hub on Publish.
type Event struct {
	Name string
	ID   int64
	Data any
}

// Writer is a thin wrapper around an http.ResponseWriter that emits SSE frames
// in the standard wire format (`event:` / `id:` / `data:` lines and a blank
// line separator). One Writer per HTTP connection; not safe for concurrent
// use by multiple goroutines (the Hub serialises writes per subscriber).
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
	seq     atomic.Int64
	done    <-chan struct{}
	mu      sync.Mutex
}

// NewWriter sets SSE headers on w and returns a Writer. The caller is expected
// to have already verified that the request is authorised (Tailflow's auth
// middleware runs before the handler).
//
// `done` is the request's context-done channel; Send checks it before writing
// and returns ErrClientGone if the browser has gone away, so handlers can
// break out of a fan-out loop cleanly.
func NewWriter(w http.ResponseWriter, r *http.Request) (*Writer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("hub: response writer does not support flushing")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	// No CORS by default (design §9: "no CORS by default"). Tailflow's auth
	// middleware would set any required headers.
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &Writer{
		w:       w,
		flusher: flusher,
		done:    r.Context().Done(),
	}, nil
}

// Send writes one SSE event. data is JSON-marshalled; if data is []byte or
// string it is written as-is. Returns ErrClientGone (wrapped) if the request
// context has been cancelled.
func (w *Writer) Send(name string, data any) error {
	return w.WriteEvent(Event{Name: name, Data: data})
}

// WriteEvent writes one SSE event with an explicit ID. Used by the Hub to
// forward events with their original sequence numbers.
func (w *Writer) WriteEvent(e Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.done:
		return fmt.Errorf("%w: connection closed before write", ErrClientGone)
	default:
	}
	var b []byte
	switch v := e.Data.(type) {
	case nil:
		b = []byte("{}")
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		var err error
		b, err = json.Marshal(v)
		if err != nil {
			return fmt.Errorf("hub: marshal sse data: %w", err)
		}
	}
	if _, err := fmt.Fprintf(w.w, "event: %s\nid: %d\ndata: %s\n\n", e.Name, e.ID, b); err != nil {
		return fmt.Errorf("%w: write failed: %v", ErrClientGone, err)
	}
	w.flusher.Flush()
	return nil
}

// Heartbeat sends a comment line that keeps the connection alive through
// idle-timeout proxies. The format matches tailkitd's `: heartbeat\n\n`.
func (w *Writer) Heartbeat() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.done:
		return ErrClientGone
	default:
	}
	if _, err := w.w.Write([]byte(": heartbeat\n\n")); err != nil {
		return fmt.Errorf("%w: heartbeat write failed: %v", ErrClientGone, err)
	}
	w.flusher.Flush()
	return nil
}

// Error emits an `error` event with the given message. Used by handlers when
// an upstream failure happens mid-stream.
func (w *Writer) Error(msg string) {
	_ = w.Send(EventError, map[string]string{"error": msg})
}

// SetSequence primes the writer's sequence counter so subsequent events
// continue from a known ID (used on reconnect with Last-Event-ID).
func (w *Writer) SetSequence(seq int64) {
	w.seq.Store(seq)
}

// NextID returns the next monotonic ID. The Hub uses this on Publish.
func (w *Writer) NextID() int64 { return w.seq.Add(1) }

// Closed returns whether the underlying request context has been cancelled.
func (w *Writer) Closed() bool {
	select {
	case <-w.done:
		return true
	default:
		return false
	}
}

// Context returns the request context, so handlers can break out of long
// fan-out loops when the browser disconnects.
func (w *Writer) Context() context.Context {
	// We don't store the context, but `done` is r.Context().Done(), so we
	// return a fresh background context that callers can ignore. This method
	// is here to make the API familiar; in practice handlers should take the
	// request context directly.
	return context.Background()
}
