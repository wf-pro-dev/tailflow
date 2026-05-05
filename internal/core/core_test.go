package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

func TestNewID(t *testing.T) {
	first := NewID()
	second := NewID()

	if first == second {
		t.Fatal("expected unique IDs")
	}
	if _, err := ulid.Parse(first); err != nil {
		t.Fatalf("first ID is not a valid ULID: %v", err)
	}
	if _, err := ulid.Parse(second); err != nil {
		t.Fatalf("second ID is not a valid ULID: %v", err)
	}
}

func TestMust(t *testing.T) {
	t.Run("returns value when error is nil", func(t *testing.T) {
		got := Must(42, nil)
		if got != 42 {
			t.Fatalf("Must returned %d, want 42", got)
		}
	})

	t.Run("panics when error is present", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()

		_ = Must(0, errors.New("boom"))
	})
}

func TestMergeErrors(t *testing.T) {
	tests := []struct {
		name string
		errs map[NodeName]error
		want string
	}{
		{
			name: "nil when map empty",
			errs: map[NodeName]error{},
		},
		{
			name: "nil when all errors nil",
			errs: map[NodeName]error{
				"node-a": nil,
				"node-b": nil,
			},
		},
		{
			name: "sorted deterministic output",
			errs: map[NodeName]error{
				"node-b": errors.New("second"),
				"node-a": errors.New("first"),
			},
			want: "merge errors: node-a: first; node-b: second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := MergeErrors(tt.errs)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("MergeErrors returned %v, want nil", err)
				}
				return
			}

			if err == nil {
				t.Fatal("MergeErrors returned nil, want error")
			}
			if err.Error() != tt.want {
				t.Fatalf("MergeErrors returned %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestTimestampJSON(t *testing.T) {
	t.Run("marshal uses RFC3339Nano in UTC", func(t *testing.T) {
		ts := NewTimestamp(time.Date(2026, 4, 17, 10, 11, 12, 123456789, time.FixedZone("X", 2*3600)))

		got, err := ts.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON returned error: %v", err)
		}

		want := `"2026-04-17T08:11:12.123456789Z"`
		if string(got) != want {
			t.Fatalf("MarshalJSON returned %s, want %s", got, want)
		}
	})

	t.Run("unmarshal accepts null", func(t *testing.T) {
		var ts Timestamp
		if err := ts.UnmarshalJSON([]byte("null")); err != nil {
			t.Fatalf("UnmarshalJSON returned error: %v", err)
		}
		if !ts.IsZero() {
			t.Fatal("expected zero timestamp")
		}
	})

	t.Run("unmarshal parses RFC3339Nano", func(t *testing.T) {
		var ts Timestamp
		err := ts.UnmarshalJSON([]byte(`"2026-04-17T08:11:12.123456789Z"`))
		if err != nil {
			t.Fatalf("UnmarshalJSON returned error: %v", err)
		}

		got := ts.Time()
		if got.Format(time.RFC3339Nano) != "2026-04-17T08:11:12.123456789Z" {
			t.Fatalf("unexpected parsed time: %s", got.Format(time.RFC3339Nano))
		}
	})
}

func TestEventBusPublishAndSubscribe(t *testing.T) {
	bus := NewEventBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := bus.Subscribe(ctx, TopicNode)
	bus.Publish(TopicNode, "payload")

	select {
	case got := <-ch:
		if got != "payload" {
			t.Fatalf("Publish delivered %v, want payload", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published event")
	}
}

func TestEventBusSubscribeClosesChannelOnCancel(t *testing.T) {
	bus := NewEventBus()
	ctx, cancel := context.WithCancel(context.Background())
	ch := bus.Subscribe(ctx, TopicNode)

	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}

func TestBroadcastEvent(t *testing.T) {
	bus := NewEventBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := bus.Subscribe(ctx, TopicNode)
	BroadcastEvent(bus, EventNodeStatusChanged.String(), map[string]string{"name": "node-a"})

	select {
	case raw := <-ch:
		event, ok := raw.(Event[map[string]string])
		if !ok {
			t.Fatalf("BroadcastEvent delivered %T, want Event[map[string]string]", raw)
		}
		if event.Name != EventNodeStatusChanged.String() {
			t.Fatalf("event name = %q, want %s", event.Name, EventNodeStatusChanged.String())
		}
		if event.Data["name"] != "node-a" {
			t.Fatalf("event data = %#v, want node-a", event.Data)
		}
		if event.TS.IsZero() {
			t.Fatal("expected timestamp to be set")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast event")
	}
}
