// Per-host supervisor (design §16.4). One goroutine tree per discovered host,
// owned by an errgroup whose context is cancelled when the host disappears
// or the process stops.
//
// The supervisor runs up to five concurrent loops:
//   1. Capability probe (every 60s)
//   2. Metrics loop (if Caps.Metrics) — reconnect w/ backoff + aggregator
//   3. Ports loop (if Caps.Metrics) — snapshot + stream deltas
//   4. Docker loop (if Caps.Docker) — debounced refetch on events
//   5. Status loop — recompute Status every 1s

package collector

import (
        "context"
        "errors"
        "log/slog"
        "time"

        "golang.org/x/sync/errgroup"

        "github.com/wf-pro-dev/tailkit/client"

        "github.com/wf-pro-dev/tailflow/internal/state"
)

// runSupervisor owns the goroutine tree for one host. It blocks until the
// host's context is cancelled (host removed or process stopping).
func (c *Collector) runSupervisor(ctx context.Context, h client.Host) {
        hostName := h.Name
        logger := c.logger.With("host", hostName)

        // Seed the store with the host's static fields. The capability probe
        // will fill in Caps shortly; until then the host appears with zero caps.
        c.store.UpsertHost(mapHost(h, c.cfg.StreamInterval))

        scope := c.fleet.Host(hostName)

        // Initial capability probe (synchronous) so the supervisor knows which
        // loops to start.
        caps := c.probeCapabilities(ctx, scope, hostName, logger)
        c.store.UpsertHost(withCaps(mapHost(h, c.cfg.StreamInterval), caps))

        // Errgroup with the host's context. When any loop returns an error
        // (other than ctx.Err()), the group cancels the others.
        g, gctx := errgroup.WithContext(ctx)

        // Capability probe loop (every 60s).
        g.Go(func() error {
                return c.runCapabilityLoop(gctx, scope, hostName, logger)
        })

        if caps.Metrics {
                // Metrics loop: reconnect-wrapped All stream → aggregator → store.
                g.Go(func() error {
                        return c.runMetricsLoop(gctx, scope, hostName, logger)
                })
                // Ports loop: snapshot + stream deltas.
                g.Go(func() error {
                        return c.runPortsLoop(gctx, scope, hostName, logger)
                })
        }

        if caps.Docker {
                // Docker loop: debounced refetch on events.
                g.Go(func() error {
                        return c.runDockerLoop(gctx, scope, hostName, logger)
                })
        }

        // Status loop: recompute Status every 1s.
        g.Go(func() error {
                return c.runStatusLoop(gctx, hostName, logger)
        })

        if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, errHostRemoved) {
                logger.Warn("supervisor exited with error", "err", err)
        } else {
                logger.Debug("supervisor exited")
        }
}

// runStatusLoop recomputes the host's derived status every second and writes
// it to the store (only when it changes — SetStatus is a no-op otherwise).
func (c *Collector) runStatusLoop(ctx context.Context, hostName string, logger *slog.Logger) error {
        ticker := c.clock.NewTimer(time.Second)
        defer ticker.Stop()

        for {
                select {
                case <-ctx.Done():
                        return ctx.Err()
                case <-ticker.C():
                        h, ok := c.store.Host(hostName)
                        if !ok {
                                return errHostRemoved
                        }
                        // Use the AgentOnline field set by mapHost from the discovery
                        // data (client.Host.Tailkitd.Peer.Online). This is the real
                        // agent-reachability signal — NOT derived from the current status
                        // (which would be circular: status depends on agentReachable,
                        // agentReachable would depend on status).
                        agentReachable := h.AgentOnline
                        failures := c.store.Failures(hostName)
                        status := state.DeriveStatus(
                                h.Online,
                                agentReachable,
                                h.LastSampleAt,
                                c.clock.Now(),
                                h.StreamInterval,
                                c.cfg.StaleFactor,
                                failures,
                                c.cfg.DegradeThreshold,
                        )
                        c.store.SetStatus(hostName, status)
                        ticker.Reset(time.Second)
                }
        }
}

// withCaps returns a copy of h with the capabilities set.
func withCaps(h state.Host, caps state.Capabilities) state.Host {
        h.Caps = caps
        return h
}
