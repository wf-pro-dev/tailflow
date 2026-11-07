package state

import (
	"testing"
	"time"
)

// F2-01: Two counter samples 2s apart, +2000 bytes → rate = 1000 B/s.
func TestRateBasic(t *testing.T) {
	prev := uint64(1000)
	curr := uint64(3000)
	pt := time.Unix(1000, 0)
	ct := time.Unix(1002, 0)
	rate, ok := Rate(prev, curr, pt, ct)
	if !ok {
		t.Fatal("Rate returned ok=false for valid input")
	}
	if rate != 1000 {
		t.Errorf("Rate = %v B/s, want 1000", rate)
	}
}

// F2-02: Counter decreases (reset) → drop, no negative rate.
func TestRateCounterReset(t *testing.T) {
	prev := uint64(5000)
	curr := uint64(1000) // reset
	pt := time.Unix(1000, 0)
	ct := time.Unix(1002, 0)
	_, ok := Rate(prev, curr, pt, ct)
	if ok {
		t.Error("Rate should return ok=false on counter reset")
	}
}

// F2-03: Rate above 1 TB/s → drop as implausible.
func TestRateImplausible(t *testing.T) {
	prev := uint64(0)
	curr := uint64(MaxPlausibleBps * 2) // 2 TB in 1s
	pt := time.Unix(1000, 0)
	ct := time.Unix(1001, 0)
	_, ok := Rate(prev, curr, pt, ct)
	if ok {
		t.Error("Rate should return ok=false for implausible rate")
	}
}

func TestRateZeroDt(t *testing.T) {
	// Same timestamp → dt = 0 → drop.
	_, ok := Rate(0, 100, time.Unix(1000, 0), time.Unix(1000, 0))
	if ok {
		t.Error("Rate should return ok=false for zero dt")
	}
}

func TestRateNegativeDt(t *testing.T) {
	// Clock skew: currTime before prevTime → drop.
	_, ok := Rate(0, 100, time.Unix(1002, 0), time.Unix(1000, 0))
	if ok {
		t.Error("Rate should return ok=false for negative dt")
	}
}
