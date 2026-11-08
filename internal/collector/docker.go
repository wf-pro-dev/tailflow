// Docker loop (design §F6, §16.4 step 4).
//
// Algorithm: event-driven refresh with debounce.
//   1. Initial Containers().List(ctx) → store.SetContainers.
//   2. Subscribe to Docker().Stream(ctx, fn) for lifecycle events.
//   3. Ignore noisy actions (exec_*, attach, detach, top, resize,
//      archive-path, extract-to-dir) using the filter salvaged from the
//      first Tailflow prototype.
//   4. On a relevant event, trigger a refetch debounced 300ms trailing-edge
//      so a burst of events (e.g. a Compose restart) causes one list call.
//   5. Also refetch Swarm services and Compose projects when their capability
//      flags are true.
//
// Acceptance (F6-01): 50 events in 100ms produce exactly one list call.
// Acceptance (F6-02): events exec_start, attach, top are ignored.

package collector

import (
        "context"
        "errors"
        "log/slog"
        "sync"
        "time"

        "github.com/wf-pro-dev/tailkit/client"

        "github.com/wf-pro-dev/tailflow/internal/state"
)

const dockerDebounce = 300 * time.Millisecond

// runDockerLoop subscribes to Docker events and refetches the container
// inventory (debounced) on relevant events. Blocks until ctx is cancelled.
func (c *Collector) runDockerLoop(ctx context.Context, scope client.HostScope, hostName string, logger *slog.Logger) error {
        // Initial fetch.
        if err := c.refetchDockerInventory(ctx, scope, hostName, logger); err != nil {
                logger.Warn("initial docker fetch failed", "err", err)
        }

        // Debounce state: a timer is reset on every relevant event; when it
        // finally fires, we refetch.
        var mu sync.Mutex
        var timer *time.Timer

        triggerRefetch := func() {
                mu.Lock()
                defer mu.Unlock()
                if timer != nil {
                        timer.Stop()
                }
                timer = time.AfterFunc(dockerDebounce, func() {
                        if err := c.refetchDockerInventory(ctx, scope, hostName, logger); err != nil {
                                logger.Warn("debounced docker refetch failed", "err", err)
                        }
                })
        }

        // Subscribe to Docker events.
        err := scope.Docker().Stream(ctx, func(e client.Event[client.DockerEvent]) error {
                ev := e.Data
                // ev.Action is events.Action (a string typedef); convert to string.
                if !shouldRefreshDockerInventory(string(ev.Action)) {
                        return nil
                }
                triggerRefetch()
                return nil
        })
        if err != nil {
                if errors.Is(err, client.ErrDockerUnavailable) {
                        // Docker was disabled after the loop started. Exit cleanly so
                        // the errgroup doesn't cancel the metrics and ports loops.
                        logger.Debug("docker unavailable on this host")
                        return nil
                }
                if errors.Is(err, context.Canceled) {
                        return err
                }
                logger.Warn("docker event stream ended", "err", err)
        }

        // Stop any pending debounce timer on exit.
        mu.Lock()
        if timer != nil {
                timer.Stop()
        }
        mu.Unlock()

        return err
}

// refetchDockerInventory lists containers, swarm services, and compose
// projects (per their capability flags) and pushes the result to the store.
func (c *Collector) refetchDockerInventory(ctx context.Context, scope client.HostScope, hostName string, logger *slog.Logger) error {
        callCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
        defer cancel()

        var inv state.ContainerInventory

        containers, err := scope.Docker().Containers().List(callCtx)
        if err != nil {
                if errors.Is(err, client.ErrDockerUnavailable) {
                        // Docker was disabled after the loop started. Treat as empty.
                        logger.Debug("docker unavailable during refetch", "err", err)
                        return nil
                }
                return err
        }
        inv.Containers = make([]state.Container, len(containers))
        for i, ct := range containers {
                inv.Containers[i] = mapContainer(ct)
        }

        // Best-effort swarm + compose; failures just leave those slices empty.
        h, ok := c.store.Host(hostName)
        if ok && h.Caps.Swarm {
                if services, err := scope.Docker().Swarm().Services().List(callCtx); err == nil {
                        inv.SwarmServices = make([]state.SwarmService, len(services))
                        for i, s := range services {
                                inv.SwarmServices[i] = mapSwarmService(s)
                        }
                } else if !errors.Is(err, client.ErrPermissionDenied) && !errors.Is(err, client.ErrDockerUnavailable) {
                        logger.Warn("swarm services list failed", "err", err)
                }
        }
        if ok && h.Caps.Compose {
                if projects, err := listComposeProjects(callCtx, scope); err == nil {
                        inv.ComposeProjects = projects
                } else if !errors.Is(err, client.ErrPermissionDenied) && !errors.Is(err, client.ErrDockerUnavailable) {
                        logger.Warn("compose projects list failed", "err", err)
                }
        }

        // Best-effort image list.
        if images, err := scope.Docker().Images().List(callCtx); err == nil {
                inv.Images = make([]state.DockerImage, len(images))
                for i, img := range images {
                        inv.Images[i] = state.DockerImage{
                                ID:        img.ID,
                                Tags:      append([]string(nil), img.RepoTags...),
                                Size:      img.Size,
                                CreatedAt: img.Created,
                        }
                }
        } else if !errors.Is(err, client.ErrPermissionDenied) && !errors.Is(err, client.ErrDockerUnavailable) {
                logger.Debug("images list failed", "err", err)
        }

        c.store.SetContainers(hostName, inv)
        return nil
}

// shouldRefreshDockerInventory returns true for Docker event actions that
// should trigger a container-list refetch. Salvaged verbatim from the first
// Tailflow prototype (internal/collector/collector.go:535-574).
//
// Ignored (noisy, no inventory impact): attach, detach, exec_create,
// exec_start, exec_die, top, resize, archive-path, extract-to-dir.
// Accepted: create, start, restart, stop, kill, die, oom, pause, unpause,
// update, rename, destroy, remove, and any health_status_* prefix.
func shouldRefreshDockerInventory(action string) bool {
        switch action {
        case "attach", "detach",
                "exec_create", "exec_start", "exec_die",
                "top", "resize",
                "archive-path", "extract-to-dir":
                return false
        default:
                // health_status: healthy/unhealthy/start are accepted (prefix match).
                // All other non-ignored actions are accepted.
                return true
        }
}
