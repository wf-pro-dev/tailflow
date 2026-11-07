// Aggregator reduces raw client.Metrics events (arriving every 2s by default)
// into one Sample per HISTORY_STEP bucket (default 5s). Design §16.5.
//
// Algorithm:
//   1. Compute bucket = floor(receivedAt / step) * step using the injected
//      clock, not the payload.
//   2. For counters (network bytes), compute the instantaneous rate against
//      the previous raw sample; drop negative deltas (counter reset) and
//      rates above 1 TB/s (implausible).
//   3. Accumulate gauges and rates into running sums with a count.
//   4. When a sample arrives in a later bucket, emit the previous bucket as
//      the mean of its inputs and start a new one. Empty buckets are not
//      emitted; the series API fills gaps with null.
//
// The aggregator is NOT thread-safe; each per-host supervisor owns one and
// feeds it serially from its metrics loop.

package state

import (
	"time"
)

// Aggregator reduces raw metric samples into per-bucket Samples.
type Aggregator struct {
	step time.Duration

	// Previous raw sample (for counter-delta computation).
	prevNet   map[string]uint64 // interface name → bytesRecv
	prevNetTx map[string]uint64 // interface name → bytesSent
	prevTime  time.Time

	// Current bucket accumulator.
	curBucket time.Time
	count     int
	cpuTotal  float64
	cpuPerCoreSum []float64
	memUsedPct float64
	memUsedBytes uint64
	memTotalBytes uint64
	swapUsedPct float64
	netRxBps   float64
	netTxBps   float64
	hasMem     bool
}

// NewAggregator returns an Aggregator that buckets samples by the given step.
func NewAggregator(step time.Duration) *Aggregator {
	if step <= 0 {
		step = 5 * time.Second
	}
	return &Aggregator{
		step:      step,
		prevNet:   make(map[string]uint64),
		prevNetTx: make(map[string]uint64),
	}
}

// RawSample is the input to the aggregator: the per-host metric values
// extracted from one client.Metrics event by the collector. The collector
// is responsible for skipping loopback interfaces and clamping CPU to 0-100.
type RawSample struct {
	ReceivedAt    time.Time
	CPUTotal      float64
	CPUPerCore    []float64
	MemUsedPct    float64
	MemUsedBytes  uint64
	MemTotalBytes uint64
	SwapUsedPct   float64
	HasMem        bool

	// Per-interface cumulative counters (BytesRecv, BytesSent) at the
	// ReceivedAt instant. Loopback should already be filtered out by the
	// collector.
	Net []NetSample
}

// NetSample is one interface's cumulative counters at one instant.
type NetSample struct {
	Name      string
	BytesRecv uint64
	BytesSent uint64
}

// Add feeds one raw sample. If the sample lands in a new bucket, the
// previous bucket is emitted as a Sample (mean of its inputs) and a new
// bucket is started. Returns (emitted, ok) where ok is true when a Sample
// was emitted. The first sample never emits (it just seeds prevNet for
// rate computation on the next sample).
func (a *Aggregator) Add(r RawSample) (Sample, bool) {
	bucket := a.bucketOf(r.ReceivedAt)

	if a.curBucket.IsZero() {
		// First sample ever: seed counters, start bucket.
		a.curBucket = bucket
		a.accumulate(r, true)
		a.seedCounters(r)
		return Sample{}, false
	}

	if bucket.Equal(a.curBucket) {
		// Same bucket: accumulate.
		a.accumulate(r, false)
		a.seedCounters(r)
		return Sample{}, false
	}

	// New bucket: emit the previous bucket as the mean of its inputs.
	emitted := Sample{
		T:             a.curBucket,
		CPUTotal:      a.cpuTotal / float64(a.count),
		MemUsedPct:    a.memUsedPct / float64(a.count),
		MemUsedBytes:  a.memUsedBytes / uint64(a.count),
		MemTotalBytes: a.memTotalBytes / uint64(a.count),
		SwapUsedPct:   a.swapUsedPct / float64(a.count),
		NetRxBps:      a.netRxBps / float64(a.count),
		NetTxBps:      a.netTxBps / float64(a.count),
	}
	if len(a.cpuPerCoreSum) > 0 {
		emitted.CPUPerCore = make([]float64, len(a.cpuPerCoreSum))
		for i, s := range a.cpuPerCoreSum {
			emitted.CPUPerCore[i] = s / float64(a.count)
		}
	}
	if !a.hasMem {
		emitted.MemUsedPct = 0
		emitted.MemUsedBytes = 0
		emitted.MemTotalBytes = 0
		emitted.SwapUsedPct = 0
	}

	// Reset for new bucket.
	a.curBucket = bucket
	a.count = 0
	a.cpuTotal = 0
	a.cpuPerCoreSum = nil
	a.memUsedPct = 0
	a.memUsedBytes = 0
	a.memTotalBytes = 0
	a.swapUsedPct = 0
	a.netRxBps = 0
	a.netTxBps = 0
	a.hasMem = false

	a.accumulate(r, true)
	a.seedCounters(r)

	return emitted, true
}

// bucketOf returns the floor of t to the aggregator's step boundary.
func (a *Aggregator) bucketOf(t time.Time) time.Time {
	stepNs := a.step.Nanoseconds()
	if stepNs <= 0 {
		return t
	}
	ns := t.UnixNano()
	floored := (ns / stepNs) * stepNs
	return time.Unix(0, floored)
}

// accumulate adds r's gauges to the current bucket's running sums. Rates
// for network counters are computed against prevNet and added to the bucket.
// If firstInBucket is true, the cpuPerCoreSum slice is (re)allocated to
// match r.CPUPerCore's length.
func (a *Aggregator) accumulate(r RawSample, firstInBucket bool) {
	a.count++
	a.cpuTotal += clampPercent(r.CPUTotal)

	if firstInBucket || len(a.cpuPerCoreSum) != len(r.CPUPerCore) {
		a.cpuPerCoreSum = make([]float64, len(r.CPUPerCore))
	}
	for i, v := range r.CPUPerCore {
		a.cpuPerCoreSum[i] += clampPercent(v)
	}

	if r.HasMem {
		a.hasMem = true
		a.memUsedPct += clampPercent(r.MemUsedPct)
		a.memUsedBytes += r.MemUsedBytes
		a.memTotalBytes += r.MemTotalBytes
		a.swapUsedPct += clampPercent(r.SwapUsedPct)
	}

	// Network rates: sum instantaneous rates across interfaces into the
	// bucket.
	if !a.prevTime.IsZero() {
		var rxSum, txSum float64
		for _, n := range r.Net {
			if prevRx, ok := a.prevNet[n.Name]; ok {
				if rate, ok := Rate(prevRx, n.BytesRecv, a.prevTime, r.ReceivedAt); ok {
					rxSum += rate
				}
			}
			if prevTx, ok := a.prevNetTx[n.Name]; ok {
				if rate, ok := Rate(prevTx, n.BytesSent, a.prevTime, r.ReceivedAt); ok {
					txSum += rate
				}
			}
		}
		a.netRxBps += rxSum
		a.netTxBps += txSum
	}
}

// seedCounters stores r's counters as the prev for the next Add call.
func (a *Aggregator) seedCounters(r RawSample) {
	a.prevTime = r.ReceivedAt
	for _, n := range r.Net {
		a.prevNet[n.Name] = n.BytesRecv
		a.prevNetTx[n.Name] = n.BytesSent
	}
}

// clampPercent clamps a CPU/memory percentage to [0, 100]. tailkitd
// occasionally emits 100.01 due to float rounding; the API contract is 0-100.
func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
