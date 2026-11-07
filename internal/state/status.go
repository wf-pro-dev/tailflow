// Derived host status state machine (design §5.2).
//
// Inputs:
//   - Host.Online (peer online per Tailscale)
//   - agent reachable: Host.Tailkitd != nil && Host.Tailkitd.Peer != nil && Host.Tailkitd.Peer.Online
//   - sample freshness: time since LastSampleAt vs stream interval × stale factor
//   - consecutive stream failures (with hysteresis to avoid flapping)
//
// Status is recomputed by the collector's status loop every second; the
// store only persists the result.

package state

import "time"

// DeriveStatus computes the status from the three inputs. failures is the
// number of consecutive stream failures; degradeThreshold is
// TAILFLOW_DEGRADE_THRESHOLD (default 3). The hysteresis (don't flap back
// to healthy on the first success after degraded) is the caller's
// responsibility — the supervisor only increments the counter on actual
// failures and resets it after streamInterval*30 of healthy streaming (§F2).
func DeriveStatus(online, agentReachable bool, lastSampleAt *time.Time, now time.Time, streamInterval time.Duration, staleFactor int, failures, degradeThreshold int) Status {
	switch {
	case !online:
		return StatusOffline
	case !agentReachable:
		return StatusNoAgent
	case lastSampleAt == nil:
		// Never sampled yet — treat as no-agent (we have no freshness signal).
		// The collector will overwrite this with healthy on the first sample.
		return StatusNoAgent
	case isStale(lastSampleAt, now, streamInterval, staleFactor):
		return StatusStale
	case failures >= degradeThreshold:
		return StatusDegraded
	default:
		return StatusHealthy
	}
}

// isStale returns true if the last sample is older than staleFactor ×
// streamInterval (design §5.2). With defaults (factor 3, interval 2s) that's
// 6s — matching README gap #4 / design D2.
func isStale(lastSampleAt *time.Time, now time.Time, streamInterval time.Duration, staleFactor int) bool {
	if staleFactor < 1 {
		staleFactor = 3
	}
	if streamInterval <= 0 {
		streamInterval = 2 * time.Second
	}
	threshold := time.Duration(staleFactor) * streamInterval
	return now.Sub(*lastSampleAt) > threshold
}

// IsReachableAgent reports whether the host's tailkitd peer is online. This
// is the same check tailkit's peers.go hostReachable uses, lifted here so
// the state package doesn't depend on tailkit types. The collector calls
// this with the values it pulled from client.Host.
func IsReachableAgent(online bool, tailkitdPeerOnline *bool) bool {
	if !online {
		return false
	}
	if tailkitdPeerOnline == nil {
		return false
	}
	return *tailkitdPeerOnline
}
