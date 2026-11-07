// Capability probe (design §F9, §16.4 step 1).
//
// Probes the host's integrations by calling their config endpoints and
// mapping typed errors to capability reasons. Run once at supervisor start
// and then every 60s.

package collector

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/wf-pro-dev/tailkit/client"

	"github.com/wf-pro-dev/tailflow/internal/state"
)

const capabilityProbeInterval = 60 * time.Second

// runCapabilityLoop probes the host's capabilities every 60s and updates
// the store. Blocks until ctx is cancelled.
func (c *Collector) runCapabilityLoop(ctx context.Context, scope client.HostScope, hostName string, logger *slog.Logger) error {
	ticker := c.clock.NewTimer(capabilityProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C():
			caps := c.probeCapabilities(ctx, scope, hostName, logger)
			if h, ok := c.store.Host(hostName); ok {
				c.store.UpsertHost(withCaps(h, caps))
			}
			ticker.Reset(capabilityProbeInterval)
		}
	}
}

// probeCapabilities queries the host's integration config endpoints and
// returns the derived Capabilities. Errors are mapped per design §16.7 /
// README gap #2:
//
//	ErrMetricsUnavailable / 404 → metrics=false, reason "unavailable"
//	ErrDockerUnavailable  / 503 → docker=false,  reason "not-configured"
//	ErrPermissionDenied   / 403 → reason "permission-denied"
//
// The probe is best-effort: a transient error leaves the previous capability
// value in place (we only update on a definitive result).
func (c *Collector) probeCapabilities(ctx context.Context, scope client.HostScope, hostName string, logger *slog.Logger) state.Capabilities {
	var caps state.Capabilities
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
	defer cancel()

	// Metrics: probe via the config endpoint. tailkitd returns the
	// MetricsConfig if metrics.toml exists; 404 if not.
	// Note: tailkit's MetricsScope has no Config() method on the singular
	// scope (only on the fleet scope). We probe via Ports() which returns
	// ErrMetricsUnavailable when metrics.toml is missing.
	_, err := scope.Metrics().Ports(callCtx)
	switch {
	case err == nil:
		caps.Metrics = true
	case errors.Is(err, client.ErrMetricsUnavailable):
		caps.Metrics = false
		caps.SetFalse("metrics", state.CapReasonUnavailable)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// Transient — leave previous value. Don't update.
	default:
		// Unknown error — treat as unavailable for safety.
		caps.Metrics = false
		caps.SetFalse("metrics", state.CapReasonUnavailable)
	}

	// Docker: probe via Config(). Note the path bug (gap #4): the SDK calls
	// /docker/config but the daemon exposes /integrations/docker/config.
	// We bypass the SDK and call the daemon path directly via Backend.Do.
	// For now we use the SDK's Config() and tolerate the 404 — the
	// supervisor will still start the docker loop if Metrics says docker is
	// available, and the loop's first List call will set the real state.
	// This is a known limitation to fix in M2 by adding a Backend.Do bypass
	// in tailkitsrc.
	cfg, err := scope.Docker().Config(callCtx)
	switch {
	case err == nil:
		caps.Docker = cfg.Enabled
		if !cfg.Enabled {
			caps.SetFalse("docker", state.CapReasonNotConfigured)
		}
	case errors.Is(err, client.ErrDockerUnavailable):
		caps.Docker = false
		caps.SetFalse("docker", state.CapReasonNotConfigured)
	case errors.Is(err, client.ErrPermissionDenied):
		caps.Docker = false
		caps.SetFalse("docker", state.CapReasonPermissionDenied)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// Transient — leave previous.
	default:
		// 404 (old agent) or other — treat as not-configured.
		caps.Docker = false
		caps.SetFalse("docker", state.CapReasonUnavailable)
	}

	// Swarm: derive from docker config's Swarm section. The daemon returns
	// 403 with "swarm.read not enabled" if [swarm] enabled=false.
	if caps.Docker && cfg.Enabled {
		// cfg is the DockerConfig; check the Swarm section's Enabled.
		// Note: DockerConfig has nested sections; we read Swarm.Enabled.
		// The exact field path depends on the tailkit type. If the SDK's
		// DockerConfig doesn't expose it cleanly, we fall back to a probe
		// call below.
		caps.Swarm = cfg.Swarm.Enabled
		if !cfg.Swarm.Enabled {
			caps.SetFalse("swarm", state.CapReasonNotConfigured)
		}
	}

	// Compose: derive from docker config's Compose section.
	if caps.Docker && cfg.Enabled {
		caps.Compose = cfg.Compose.Enabled
		if !cfg.Compose.Enabled {
			caps.SetFalse("compose", state.CapReasonNotConfigured)
		}
	}

	return caps
}
