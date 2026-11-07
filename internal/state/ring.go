// Package state holds Tailflow's in-memory store: hosts, ring buffers of
// metric samples, derived status, and the per-host concurrency model.
//
// This package MUST NOT import tailkit or tailkit/client (design §F10,
// enforced by .golangci.yml depguard). The collector maps tailkit types
// onto the types declared here at the boundary.
//
// Design refs: §5 (data model), §5.3 (concurrency), §16.2 (state types),
// §16.5 (aggregator).
package state

import "sync"

// Ring is a fixed-capacity circular buffer. Push is O(1) and allocates only
// when the buffer is empty (the first Push allocates the backing slice). Once
// full, Push overwrites the oldest entry. Snapshot is O(n) and returns a
// fresh copy so callers can hand the slice to JSON encoders without holding
// the lock.
//
// Design §16.2 contract:
//
//	type Ring[T any] struct { /* fixed capacity, one mutex */ }
//	func NewRing[T any](capacity int) *Ring[T]
//	func (r *Ring[T]) Push(v T)
//	func (r *Ring[T]) Len() int
//	func (r *Ring[T]) Snapshot() []T // oldest to newest; a copy
//
// Property test (F2-04): push N > capacity items → Len() == capacity, order
// preserved, oldest evicted.
type Ring[T any] struct {
	mu       sync.RWMutex
	buf      []T
	head     int // index of the oldest entry
	len      int
	capacity int
}

// NewRing returns a Ring with the given fixed capacity. Capacity must be > 0;
// callers should derive it from TAILFLOW_HISTORY_WINDOW / TAILFLOW_HISTORY_STEP
// (default 1h / 5s = 720).
func NewRing[T any](capacity int) *Ring[T] {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring[T]{
		buf:      make([]T, 0, capacity),
		capacity: capacity,
	}
}

// Push appends v, evicting the oldest entry if the buffer is full.
func (r *Ring[T]) Push(v T) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.len < r.capacity {
		// Buffer not yet full: append.
		r.buf = append(r.buf, v)
		r.len++
		return
	}

	// Buffer full: overwrite the oldest entry, advance head.
	r.buf[r.head] = v
	r.head = (r.head + 1) % r.capacity
}

// Len returns the number of items currently in the ring (<= capacity).
func (r *Ring[T]) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.len
}

// Capacity returns the fixed capacity the ring was constructed with.
func (r *Ring[T]) Capacity() int { return r.capacity }

// Snapshot returns a copy of the ring's contents, oldest to newest. The
// caller owns the returned slice and may mutate or hand it to a JSON encoder
// without holding the lock.
func (r *Ring[T]) Snapshot() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.len == 0 {
		return nil
	}
	out := make([]T, r.len)
	if r.len < r.capacity {
		// Buffer not yet full: buf is [oldest, ..., newest], head == 0.
		copy(out, r.buf[:r.len])
		return out
	}
	// Buffer full: head points at the oldest; wrap around.
	n := copy(out, r.buf[r.head:])
	copy(out[n:], r.buf[:r.head])
	return out
}

// Latest returns the most recently pushed item and ok=true, or ok=false if
// the ring is empty. Cheaper than Snapshot when only the newest value is
// needed (e.g. for /metrics/latest).
func (r *Ring[T]) Latest() (v T, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.len == 0 {
		return v, false
	}
	if r.len < r.capacity {
		return r.buf[r.len-1], true
	}
	// head points at the oldest; newest is at head-1 (mod capacity).
	idx := (r.head + r.len - 1) % r.capacity
	return r.buf[idx], true
}
