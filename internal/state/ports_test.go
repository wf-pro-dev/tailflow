package state

import (
	"sort"
	"testing"
)

// F5-01 (golden): Replaying a recorded sequence of snapshot/bound/released
// events yields the same port set as the final snapshot.
//
// This is the design §F5 acceptance test. We build a port set by applying a
// sequence of snapshot → bound → bound → released → snapshot deltas, then
// compare against the expected final set.
func TestF5_01_PortReplayGolden(t *testing.T) {
	store := New(720)
	store.UpsertHost(Host{Name: "vps-1", Online: true, Status: StatusHealthy})

	// Step 1: initial snapshot with 3 ports.
	store.ApplyPortUpdate("vps-1", "snapshot", Port{}, []Port{
		{Addr: "0.0.0.0", Port: 22, Proto: "tcp", PID: 1000, Process: "sshd"},
		{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 2000, Process: "nginx"},
		{Addr: "127.0.0.1", Port: 5432, Proto: "tcp", PID: 3000, Process: "postgres"},
	})

	// Step 2: a new port is bound.
	store.ApplyPortUpdate("vps-1", "bound", Port{Addr: "0.0.0.0", Port: 443, Proto: "tcp", PID: 2000, Process: "nginx"}, nil)

	// Step 3: duplicate bound for an existing port (idempotent — F5-02).
	store.ApplyPortUpdate("vps-1", "bound", Port{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 2000, Process: "nginx"}, nil)

	// Step 4: port 5432 is released.
	store.ApplyPortUpdate("vps-1", "released", Port{Addr: "127.0.0.1", Port: 5432, Proto: "tcp", PID: 3000, Process: "postgres"}, nil)

	// Expected final set: 22, 80, 443 (5432 was released).
	got := store.Ports("vps-1")
	want := []Port{
		{Addr: "0.0.0.0", Port: 22, Proto: "tcp", PID: 1000, Process: "sshd"},
		{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 2000, Process: "nginx"},
		{Addr: "0.0.0.0", Port: 443, Proto: "tcp", PID: 2000, Process: "nginx"},
	}
	if !portsEqual(got, want) {
		t.Errorf("after replay:\n  got  = %v\n  want = %v", got, want)
	}

	// Now apply a fresh snapshot and verify it matches the replayed set.
	store.ApplyPortUpdate("vps-1", "snapshot", Port{}, []Port{
		{Addr: "0.0.0.0", Port: 22, Proto: "tcp", PID: 1000, Process: "sshd"},
		{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 2000, Process: "nginx"},
		{Addr: "0.0.0.0", Port: 443, Proto: "tcp", PID: 2000, Process: "nginx"},
	})
	got2 := store.Ports("vps-1")
	if !portsEqual(got2, want) {
		t.Errorf("final snapshot does not match replayed set:\n  got  = %v\n  want = %v", got2, want)
	}
}

// F5-02 (unit): Duplicate `bound` for the same tuple is idempotent — it does
// not create a second entry or change the existing one.
func TestF5_02_DuplicateBoundIsIdempotent(t *testing.T) {
	store := New(720)
	store.UpsertHost(Host{Name: "vps-1", Online: true, Status: StatusHealthy})

	p := Port{Addr: "0.0.0.0", Port: 443, Proto: "tcp", PID: 1234, Process: "nginx"}
	store.ApplyPortUpdate("vps-1", "bound", p, nil)
	store.ApplyPortUpdate("vps-1", "bound", p, nil)
	store.ApplyPortUpdate("vps-1", "bound", p, nil)

	got := store.Ports("vps-1")
	if len(got) != 1 {
		t.Errorf("after 3 duplicate bounds: len(Ports) = %d, want 1", len(got))
	}
	if got[0] != p {
		t.Errorf("port = %v, want %v (unchanged by duplicate bounds)", got[0], p)
	}
}

// TestPortSnapshotReplaces verifies that a snapshot event replaces the entire
// port set (not merges into it).
func TestPortSnapshotReplaces(t *testing.T) {
	store := New(720)
	store.UpsertHost(Host{Name: "vps-1", Online: true, Status: StatusHealthy})

	store.ApplyPortUpdate("vps-1", "snapshot", Port{}, []Port{
		{Addr: "0.0.0.0", Port: 22, Proto: "tcp", PID: 1000},
		{Addr: "0.0.0.0", Port: 80, Proto: "tcp", PID: 2000},
	})
	if len(store.Ports("vps-1")) != 2 {
		t.Fatalf("after first snapshot: %d ports, want 2", len(store.Ports("vps-1")))
	}

	// Second snapshot with a completely different set.
	store.ApplyPortUpdate("vps-1", "snapshot", Port{}, []Port{
		{Addr: "0.0.0.0", Port: 443, Proto: "tcp", PID: 3000},
	})
	got := store.Ports("vps-1")
	if len(got) != 1 {
		t.Fatalf("after second snapshot: %d ports, want 1 (snapshot replaces)", len(got))
	}
	if got[0].Port != 443 {
		t.Errorf("port = %d, want 443", got[0].Port)
	}
}

// TestPortReleasedNonexistent verifies that releasing a port that doesn't
// exist is a safe no-op.
func TestPortReleasedNonexistent(t *testing.T) {
	store := New(720)
	store.UpsertHost(Host{Name: "vps-1", Online: true, Status: StatusHealthy})

	store.ApplyPortUpdate("vps-1", "snapshot", Port{}, []Port{
		{Addr: "0.0.0.0", Port: 22, Proto: "tcp", PID: 1000},
	})

	// Release a port that was never bound.
	store.ApplyPortUpdate("vps-1", "released", Port{Addr: "0.0.0.0", Port: 9999, Proto: "tcp", PID: 9999}, nil)

	got := store.Ports("vps-1")
	if len(got) != 1 {
		t.Errorf("after releasing nonexistent: %d ports, want 1 (no-op)", len(got))
	}
}

// --- helpers ---------------------------------------------------------------

func portsEqual(a, b []Port) bool {
	if len(a) != len(b) {
		return false
	}
	// Sort both by (port, proto, addr) for stable comparison.
	sortA := make([]Port, len(a))
	copy(sortA, a)
	sortB := make([]Port, len(b))
	copy(sortB, b)
	sort.Slice(sortA, func(i, j int) bool {
		if sortA[i].Port != sortA[j].Port {
			return sortA[i].Port < sortA[j].Port
		}
		return sortA[i].Proto < sortA[j].Proto
	})
	sort.Slice(sortB, func(i, j int) bool {
		if sortB[i].Port != sortB[j].Port {
			return sortB[i].Port < sortB[j].Port
		}
		return sortB[i].Proto < sortB[j].Proto
	})
	for i := range sortA {
		if sortA[i] != sortB[i] {
			return false
		}
	}
	return true
}
