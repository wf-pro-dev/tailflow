// Systemd loop — fetches systemd units periodically and stores them.
// Design: the systemd tab shows unit name, load state, active state,
// sub state, and description. Failed units are highlighted.
//
// We poll every 30s (systemd units don't change often, and there's no
// SSE stream for them in tailkitd v0.1).

package collector

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/wf-pro-dev/tailkit/client"

	"github.com/wf-pro-dev/tailflow/internal/state"
)

const systemdPollInterval = 30 * time.Second

// runSystemdLoop polls systemd units every 30s and stores them.
func (c *Collector) runSystemdLoop(ctx context.Context, scope client.HostScope, hostName string, logger *slog.Logger) error {
	// Initial fetch.
	c.refetchSystemdUnits(ctx, scope, hostName, logger)

	ticker := c.clock.NewTimer(systemdPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C():
			c.refetchSystemdUnits(ctx, scope, hostName, logger)
			ticker.Reset(systemdPollInterval)
		}
	}
}

func (c *Collector) refetchSystemdUnits(ctx context.Context, scope client.HostScope, hostName string, logger *slog.Logger) {
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
	defer cancel()

	units, err := scope.Systemd().Units().List(callCtx)
	if err != nil {
		if errors.Is(err, client.ErrSystemdUnavailable) {
			logger.Debug("systemd unavailable on this host")
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		logger.Debug("systemd units list failed", "err", err)
		return
	}

	mapped := make([]state.SystemdUnit, len(units))
	for i, u := range units {
		mapped[i] = state.SystemdUnit{
			Name:        u.Name,
			LoadState:   u.LoadState,
			ActiveState: u.ActiveState,
			SubState:    u.SubState,
			Description: u.Description,
		}
	}
	c.store.SetSystemdUnits(hostName, mapped)
}
