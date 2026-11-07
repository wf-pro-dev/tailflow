package hub

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestHubPublishAssignsMonotonicIDs(t *testing.T) {
	h := NewHub()
	s, err := h.Subscribe([]string{"hosts"}, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer s.Close()

	h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: "vps-1"})
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: "vps-2"})
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostRemoved, Data: "vps-3"})

	var got []uint64
	for i := 0; i < 3; i++ {
		select {
		case e := <-s.C():
			got = append(got, e.ID)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("got IDs %v, want [1 2 3]", got)
	}
}

func TestHubTopicFiltering(t *testing.T) {
	h := NewHub()
	s, err := h.Subscribe([]string{"metrics:vps-1"}, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer s.Close()

	// These should be delivered.
	h.Publish(HubEvent{Topic: "metrics:vps-1", Name: EventMetricsSample, Data: "a"})
	h.Publish(HubEvent{Topic: "metrics:vps-1", Name: EventMetricsSample, Data: "b"})
	// These should NOT be delivered.
	h.Publish(HubEvent{Topic: "metrics:vps-2", Name: EventMetricsSample, Data: "c"})
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: "d"})

	got := drainN(t, s.C(), 2, time.Second)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	if got[0].Data != "a" || got[1].Data != "b" {
		t.Errorf("got data %v %v, want a b", got[0].Data, got[1].Data)
	}

	// No more events should arrive.
	select {
	case e := <-s.C():
		t.Errorf("unexpected event: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHubReplayOnSubscribe(t *testing.T) {
	h := NewHub()
	// Publish 3 events BEFORE subscribing.
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: "vps-1"})
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: "vps-2"})
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostRemoved, Data: "vps-3"})

	// Subscribe with lastID=1; should get events 2 and 3 replayed.
	s, err := h.Subscribe([]string{"hosts"}, 1)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer s.Close()

	got := drainN(t, s.C(), 2, time.Second)
	if len(got) != 2 {
		t.Fatalf("got %d replayed events, want 2", len(got))
	}
	if got[0].ID != 2 || got[1].ID != 3 {
		t.Errorf("replayed IDs = %v, want [2 3]", []uint64{got[0].ID, got[1].ID})
	}
}

func TestHubReplayGapTooOld(t *testing.T) {
	// Use a tiny replay ring (size 3) so we can evict events easily.
	h := NewHubWithSizes(3, 16)
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: "1"})
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: "2"})
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: "3"})
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: "4"}) // evicts ID 1
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: "5"}) // evicts ID 2

	// lastID=1 is older than the oldest event in the ring (which is now ID 3).
	_, err := h.Subscribe([]string{"hosts"}, 1)
	if !errors.Is(err, ErrGapTooOld) {
		t.Fatalf("Subscribe with stale lastID: err = %v, want ErrGapTooOld", err)
	}
}

func TestHubReplayFiltersByTopic(t *testing.T) {
	h := NewHub()
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: "h1"})
	h.Publish(HubEvent{Topic: "metrics:vps-1", Name: EventMetricsSample, Data: "m1"})
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: "h2"})

	// Subscribe to metrics:vps-1 only; should get just m1 replayed.
	s, err := h.Subscribe([]string{"metrics:vps-1"}, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer s.Close()

	got := drainN(t, s.C(), 1, time.Second)
	if len(got) != 1 || got[0].Data != "m1" {
		t.Errorf("replay = %v, want [m1]", got)
	}
}

func TestHubBackpressureCoalescesMetricsSamples(t *testing.T) {
	// Small sub buffer so we can fill it easily.
	h := NewHubWithSizes(100, 2)
	s, err := h.Subscribe([]string{"metrics:vps-1"}, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer s.Close()

	// Publish 10 metrics.sample events for the same host faster than the
	// subscriber can read them. The hub should coalesce (replace newest)
	// rather than close the sub.
	for i := 0; i < 10; i++ {
		h.Publish(HubEvent{Topic: "metrics:vps-1", Name: EventMetricsSample, Data: i})
	}

	// Drain whatever we got. We should NOT have been closed (no slow-consumer).
	select {
	case <-s.Closed():
		// Check it wasn't due to slow-consumer.
		if s.Reason() == slowConsumerReason {
			t.Fatalf("sub closed as slow-consumer; metrics.sample should coalesce")
		}
	default:
	}

	// Drain remaining events.
	got := drainAll(t, s.C(), 500*time.Millisecond)
	if len(got) == 0 {
		t.Fatal("expected at least one coalesced sample, got 0")
	}
	// The last coalesced sample should have Data == 9 (the newest).
	last := got[len(got)-1]
	if last.Data != 9 {
		t.Errorf("last coalesced sample = %v, want 9", last.Data)
	}
}

func TestHubBackpressureClosesOnStructuralEventFull(t *testing.T) {
	h := NewHubWithSizes(100, 2)
	s, err := h.Subscribe([]string{"hosts"}, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer s.Close()

	// Fill the buffer with structural events without reading.
	for i := 0; i < 5; i++ {
		h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: i})
	}

	// The sub should be closed with reason "slow-consumer".
	select {
	case <-s.Closed():
	case <-time.After(time.Second):
		t.Fatal("expected sub to be closed due to slow-consumer")
	}
	if got := s.Reason(); got != slowConsumerReason {
		t.Errorf("Reason = %q, want %q", got, slowConsumerReason)
	}
}

func TestHubCloseUnsubscribes(t *testing.T) {
	h := NewHub()
	s, err := h.Subscribe([]string{"hosts"}, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	s.Close()

	// Publish after close; should not panic and should not deliver.
	h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: "x"})

	// Channel should be closed.
	select {
	case _, ok := <-s.C():
		if ok {
			t.Error("channel should be closed after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}

	// Verify the sub is removed from the hub.
	h.mu.RLock()
	count := len(h.subs)
	h.mu.RUnlock()
	if count != 0 {
		t.Errorf("hub has %d subs after Close, want 0", count)
	}
}

func TestHubConcurrentPublish(t *testing.T) {
	// Race-detector bait: many goroutines publishing to many subscribers.
	h := NewHub()
	subs := make([]*Sub, 10)
	for i := range subs {
		s, err := h.Subscribe([]string{"hosts"}, 0)
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		subs[i] = s
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				h.Publish(HubEvent{Topic: "hosts", Name: EventHostUpdated, Data: n*100 + j})
			}
		}(i)
	}
	wg.Wait()

	// Close all subs and verify each got some events (or was closed cleanly).
	for _, s := range subs {
		s.Close()
	}
}

// --- helpers ---

func drainN(t *testing.T, ch <-chan HubEvent, n int, timeout time.Duration) []HubEvent {
	t.Helper()
	var got []HubEvent
	deadline := time.After(timeout)
	for len(got) < n {
		select {
		case e, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, e)
		case <-deadline:
			return got
		}
	}
	return got
}

func drainAll(t *testing.T, ch <-chan HubEvent, timeout time.Duration) []HubEvent {
	t.Helper()
	var got []HubEvent
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, e)
		case <-time.After(timeout):
			return got
		}
	}
}
