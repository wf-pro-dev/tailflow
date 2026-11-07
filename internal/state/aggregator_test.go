package state

import (
	"testing"
	"time"
)

// F2-06 (unit): Six raw samples across two 5s buckets → two emitted Samples,
// each the mean of its inputs.
func TestAggregatorTwoBuckets(t *testing.T) {
	a := NewAggregator(5 * time.Second)
	// Bucket 1 (t=0, t=2, t=4): cpu 10, 20, 30 → mean 20
	// Bucket 2 (t=5, t=7):     cpu 40, 50    → mean 45
	// We expect emit at the bucket-2 boundary (t=5).
	samples := []RawSample{
		{ReceivedAt: time.Unix(0, 0), CPUTotal: 10, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000},
		{ReceivedAt: time.Unix(2, 0), CPUTotal: 20, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000},
		{ReceivedAt: time.Unix(4, 0), CPUTotal: 30, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000},
		{ReceivedAt: time.Unix(5, 0), CPUTotal: 40, HasMem: true, MemUsedPct: 60, MemUsedBytes: 1200, MemTotalBytes: 2000},
		{ReceivedAt: time.Unix(7, 0), CPUTotal: 50, HasMem: true, MemUsedPct: 60, MemUsedBytes: 1200, MemTotalBytes: 2000},
	}
	var emitted []Sample
	for _, s := range samples {
		if e, ok := a.Add(s); ok {
			emitted = append(emitted, e)
		}
	}
	if len(emitted) != 1 {
		t.Fatalf("got %d emitted samples (mid-stream), want 1", len(emitted))
	}
	if emitted[0].CPUTotal != 20 {
		t.Errorf("bucket 1 mean cpu = %v, want 20", emitted[0].CPUTotal)
	}
	if emitted[0].MemUsedPct != 50 {
		t.Errorf("bucket 1 mean mem pct = %v, want 50", emitted[0].MemUsedPct)
	}
	// Force the second bucket to emit by adding a sample in a third bucket.
	if e, ok := a.Add(RawSample{ReceivedAt: time.Unix(10, 0), CPUTotal: 0, HasMem: true}); ok {
		emitted = append(emitted, e)
	}
	if len(emitted) != 2 {
		t.Fatalf("got %d emitted total, want 2", len(emitted))
	}
	if emitted[1].CPUTotal != 45 {
		t.Errorf("bucket 2 mean cpu = %v, want 45", emitted[1].CPUTotal)
	}
	if emitted[1].MemUsedPct != 60 {
		t.Errorf("bucket 2 mean mem pct = %v, want 60", emitted[1].MemUsedPct)
	}
}

// F2-07 (unit): Payload with nil Memory.Virtual → no panic; memory fields
// skipped (HasMem=false). The emitted sample's mem fields should be 0.
func TestAggregatorNilMemory(t *testing.T) {
	a := NewAggregator(5 * time.Second)
	// First sample: no memory.
	a.Add(RawSample{ReceivedAt: time.Unix(0, 0), CPUTotal: 10, HasMem: false})
	// Second sample in a new bucket: trigger emit of bucket 1.
	e, ok := a.Add(RawSample{ReceivedAt: time.Unix(5, 0), CPUTotal: 20, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000})
	if !ok {
		t.Fatal("expected emit at bucket 2 boundary")
	}
	if e.MemUsedPct != 0 {
		t.Errorf("emitted MemUsedPct = %v, want 0 (nil memory should be skipped)", e.MemUsedPct)
	}
	if e.MemUsedBytes != 0 {
		t.Errorf("emitted MemUsedBytes = %v, want 0", e.MemUsedBytes)
	}
	if e.CPUTotal != 10 {
		t.Errorf("emitted CPUTotal = %v, want 10", e.CPUTotal)
	}
}

