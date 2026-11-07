package state

import (
	"testing"
	"time"
)

func TestDeriveStatusOffline(t *testing.T) {
	got := DeriveStatus(false, false, nil, time.Now(), 2*time.Second, 3, 0, 3)
	if got != StatusOffline {
		t.Errorf("DeriveStatus(offline) = %s, want offline", got)
	}
}

func TestDeriveStatusNoAgent(t *testing.T) {
	// Online but no agent.
	got := DeriveStatus(true, false, nil, time.Now(), 2*time.Second, 3, 0, 3)
	if got != StatusNoAgent {
		t.Errorf("DeriveStatus(online, no agent) = %s, want no-agent", got)
	}
}

func TestDeriveStatusNoAgentNeverSampled(t *testing.T) {
	// Agent reachable but no sample yet — treat as no-agent (will flip to
	// healthy on first sample).
	got := DeriveStatus(true, true, nil, time.Now(), 2*time.Second, 3, 0, 3)
	if got != StatusNoAgent {
		t.Errorf("DeriveStatus(reachable, never sampled) = %s, want no-agent", got)
	}
}

func TestDeriveStatusStale(t *testing.T) {
	// Last sample 10s ago; stale factor 3 × 2s = 6s threshold.
	last := time.Now().Add(-10 * time.Second)
	got := DeriveStatus(true, true, &last, time.Now(), 2*time.Second, 3, 0, 3)
	if got != StatusStale {
		t.Errorf("DeriveStatus(stale) = %s, want stale", got)
	}
}

func TestDeriveStatusDegraded(t *testing.T) {
	// Recent sample but 3 consecutive failures.
	last := time.Now()
	got := DeriveStatus(true, true, &last, time.Now(), 2*time.Second, 3, 3, 3)
	if got != StatusDegraded {
		t.Errorf("DeriveStatus(degraded) = %s, want degraded", got)
	}
}

func TestDeriveStatusHealthy(t *testing.T) {
	last := time.Now()
	got := DeriveStatus(true, true, &last, time.Now(), 2*time.Second, 3, 0, 3)
	if got != StatusHealthy {
		t.Errorf("DeriveStatus(healthy) = %s, want healthy", got)
	}
}

func TestDeriveStatusDegradedBeatsStale(t *testing.T) {
	// Both stale AND 3 failures: which wins? Design §5.2 lists stale before
	// degraded in the table, so stale wins. This matches the order in
	// DeriveStatus above.
	last := time.Now().Add(-10 * time.Second)
	got := DeriveStatus(true, true, &last, time.Now(), 2*time.Second, 3, 3, 3)
	if got != StatusStale {
		t.Errorf("DeriveStatus(stale+degraded) = %s, want stale (table order)", got)
	}
}

func TestIsReachableAgent(t *testing.T) {
	no := false
	yes := true
	cases := []struct {
		online    bool
		peerOnline *bool
		want      bool
	}{
		{false, nil, false},
		{false, &yes, false},
		{true, nil, false},
		{true, &no, false},
		{true, &yes, true},
	}
	for _, c := range cases {
		got := IsReachableAgent(c.online, c.peerOnline)
		if got != c.want {
			t.Errorf("IsReachableAgent(%v, %v) = %v, want %v", c.online, c.peerOnline, got, c.want)
		}
	}
}
