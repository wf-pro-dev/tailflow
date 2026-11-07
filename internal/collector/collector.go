// Package collector owns the discovery loop and per-host supervisors that
// feed the in-memory state store. It is the only package besides
// internal/source that imports tailkit/client (design §F10).
//
// Architecture (design §4.2):
//
//      Discovery loop
//        └─ reconcile(discovered, running)
//             ├─ start supervisor for each added host
//             └─ cancel context for each removed host
//
//      Per-host supervisor (one goroutine tree per host, owned by errgroup)
//        ├─ capability probe (every 60s)
//        ├─ metrics loop (reconnect w/ backoff + aggregator → store.PushSample)
//        ├─ ports loop (snapshot + stream deltas → store.ApplyPortUpdate)
//        ├─ docker loop (debounced refetch on events → store.SetContainers)
//        └─ status loop (recompute Status every 1s → store.SetStatus)
package collector

import (
        "context"
        "errors"
        "log/slog"
        "sync"
        "time"

        "golang.org/x/sync/singleflight"

        "github.com/wf-pro-dev/tailkit/client"

        "github.com/wf-pro-dev/tailflow/internal/source"
        "github.com/wf-pro-dev/tailflow/internal/state"
)

// Collector owns the discovery loop and the set of per-host supervisors.
type Collector struct {
        fleet    source.Fleet
        store    *state.Store
        clock    source.Clock
        logger   *slog.Logger
        cfg      Config

        mu       sync.Mutex
        supervisors map[string]context.CancelFunc // host name → cancel
        sfGroup  singleflight.Group

        // streams is the refcounted on-demand stream manager for container
        // stats and logs (design §F7). Lazily initialised on first use.
        streams     *StreamManager
        streamsOnce sync.Once
}

// Config holds the collector's tunable parameters, derived from
// internal/config.Config at startup.
type Config struct {
        DiscoveryInterval time.Duration
        HistoryStep       time.Duration
        CallTimeout       time.Duration
        StaleFactor       int
        DegradeThreshold  int
        MaxHosts          int
}

// New returns a Collector ready to Run. It does not start any goroutines.
func New(fleet source.Fleet, store *state.Store, clock source.Clock, logger *slog.Logger, cfg Config) *Collector {
        return &Collector{
                fleet:       fleet,
                store:       store,
                clock:       clock,
                logger:      logger,
                cfg:         cfg,
                supervisors: make(map[string]context.CancelFunc),
        }
}

// Streams returns the refcounted on-demand stream manager for container
// stats and logs (design §F7). Lazily initialised on first call.
func (c *Collector) Streams() *StreamManager {
        c.streamsOnce.Do(func() {
                c.streams = c.NewStreamManager()
        })
        return c.streams
}

// Run starts the discovery loop and blocks until ctx is cancelled. On
// cancellation it stops all supervisors and waits for them to drain.
func (c *Collector) Run(ctx context.Context) error {
        c.logger.Info("collector starting",
                "discovery_interval", c.cfg.DiscoveryInterval.String(),
                "history_step", c.cfg.HistoryStep.String(),
                "max_hosts", c.cfg.MaxHosts,
        )

        // Run discovery once immediately so the UI shows hosts without waiting
        // for the first tick.
        c.discoverOnce(ctx)

        ticker := c.clock.NewTimer(c.cfg.DiscoveryInterval)
        defer ticker.Stop()

        for {
                select {
                case <-ctx.Done():
                        c.logger.Info("collector stopping")
                        c.stopAllSupervisors()
                        return ctx.Err()
                case <-ticker.C():
                        c.discoverOnce(ctx)
                        ticker.Reset(c.cfg.DiscoveryInterval)
                }
        }
}

// discoverOnce lists hosts and reconciles the supervisor set. Multiple
// concurrent triggers (e.g. from a manual Refresh) share one Fleet.Discover
// call via singleflight (design §F1, F1-05).
func (c *Collector) discoverOnce(ctx context.Context) {
        // singleflight dedupes concurrent triggers. The key is constant so any
        // number of concurrent callers all get the same result.
        v, err, _ := c.sfGroup.Do("discover", func() (any, error) {
                // Use a child context with a timeout so a hung Discover doesn't block
                // the loop forever.
                dctx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
                defer cancel()
                return c.fleet.Discover(dctx)
        })
        if err != nil {
                // F1-04: previous hosts and statuses retained; error logged; retried
                // next interval. Do NOT remove hosts on a Discover error.
                c.logger.Warn("discover failed", "err", err)
                return
        }
        hosts := v.([]client.Host)
        c.reconcile(ctx, hosts)
}

