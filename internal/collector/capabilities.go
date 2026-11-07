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
//      ErrMetricsUnavailable / 404 → metrics=false, reason "unavailable"
//      ErrDockerUnavailable  / 503 → docker=false,  reason "not-configured"
//      ErrPermissionDenied   / 403 → reason "permission-denied"
//
// The probe is best-effort: a transient error leaves the previous capability
// value in place (we only update on a definitive result).
func (c *Collector) probeCapabilities(ctx context.Context, scope client.HostScope, hostName string, logger *slog.Logger) state.Capabilities {
        var caps state.Capabilities
        callCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
        defer cancel()

        // Metrics: probe via CPU() which returns ErrMetricsUnavailable when
        // metrics.toml is missing. We previously used Ports(), but the tailkitd
        // installer's skeleton metrics.toml omits the [ports] section (gap #5),
        // so Ports() returns 503 on freshly-installed nodes even though the
        // metrics All stream works fine. CPU() is a core metrics feature that's
        // always enabled when metrics.toml exists.
        _, err := scope.Metrics().CPU(callCtx)
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

        // Docker: bypass the SDK and call /integrations/docker/config directly
        // via Backend.Do. The SDK's Docker().Config() hits /docker/config (gap
        // #4) which doesn't exist on the daemon — it would always 404. The
        // daemon exposes /integrations/docker/config.
        cfg, err := fetchDockerConfig(callCtx, scope)
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
                // 404 (old agent) or other — treat as unavailable.
                caps.Docker = false
                caps.SetFalse("docker", state.CapReasonUnavailable)
        }

        // Swarm and Compose: derive from the docker config's sections. Only
        // meaningful when Docker itself is enabled.
        if caps.Docker && cfg.Enabled {
                caps.Swarm = cfg.Swarm.Enabled
                if !cfg.Swarm.Enabled {
                        caps.SetFalse("swarm", state.CapReasonNotConfigured)
                }
                caps.Compose = cfg.Compose.Enabled
                if !cfg.Compose.Enabled {
                        caps.SetFalse("compose", state.CapReasonNotConfigured)
                }
        }

        return caps
}
