// Command tailflow is the entrypoint for the Tailflow v1 server.
//
// In M0 it supports two subcommands:
//
//      serve       Start the HTTP server (tailnet or local mode per config)
//      healthcheck Hit the loopback health listener and exit 0/1
//
// The discovery loop, hub, and full API contract land in M1+; for now `serve`
// starts the listener and serves /api/v1/healthz and /api/v1/meta plus the SPA
// fallback so the build is runnable and `make dev` shows something.
//
// Design refs: §15 (config), §19.1 (developer workflow), §19.2 (health check).
package main

import (
        "context"
        "errors"
        "flag"
        "fmt"
        "log/slog"
        "net/http"
        "os"
        "os/signal"
        "strings"
        "syscall"
        "time"

        "github.com/wf-pro-dev/tailkit"

        "github.com/wf-pro-dev/tailflow/internal/api"
        "github.com/wf-pro-dev/tailflow/internal/collector"
        "github.com/wf-pro-dev/tailflow/internal/config"
        hubpkg "github.com/wf-pro-dev/tailflow/internal/hub"
        "github.com/wf-pro-dev/tailflow/internal/source"
        "github.com/wf-pro-dev/tailflow/internal/source/fakesource"
        "github.com/wf-pro-dev/tailflow/internal/state"
        "github.com/wf-pro-dev/tailflow/web"
)

// hubAdapter bridges state.HubPublisher to hub.Hub. We need this because
// internal/state cannot import internal/hub (depguard rule: state imports
// neither tailkit nor hub). The adapter lives here in cmd/tailflow where
// both packages are visible.
type hubAdapter struct {
        hub *hubpkg.Hub
}

func (a hubAdapter) Publish(e state.HubEvent) {
        a.hub.Publish(hubpkg.HubEvent{
                Topic: e.Topic,
                Name:  e.Name,
                Data:  e.Data,
        })
}

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

// tailkitVersion is the pinned tailkit dependency. Hard-coded to match go.mod;
// when we bump go.mod, update this string in the same change.
const tailkitVersion = "v0.5.4"

func main() {
        if err := run(os.Args[1:]); err != nil {
                fmt.Fprintf(os.Stderr, "tailflow: %v\n", err)
                os.Exit(2)
        }
}

func run(args []string) error {
        if len(args) == 0 {
                return errors.New("no subcommand; expected 'serve' or 'healthcheck'")
        }
        switch args[0] {
        case "serve":
                return cmdServe(args[1:])
        case "healthcheck":
                return cmdHealthcheck(args[1:])
        case "version":
                fmt.Printf("tailflow %s (tailkit %s)\n", version, tailkitVersion)
                return nil
        case "-h", "--help", "help":
                printUsage(os.Stdout)
                return nil
        default:
                printUsage(os.Stderr)
                return fmt.Errorf("unknown subcommand %q", args[0])
        }
}

func printUsage(w *os.File) {
        fmt.Fprintln(w, "Usage: tailflow <subcommand> [flags]")
        fmt.Fprintln(w, "")
        fmt.Fprintln(w, "Subcommands:")
        fmt.Fprintln(w, "  serve       Start the HTTP server (tailnet or local mode per config)")
        fmt.Fprintln(w, "  healthcheck Hit the loopback health listener and exit 0/1")
        fmt.Fprintln(w, "  version     Print version and exit")
}