// reconcile starts supervisors for added hosts and stops supervisors for
// removed hosts (design §F1). Set reconciliation by host name.
func (c *Collector) reconcile(ctx context.Context, discovered []client.Host) {
        discoveredNames := make(map[string]client.Host, len(discovered))
        for _, h := range discovered {
                discoveredNames[h.Name] = h
        }

        // F1-06: cap on supervised hosts.
        if len(discovered) > c.cfg.MaxHosts {
                c.logger.Warn("discovered hosts exceed MAX_HOSTS; extras ignored",
                        "discovered", len(discovered), "max", c.cfg.MaxHosts)
                // Trim the map deterministically (by sorted name) so the same hosts
                // are kept across runs.
                names := make([]string, 0, len(discoveredNames))
                for n := range discoveredNames {
                        names = append(names, n)
                }
                sortStrings(names)
                for i := c.cfg.MaxHosts; i < len(names); i++ {
                        delete(discoveredNames, names[i])
                }
        }

        c.mu.Lock()
        defer c.mu.Unlock()

        // Added: in discovered but not in supervisors.
        for name, h := range discoveredNames {
                if _, running := c.supervisors[name]; running {
                        // Already running — update the host's static fields.
                        c.store.UpsertHost(mapHost(h, c.cfg.HistoryStep))
                        continue
                }
                // Start a new supervisor.
                supCtx, cancel := context.WithCancel(ctx)
                c.supervisors[name] = cancel
                go c.runSupervisor(supCtx, h)
                c.logger.Info("supervisor started", "host", name)
        }

        // Removed: in supervisors but not in discovered.
        for name, cancel := range c.supervisors {
                if _, still := discoveredNames[name]; still {
                        continue
                }
                cancel()
                delete(c.supervisors, name)
                c.store.RemoveHost(name)
                c.logger.Info("supervisor stopped", "host", name)
        }
}

// stopAllSupervisors cancels every supervisor's context. Called on shutdown.
func (c *Collector) stopAllSupervisors() {
        c.mu.Lock()
        defer c.mu.Unlock()
        for name, cancel := range c.supervisors {
                cancel()
                delete(c.supervisors, name)
        }
}

// Refresh triggers an immediate discovery cycle (in addition to the periodic
// tick). Used by future write operations that need to see the new host list
// right away. Blocks until the discover call completes.
func (c *Collector) Refresh(ctx context.Context) error {
        c.discoverOnce(ctx)
        return nil
}

func sortStrings(s []string) {
        // Inline insertion sort to avoid importing sort for one call; the slice
        // is small (<= MaxHosts) and this is not on the hot path.
        for i := 1; i < len(s); i++ {
                for j := i; j > 0 && s[j-1] > s[j]; j-- {
                        s[j-1], s[j] = s[j], s[j-1]
                }
        }
}

// mapHost converts a client.Host to a state.Host, applying the design §16.3
// mapping rules. The collector is the boundary where tailkit types become
// Tailflow types.
func mapHost(h client.Host, streamInterval time.Duration) state.Host {
        dnsName := h.TSDNSName
        // Trim trailing dot per design §16.3.
        if len(dnsName) > 0 && dnsName[len(dnsName)-1] == '.' {
                dnsName = dnsName[:len(dnsName)-1]
        }
        return state.Host{
                Name:           h.Name,
                Role:           h.Role,
                Environment:    h.Environment,
                Tags:           append([]string(nil), h.Tags...),
                TSIPs:          append([]string(nil), h.TSIPs...),
                DNSName:        dnsName,
                OS:             h.OS,
                Online:         h.Online,
                StreamInterval: streamInterval,
        }
}

// errHostRemoved is returned by a supervisor's loop when its context is
// cancelled because the host disappeared. Used to distinguish "host gone"
// from "stream errored" in the supervisor's errgroup.Wait.
var errHostRemoved = errors.New("collector: host removed")
