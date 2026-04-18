package core

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ID is a ULID-based identifier used on every stored entity.
type ID = string

// NodeName is the stable key for a node across all entities.
type NodeName = string

// Timestamp marshals consistently as RFC3339Nano in UTC.
type Timestamp time.Time

// Result carries a value and an error together for per-node fan-out operations.
type Result[T any] struct {
	Node  NodeName
	Value T
	Err   error
}

// Event is the envelope for all SSE events emitted by the API.
type Event[T any] struct {
	Name string    `json:"event"`
	TS   Timestamp `json:"ts"`
	Data T         `json:"data"`
}

// Watcher is implemented by any component that produces a stream of events.
type Watcher[T any] interface {
	Watch(ctx context.Context) (<-chan T, error)
}

// Repository is the base interface every store entity implements.
type Repository[T any] interface {
	Get(ctx context.Context, id ID) (T, error)
	List(ctx context.Context, filter Filter) ([]T, error)
	Save(ctx context.Context, entity T) error
	Delete(ctx context.Context, id ID) error
}

// Filter is a generic query parameter bag passed to List().
type Filter struct {
	NodeName NodeName
	Since    *Timestamp
	Limit    int
}

// NowTimestamp returns the current UTC time as a Timestamp.
func NowTimestamp() Timestamp {
	return NewTimestamp(time.Now().UTC())
}

// NewTimestamp normalizes a time value to UTC.
func NewTimestamp(t time.Time) Timestamp {
	return Timestamp(t.UTC())
}

// Time returns the underlying time.Time.
func (ts Timestamp) Time() time.Time {
	return time.Time(ts)
}

// IsZero reports whether the timestamp is the zero time.
func (ts Timestamp) IsZero() bool {
	return ts.Time().IsZero()
}

// String returns the timestamp in RFC3339Nano format.
func (ts Timestamp) String() string {
	if ts.IsZero() {
		return ""
	}
	return ts.Time().Format(time.RFC3339Nano)
}

// MarshalJSON serializes timestamps in RFC3339Nano format.
func (ts Timestamp) MarshalJSON() ([]byte, error) {
	if ts.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(ts.Time().UTC().Format(time.RFC3339Nano))
}

// UnmarshalJSON parses RFC3339Nano timestamps and accepts null.
func (ts *Timestamp) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*ts = Timestamp{}
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("unmarshal timestamp: %w", err)
	}

	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return fmt.Errorf("parse timestamp %q: %w", value, err)
	}

	*ts = NewTimestamp(parsed)
	return nil
}