// cmdServe implements `tailflow serve`. It loads config, builds the logger,
// starts the tsnet or local listener, mounts the API handler, and blocks on
// SIGINT/SIGTERM with a 10 s graceful shutdown (design §15 startup contract).
func cmdServe(args []string) error {
        fs := flag.NewFlagSet("serve", flag.ContinueOnError)
        _ = fs.Parse(args)

        cfg, err := config.Load()
        if err != nil {
                return fmt.Errorf("config: %w", err)
        }

        logger := newLogger(cfg.LogLevel, cfg.LogFormat)
        logger.Info("tailflow starting",
                "version", version,
                "tailkit_version", tailkitVersion,
                "mode", cfg.Mode,
                "hostname", cfg.Hostname,
                "listen", cfg.Listen,
                "base_path", cfg.BasePath,
        )

        bufferCfg := api.BufferConfig{
                HistoryWindow:  cfg.HistoryWindow.String(),
                HistoryStep:    cfg.HistoryStep.String(),
                StreamInterval: "2s", // Until U1 ships (design D2 / README gap #1)
                StaleFactor:    cfg.StaleFactor,
        }

        // In-memory state store. Ring capacity = window / step (default 720 for 1h/5s).
        ringCapacity := int(cfg.HistoryWindow / cfg.HistoryStep)
        if ringCapacity < 1 {
                ringCapacity = 720
        }
        store := state.New(ringCapacity)

        // Hub for SSE fan-out (M2 will wire the /api/v1/stream endpoint; for M1
        // we just construct it so the store can publish events).
        hub := hubpkg.NewHub()
        store.SetHub(hubAdapter{hub: hub})

        // Build the Fleet: fakesource in local mode (M1 has no real tailnet
        // wiring yet — that needs tsnet which lands with the integration tests),
        // tailkitsrc in tailnet mode.
        var fleet source.Fleet
        switch cfg.Mode {
        case config.ModeLocal:
                logger.Warn("local mode: using empty fakesource fleet; no hosts will appear. Set TAILFLOW_MODE=tailnet once tsnet is wired.")
                fleet = fakesource.NewFleet(nil, &fakesource.Backend{Script: fakesource.NewScript()})
        case config.ModeTailnet:
                // tsnet wiring lands in M2 with the integration test harness.
                // For M1 we fall back to fakesource so the build is runnable.
                logger.Warn("tailnet mode: tsnet wiring not yet implemented in M1; using empty fakesource fleet as placeholder")
                fleet = fakesource.NewFleet(nil, &fakesource.Backend{Script: fakesource.NewScript()})
        }

        // Collector runs the discovery loop and per-host supervisors.
        collectorCfg := collector.Config{
                DiscoveryInterval: cfg.DiscoveryInterval,
                HistoryStep:       cfg.HistoryStep,
                CallTimeout:       cfg.CallTimeout,
                StaleFactor:       cfg.StaleFactor,
                DegradeThreshold:  cfg.DegradeThreshold,
                MaxHosts:          cfg.MaxHosts,
        }
        col := collector.New(fleet, store, source.SystemClock{}, logger, collectorCfg)
        collectorCtx, cancelCollector := context.WithCancel(context.Background())
        go func() {
                if err := col.Run(collectorCtx); err != nil && !errors.Is(err, context.Canceled) {
                        logger.Error("collector exited", "err", err)
                }
        }()

        handler := api.NewHandler(cfg.BasePath, version, tailkitVersion, cfg, store, hub, bufferCfg, http.FS(web.DistFS()))

        // Health listener: always loopback, even in tailnet mode, so the
        // container health check can reach it without going over the tailnet
        // (design §19.2). Serves only /api/v1/healthz.
        healthMux := http.NewServeMux()
        healthMux.HandleFunc("/api/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"status":"ok"}`))
        })
        healthSrv := &http.Server{
                Addr:              cfg.HealthAddr,
                Handler:           healthMux,
                ReadHeaderTimeout: 5 * time.Second,
        }
        go func() {
                logger.Info("health listener starting", "addr", cfg.HealthAddr)
                if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
                        logger.Error("health listener failed", "err", err)
                }
        }()

        // Main listener. In local mode (the default for `make dev`) we serve
        // plain HTTP on 127.0.0.1 or whatever TAILFLOW_LISTEN is set to. In
        // tailnet mode we serve over tsnet (TODO M1: wire the tsnet server;
        // for M0 we serve plain HTTP so the build is runnable end-to-end).
        if cfg.Mode == config.ModeTailnet {
                logger.Warn("tailnet mode is not yet wired in M0; serving plain HTTP on the configured listener. tsnet integration lands in M1.")
        }

        srv := &http.Server{
                Addr:              cfg.Listen,
                Handler:           handler,
                ReadHeaderTimeout: 5 * time.Second,
        }

        ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
        defer stop()

        go func() {
                logger.Info("main listener starting", "addr", cfg.Listen)
                if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
                        logger.Error("main listener failed", "err", err)
                        stop()
                }
        }()

        <-ctx.Done()
        logger.Info("shutdown signal received; draining for up to 10s")

        // Stop the collector first so it stops writing to the store.
        cancelCollector()

        shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        if err := srv.Shutdown(shutdownCtx); err != nil {
                logger.Error("graceful shutdown failed", "err", err)
        }
        if err := healthSrv.Shutdown(shutdownCtx); err != nil {
                logger.Error("health listener shutdown failed", "err", err)
        }
        logger.Info("tailflow stopped")
        return nil
}

// cmdHealthcheck implements `tailflow healthcheck`. It hits the loopback
// health listener and exits 0 on 200, 1 otherwise. Used as the container
// HEALTHCHECK (design §19.2).
func cmdHealthcheck(args []string) error {
        fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
        addr := fs.String("addr", "127.0.0.1:8081", "loopback health listener address")
        _ = fs.Parse(args)

        client := &http.Client{Timeout: 3 * time.Second}
        url := "http://" + strings.TrimPrefix(*addr, "http://") + "/api/v1/healthz"
        resp, err := client.Get(url)
        if err != nil {
                fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
                return errors.New("unhealthy")
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
                fmt.Fprintf(os.Stderr, "healthcheck: status %d\n", resp.StatusCode)
                return errors.New("unhealthy")
        }
        return nil
}

// newLogger builds a slog.Logger at the configured level and format.
// Designed to be swapped for tailkitd's zap when Tailflow is embedded (§20).
func newLogger(level, format string) *slog.Logger {
        var lvl slog.Level
        switch strings.ToLower(level) {
        case "debug":
                lvl = slog.LevelDebug
        case "info":
                lvl = slog.LevelInfo
        case "warn":
                lvl = slog.LevelWarn
        case "error":
                lvl = slog.LevelError
        }

        opts := &slog.HandlerOptions{Level: lvl}
        var h slog.Handler
        if strings.ToLower(format) == "text" {
                h = slog.NewTextHandler(os.Stdout, opts)
        } else {
                h = slog.NewJSONHandler(os.Stdout, opts)
        }
        return slog.New(h)
}

// Compile-time check that we depend on tailkit (the depguard rule allows
// cmd/tailflow to import the root package; this is the integration point that
// will become the tsnet server in M1). For M0 the import is unused but kept
// to validate the dependency resolves.
var _ = tailkit.NewServer
