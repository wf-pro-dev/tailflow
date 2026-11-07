// Rate computation for cumulative counters (design §F2).
//
// tailkitd emits network counters as cumulative totals (BytesSent/BytesRecv
// from gopsutil). To show a rate we compute (c_now - c_prev) / dt. Two edge
// cases to handle:
//
//   1. Counter reset (negative delta): the host rebooted or the interface
//      was recreated. Drop the sample; do not emit a negative rate (F2-02).
//
//   2. Implausible rate (> 1 TB/s): the upstream sent garbage. Drop the
//      sample (F2-03).
//
// Use the monotonic receive time (now), not the payload timestamp, for dt —
// the payload timestamp can drift or jump if the host's clock is off.

package state

import "time"

// MaxPlausibleBps is 1 TB/s in bytes/s. Rates above this are dropped as
// implausible (design §F2, §16.5).
const MaxPlausibleBps = 1_000_000_000_000

// Rate computes the per-second rate from two cumulative counter readings.
// Returns (rate, ok). ok is false when the delta should be dropped (counter
// reset or implausible rate).
func Rate(prev, curr uint64, prevTime, currTime time.Time) (float64, bool) {
	dt := currTime.Sub(prevTime).Seconds()
	if dt <= 0 {
		return 0, false
	}
	if curr < prev {
		// Counter reset (reboot, interface recreation). Drop.
		return 0, false
	}
	rate := float64(curr-prev) / dt
	if rate > MaxPlausibleBps {
		return 0, false
	}
	return rate, true
}
