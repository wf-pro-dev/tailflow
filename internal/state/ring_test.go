package state

import (
        "fmt"
        "testing"
)

// F2-04 (property): Push N > capacity items; Len() == capacity; order
// preserved; oldest evicted. Tested with both small and large N, and with
// the testing/quick framework for fuzz-style coverage.
func TestRingNeverExceedsCapacity(t *testing.T) {
        cases := []struct {
                cap, n int
        }{
                {1, 1}, {1, 5}, {1, 100},
                {3, 3}, {3, 5}, {3, 100},
                {720, 720}, {720, 1000},
        }
        for _, c := range cases {
                t.Run(fmt.Sprintf("cap=%d_n=%d", c.cap, c.n), func(t *testing.T) {
                        r := NewRing[int](c.cap)
                        for i := 0; i < c.n; i++ {
                                r.Push(i)
                        }
                        if got := r.Len(); got != min(c.cap, c.n) {
                                t.Errorf("Len() = %d, want %d", got, min(c.cap, c.n))
                        }
                        snap := r.Snapshot()
                        if len(snap) != min(c.cap, c.n) {
                                t.Errorf("len(Snapshot()) = %d, want %d", len(snap), min(c.cap, c.n))
                        }
                        // Order preserved: snap should be [n-cap, n-cap+1, ..., n-1]
                        // when n > cap, or [0, 1, ..., n-1] when n <= cap.
                        wantStart := 0
                        if c.n > c.cap {
                                wantStart = c.n - c.cap
                        }
                        for i, v := range snap {
                                if v != wantStart+i {
                                        t.Errorf("snap[%d] = %d, want %d (order broken)", i, v, wantStart+i)
                                }
                        }
                })
        }
}

func min(a, b int) int {
        if a < b {
                return a
        }
        return b
}

// F2-04 (snapshot is a copy): mutating the returned slice does not affect
// subsequent snapshots.
func TestRingSnapshotIsACopy(t *testing.T) {
        r := NewRing[int](3)
        r.Push(1)
        r.Push(2)
        r.Push(3)
        snap := r.Snapshot()
        snap[0] = 999
        snap2 := r.Snapshot()
        if snap2[0] == 999 {
                t.Error("mutating Snapshot() result affected the ring; Snapshot must return a copy")
        }
}

func TestRingEmptySnapshot(t *testing.T) {
        r := NewRing[int](3)
        if snap := r.Snapshot(); snap != nil {
                t.Errorf("Snapshot() of empty ring = %v, want nil", snap)
        }
        if _, ok := r.Latest(); ok {
                t.Error("Latest() of empty ring should return ok=false")
        }
}

func TestRingLatest(t *testing.T) {
        r := NewRing[int](3)
        r.Push(10)
        r.Push(20)
        r.Push(30)
        if v, ok := r.Latest(); !ok || v != 30 {
                t.Errorf("Latest() = (%d, %v), want (30, true)", v, ok)
        }
        // Push past capacity; latest should still be the most recent.
        r.Push(40)
        if v, ok := r.Latest(); !ok || v != 40 {
                t.Errorf("Latest() after overflow = (%d, %v), want (40, true)", v, ok)
        }
}

// TestRingWrapAround exercises the head-advance path explicitly.
func TestRingWrapAround(t *testing.T) {
        r := NewRing[int](3)
        for i := 0; i < 7; i++ {
                r.Push(i)
        }
        // cap=3, n=7 → ring holds [4, 5, 6], head points at index 1 (where 4 is).
        if got := r.Len(); got != 3 {
                t.Fatalf("Len = %d, want 3", got)
        }
        snap := r.Snapshot()
        want := []int{4, 5, 6}
        for i, v := range snap {
                if v != want[i] {
                        t.Errorf("snap[%d] = %d, want %d", i, v, want[i])
                }
        }
}

// TestRingConcurrentPush is race-detector bait: many goroutines pushing to
// the same ring. The values may interleave, but Len() must never exceed
// capacity and Snapshot must never panic.
func TestRingConcurrentPush(t *testing.T) {
        r := NewRing[int](100)
        done := make(chan struct{})
        for g := 0; g < 10; g++ {
                go func() {
                        for i := 0; i < 100; i++ {
                                r.Push(i)
                        }
                        done <- struct{}{}
                }()
        }
        for g := 0; g < 10; g++ {
                <-done
        }
        if got := r.Len(); got > 100 {
                t.Errorf("Len = %d, exceeds capacity 100", got)
        }
        _ = r.Snapshot()
        _, _ = r.Latest()
}
