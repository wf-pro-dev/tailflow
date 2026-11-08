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
                        // Use SetCaps, NOT UpsertHost — UpsertHost can regress
                        // LastSampleAt (bug B2) and clobber Status. SetCaps only
                        // touches the Caps field.
                        c.store.SetCaps(hostName, caps)

                        // Re-fetch host metadata (role, environment, etc.) from the
                        // daemon's /host endpoint. This picks up changes to host.toml
                        // without requiring a supervisor restart.
                        if fullHost, err := scope.Get(ctx); err == nil {
                                c.store.UpdateDiscoveryFields(hostName, mapHost(fullHost, c.cfg.StreamInterval))
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
        // Start with the current caps from the store so we don't clobber a known-good
        // value with a transient failure. The probe only updates a field when it gets
        // a definitive answer (success or a typed "unavailable" error).
        if existing, ok := c.store.Host(hostName); ok {
                caps := existing.Caps
                c.probeCapabilitiesInto(ctx, scope, hostName, logger, &caps)
                return caps
        }
        var caps state.Capabilities
        c.probeCapabilitiesInto(ctx, scope, hostName, logger, &caps)
        return caps
}

// probeCapabilitiesInto updates caps in place. It only clears a capability
// on a definitive "not configured" signal (typed error), never on transient
// errors (timeout, cancellation, unknown error). This prevents a brief
// network blip from clearing capabilities that are actually fine.
func (c *Collector) probeCapabilitiesInto(ctx context.Context, scope client.HostScope, hostName string, logger *slog.Logger, caps *state.Capabilities) {
        callCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
        defer cancel()

        // Metrics: probe via CPU() which returns ErrMetricsUnavailable when
        // metrics.toml is missing.
        _, err := scope.Metrics().CPU(callCtx)
        switch {
        case err == nil:
                caps.Metrics = true
                delete(caps.Reasons, "metrics")
        case errors.Is(err, client.ErrMetricsUnavailable):
                caps.SetFalse("metrics", state.CapReasonUnavailable)
        case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
                // Transient — leave previous value.
                logger.Debug("metrics probe transient error", "err", err)
        default:
                // Unknown error — log it but DON'T clear the capability. The previous
                // value (likely true from a successful probe) stays. Only clear on a
                // definitive ErrMetricsUnavailable.
                logger.Debug("metrics probe unknown error, keeping previous", "err", err)
        }

        // Docker: bypass the SDK and call /integrations/docker/config directly.
        cfg, err := fetchDockerConfig(callCtx, scope)
        switch {
        case err == nil:
                caps.Docker = cfg.Enabled
                delete(caps.Reasons, "docker")
                if !cfg.Enabled {
                        caps.SetFalse("docker", state.CapReasonNotConfigured)
                }
        case errors.Is(err, client.ErrDockerUnavailable):
                caps.SetFalse("docker", state.CapReasonNotConfigured)
        case errors.Is(err, client.ErrPermissionDenied):
                caps.SetFalse("docker", state.CapReasonPermissionDenied)
        case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
                logger.Debug("docker probe transient error", "err", err)
        default:
                logger.Debug("docker probe unknown error, keeping previous", "err", err)
        }

        // Swarm and Compose: derive from the docker config's sections.
        if cfg.Enabled && err == nil {
                caps.Swarm = cfg.Swarm.Enabled
                delete(caps.Reasons, "swarm")
                if !cfg.Swarm.Enabled {
                        caps.SetFalse("swarm", state.CapReasonNotConfigured)
                }
                caps.Compose = cfg.Compose.Enabled
                delete(caps.Reasons, "compose")
                if !cfg.Compose.Enabled {
                        caps.SetFalse("compose", state.CapReasonNotConfigured)
                }
        }

        logger.Debug("capability probe result",
                "metrics", caps.Metrics,
                "docker", caps.Docker,
                "swarm", caps.Swarm,
                "compose", caps.Compose,
                "reasons", caps.Reasons,
                "docker_cfg_enabled", cfg.Enabled,
                "docker_cfg_err", err,
        )
}
