package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/wf-pro-dev/tailflow/internal/api"
	"github.com/wf-pro-dev/tailflow/internal/collector"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	tailruntime "github.com/wf-pro-dev/tailflow/internal/runtime"
	"github.com/wf-pro-dev/tailflow/internal/store"
	"github.com/wf-pro-dev/tailflow/internal/topology"
	"github.com/wf-pro-dev/tailkit"
)

type config struct {
	Hostname        string
	ListenAddr      string
	DBPath          string
	StateDir        string
	Ephemeral       bool
	NodeTimeout     time.Duration
	DisableWatchers bool
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	core.InitLogLevelFromEnv()

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("tailflow: %v", err)
	}
}

func run(ctx context.Context, cfg config) error {
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return err
	}

	sqliteStore, err := store.OpenSQLite(cfg.DBPath)
	if err != nil {
		return err
	}

	ts, err := tailkit.NewServer(tailkit.ServerConfig{
		Hostname:  cfg.Hostname,
		StateDir:  cfg.StateDir,
		Ephemeral: cfg.Ephemeral,
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = ts.Close()
	}()

	bus := core.NewEventBus()
	parsers := parser.NewRegistry()
	collectorSvc := collector.NewCollector(ts, sqliteStore.ProxyConfigs(), bus, parsers)
	collectorSvc.SetNodeTimeout(cfg.NodeTimeout)
	topologySvc := topology.NewManager()
	runtimeSvc := tailruntime.New(
		tailruntime.Config{
			DisableWatchers: cfg.DisableWatchers,
		},
		collectorSvc,
		topologySvc,
		bus,
	)

	apiHandler := api.NewHandler(
		sqliteStore.ProxyConfigs(),
		collectorSvc,
		topologySvc,
		runtimeSvc,
		bus,
		parsers,
	)
	httpHandler := withCORS(apiHandler)
	httpServer := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: httpHandler,
	}

	errCh := make(chan error, 3)
	go func() {
		if err := runtimeSvc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- err
		}
	}()

	go func() {
		log.Printf("tailflow: serving on http %s", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	go func() {
		log.Printf("tailflow: serving on tailnet %s%s", cfg.Hostname, cfg.ListenAddr)
		if err := ts.ListenAndServe(cfg.ListenAddr, httpHandler); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func loadConfig() (config, error) {
	hostname := envOr("TAILFLOW_HOSTNAME", "tailflow")
	listenAddr := envOr("TAILFLOW_LISTEN_ADDR", ":8080")
	dbPath := envOr("TAILFLOW_DB_PATH", "tailflow.sqlite3")
	stateDir := os.Getenv("TAILFLOW_STATE_DIR")

	ephemeral := envBool("TAILFLOW_EPHEMERAL", false)
	disableWatchers := envBool("TAILFLOW_DISABLE_WATCHERS", false)
	nodeTimeout, err := envDuration("TAILFLOW_NODE_TIMEOUT", 10*time.Second)
	if err != nil {
		return config{}, err
	}

	return config{
		Hostname:        hostname,
		ListenAddr:      listenAddr,
		DBPath:          dbPath,
		StateDir:        stateDir,
		Ephemeral:       ephemeral,
		NodeTimeout:     nodeTimeout,
		DisableWatchers: disableWatchers,
	}, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Last-Event-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