// F2-01/F2-02 (rate integration): network counter deltas feed into the
// bucket's net_rx_bps/net_tx_bps.
func TestAggregatorNetworkRates(t *testing.T) {
	a := NewAggregator(5 * time.Second)
	// t=0: rx=1000, tx=500 (seeds prev)
	a.Add(RawSample{
		ReceivedAt: time.Unix(0, 0),
		CPUTotal:   5, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000,
		Net: []NetSample{{Name: "eth0", BytesRecv: 1000, BytesSent: 500}},
	})
	// t=2: rx=3000, tx=1500 → rate = (2000/2) = 1000 B/s rx, 500 B/s tx
	a.Add(RawSample{
		ReceivedAt: time.Unix(2, 0),
		CPUTotal:   5, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000,
		Net: []NetSample{{Name: "eth0", BytesRecv: 3000, BytesSent: 1500}},
	})
	// t=5: trigger emit of bucket 1.
	e, ok := a.Add(RawSample{
		ReceivedAt: time.Unix(5, 0),
		CPUTotal:   5, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000,
		Net: []NetSample{{Name: "eth0", BytesRecv: 3000, BytesSent: 1500}},
	})
	if !ok {
		t.Fatal("expected emit")
	}
	// Bucket 1 has 2 inputs: t=0 (no rate, prev just seeded) and t=2 (rate 1000/500).
	// Mean of [0, 1000] = 500 rx; mean of [0, 500] = 250 tx.
	if e.NetRxBps != 500 {
		t.Errorf("NetRxBps = %v, want 500 (mean of [0, 1000])", e.NetRxBps)
	}
	if e.NetTxBps != 250 {
		t.Errorf("NetTxBps = %v, want 250 (mean of [0, 500])", e.NetTxBps)
	}
}

// Counter reset (F2-02 integration): the second sample has a lower counter;
// the rate should be dropped (treated as 0 in the sum).
func TestAggregatorCounterResetDropped(t *testing.T) {
	a := NewAggregator(5 * time.Second)
	a.Add(RawSample{
		ReceivedAt: time.Unix(0, 0),
		CPUTotal:   5, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000,
		Net: []NetSample{{Name: "eth0", BytesRecv: 5000, BytesSent: 5000}},
	})
	// t=2: counter reset (rx drops to 100). Rate should be dropped.
	a.Add(RawSample{
		ReceivedAt: time.Unix(2, 0),
		CPUTotal:   5, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000,
		Net: []NetSample{{Name: "eth0", BytesRecv: 100, BytesSent: 100}},
	})
	e, ok := a.Add(RawSample{
		ReceivedAt: time.Unix(5, 0),
		CPUTotal:   5, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000,
		Net: []NetSample{{Name: "eth0", BytesRecv: 100, BytesSent: 100}},
	})
	if !ok {
		t.Fatal("expected emit")
	}
	// No valid rate in bucket 1 (t=0 seeded, t=2 reset → dropped).
	if e.NetRxBps != 0 {
		t.Errorf("NetRxBps = %v, want 0 (counter reset should drop rate)", e.NetRxBps)
	}
}

func TestAggregatorCPUPerCoreAveraged(t *testing.T) {
	a := NewAggregator(5 * time.Second)
	a.Add(RawSample{ReceivedAt: time.Unix(0, 0), CPUTotal: 10, CPUPerCore: []float64{5, 15}, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000})
	a.Add(RawSample{ReceivedAt: time.Unix(2, 0), CPUTotal: 30, CPUPerCore: []float64{25, 35}, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000})
	e, ok := a.Add(RawSample{ReceivedAt: time.Unix(5, 0), CPUTotal: 0, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000})
	if !ok {
		t.Fatal("expected emit")
	}
	if e.CPUTotal != 20 {
		t.Errorf("CPUTotal = %v, want 20 (mean of 10, 30)", e.CPUTotal)
	}
	if len(e.CPUPerCore) != 2 {
		t.Fatalf("CPUPerCore len = %d, want 2", len(e.CPUPerCore))
	}
	if e.CPUPerCore[0] != 15 { // mean of 5, 25
		t.Errorf("CPUPerCore[0] = %v, want 15", e.CPUPerCore[0])
	}
	if e.CPUPerCore[1] != 25 { // mean of 15, 35
		t.Errorf("CPUPerCore[1] = %v, want 25", e.CPUPerCore[1])
	}
}

func TestAggregatorClampsPercent(t *testing.T) {
	a := NewAggregator(5 * time.Second)
	// CPU 100.01 should clamp to 100.
	a.Add(RawSample{ReceivedAt: time.Unix(0, 0), CPUTotal: 100.01, HasMem: true, MemUsedPct: 100.01, MemUsedBytes: 1000, MemTotalBytes: 2000})
	e, ok := a.Add(RawSample{ReceivedAt: time.Unix(5, 0), CPUTotal: 0, HasMem: true, MemUsedPct: 50, MemUsedBytes: 1000, MemTotalBytes: 2000})
	if !ok {
		t.Fatal("expected emit")
	}
	if e.CPUTotal != 100 {
		t.Errorf("CPUTotal = %v, want 100 (clamped)", e.CPUTotal)
	}
	if e.MemUsedPct != 100 {
		t.Errorf("MemUsedPct = %v, want 100 (clamped)", e.MemUsedPct)
	}
}
