// Ports loop (design §F5, §16.4 step 3).
//
// 1. Fetch the initial snapshot via Metrics().Ports(ctx) and call
//    store.SetPorts.
// 2. Subscribe to Metrics().Stream().Ports(ctx, fn) for snapshot/bound/
//    released deltas. Apply each via store.ApplyPortUpdate.
//
// Acceptance (F5-01): replaying a recorded sequence of snapshot/bound/
// released events yields the same port set as the final snapshot.

package collector

import (
        "context"
        "errors"
        "log/slog"

        "github.com/wf-pro-dev/tailkit/client"

        "github.com/wf-pro-dev/tailflow/internal/state"
)

// runPortsLoop fetches the initial port snapshot then subscribes to port
// stream deltas. Blocks until ctx is cancelled.
func (c *Collector) runPortsLoop(ctx context.Context, scope client.HostScope, hostName string, logger *slog.Logger) error {
        // 1. Initial snapshot.
        snap, err := scope.Metrics().Ports(ctx)
        if err != nil {
                if errors.Is(err, client.ErrMetricsUnavailable) || errors.Is(err, context.Canceled) {
                        logger.Debug("ports snapshot unavailable", "err", err)
                } else {
                        logger.Warn("ports snapshot failed", "err", err)
                }
                // Don't return; the stream below might still work.
        } else {
                ports := make([]state.Port, len(snap))
                for i, p := range snap {
                        ports[i] = mapPort(p)
                }
                c.store.SetPorts(hostName, ports)
        }

        // 2. Stream deltas.
        err = scope.Metrics().Stream().Ports(ctx, func(e client.Event[client.PortUpdate]) error {
                u := e.Data
                switch u.Kind {
                case "snapshot":
                        ports := make([]state.Port, len(u.Ports))
                        for i, p := range u.Ports {
                                ports[i] = mapPort(p)
                        }
                        c.store.ApplyPortUpdate(hostName, "snapshot", state.Port{}, ports)
                case "bound":
                        c.store.ApplyPortUpdate(hostName, "bound", mapPort(u.Port), nil)
                case "released":
                        c.store.ApplyPortUpdate(hostName, "released", mapPort(u.Port), nil)
                }
                return nil
        })
        if err != nil {
                if errors.Is(err, client.ErrMetricsUnavailable) {
                        // Ports are disabled in metrics.toml ([ports] enabled=false).
                        // This is a normal configuration, not an error — exit cleanly
                        // so the errgroup doesn't cancel the metrics and docker loops.
                        logger.Debug("ports disabled on this host")
                        return nil
                }
                if errors.Is(err, context.Canceled) {
                        return err
                }
                logger.Warn("ports stream ended", "err", err)
        }
        return err
}

// mapPort converts a client.Port to a state.Port.
func mapPort(p client.Port) state.Port {
        return state.Port{
                Addr:    p.Addr,
                Port:    p.Port,
                Proto:   p.Proto,
                PID:     p.PID,
                Process: p.Process,
        }
}
