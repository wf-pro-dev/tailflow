// Package config parses and validates Tailflow's environment-variable
// configuration (design §15). All values are validated at startup and every
// error is reported at once before exiting with status 2.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Mode is the runtime mode of the Tailflow process.
type Mode string

const (
	// ModeTailnet serves over tsnet (production).
	ModeTailnet Mode = "tailnet"
	// ModeLocal serves plain HTTP on 127.0.0.1 (development and tests).
	ModeLocal Mode = "local"
)

// Config is the validated Tailflow configuration. Fields are populated from
// environment variables per the table in design §15.
type Config struct {
	Mode             Mode
	Hostname         string
	AuthKey          string
	StateDir         string
	Listen           string
	BasePath         string
	DiscoveryInterval time.Duration
	HistoryWindow    time.Duration
	HistoryStep      time.Duration
	CallTimeout      time.Duration
	StaleFactor      int
	DegradeThreshold int
	MaxHosts         int
	MaxSSEClients    int
	LogLevel         string
	LogFormat        string
	HealthAddr       string
}

// Default returns the Config with all defaults applied, suitable for `make dev`
// in local mode. Production callers should call Load() instead.
func Default() Config {
	return Config{
		Mode:              ModeLocal,
		Hostname:          "tailflow",
		AuthKey:           "",
		StateDir:          "/var/lib/tailflow",
		Listen:            ":8080",
		BasePath:          "/",
		DiscoveryInterval: 15 * time.Second,
		HistoryWindow:     1 * time.Hour,
		HistoryStep:       5 * time.Second,
		CallTimeout:       10 * time.Second,
		StaleFactor:       3,
		DegradeThreshold:  3,
		MaxHosts:          500,
		MaxSSEClients:     50,
		LogLevel:          "info",
		LogFormat:         "json",
		HealthAddr:        "127.0.0.1:8081",
	}
}

// Load reads the environment, validates it, and returns a Config or an error
// listing every problem at once (design §15 "Startup contract").
func Load() (Config, error) {
	c := Default()

	c.Mode = Mode(envOr("TAILFLOW_MODE", string(c.Mode)))
	c.Hostname = envOr("TAILFLOW_HOSTNAME", c.Hostname)
	c.AuthKey = os.Getenv("TS_AUTHKEY")
	c.StateDir = envOr("TS_STATE_DIR", c.StateDir)
	c.Listen = envOr("TAILFLOW_LISTEN", c.Listen)
	c.BasePath = envOr("TAILFLOW_BASE_PATH", c.BasePath)
	c.LogLevel = envOr("TAILFLOW_LOG_LEVEL", c.LogLevel)
	c.LogFormat = envOr("TAILFLOW_LOG_FORMAT", c.LogFormat)
	c.HealthAddr = envOr("TAILFLOW_HEALTH_ADDR", c.HealthAddr)

	var errs []error

	if c.Mode != ModeTailnet && c.Mode != ModeLocal {
		errs = append(errs, fmt.Errorf("TAILFLOW_MODE must be %q or %q, got %q", ModeTailnet, ModeLocal, c.Mode))
	}

	if c.Mode == ModeTailnet && c.AuthKey == "" {
		// tsnet accepts an empty auth key when state already exists, so this
		// is a warning rather than a hard error. We allow it but log on startup.
	}

	if v, err := envDuration("TAILFLOW_DISCOVERY_INTERVAL", c.DiscoveryInterval); err != nil {
		errs = append(errs, fmt.Errorf("TAILFLOW_DISCOVERY_INTERVAL: %w", err))
	} else if v < 5*time.Second {
		errs = append(errs, fmt.Errorf("TAILFLOW_DISCOVERY_INTERVAL must be >= 5s, got %s", v))
	} else {
		c.DiscoveryInterval = v
	}

	if v, err := envDuration("TAILFLOW_HISTORY_WINDOW", c.HistoryWindow); err != nil {
		errs = append(errs, fmt.Errorf("TAILFLOW_HISTORY_WINDOW: %w", err))
	} else if v < 5*time.Minute || v > 6*time.Hour {
		errs = append(errs, fmt.Errorf("TAILFLOW_HISTORY_WINDOW must be between 5m and 6h, got %s", v))
	} else {
		c.HistoryWindow = v
	}

	if v, err := envDuration("TAILFLOW_HISTORY_STEP", c.HistoryStep); err != nil {
		errs = append(errs, fmt.Errorf("TAILFLOW_HISTORY_STEP: %w", err))
	} else if v <= 0 {
		errs = append(errs, fmt.Errorf("TAILFLOW_HISTORY_STEP must be > 0, got %s", v))
	} else {
		c.HistoryStep = v
	}

	if v, err := envDuration("TAILFLOW_CALL_TIMEOUT", c.CallTimeout); err != nil {
		errs = append(errs, fmt.Errorf("TAILFLOW_CALL_TIMEOUT: %w", err))
	} else {
		c.CallTimeout = v
	}

	if v, err := envInt("TAILFLOW_STALE_FACTOR", c.StaleFactor); err != nil {
		errs = append(errs, fmt.Errorf("TAILFLOW_STALE_FACTOR: %w", err))
	} else if v < 1 {
		errs = append(errs, fmt.Errorf("TAILFLOW_STALE_FACTOR must be >= 1, got %d", v))
	} else {
		c.StaleFactor = v
	}

	if v, err := envInt("TAILFLOW_DEGRADE_THRESHOLD", c.DegradeThreshold); err != nil {
		errs = append(errs, fmt.Errorf("TAILFLOW_DEGRADE_THRESHOLD: %w", err))
	} else if v < 1 {
		errs = append(errs, fmt.Errorf("TAILFLOW_DEGRADE_THRESHOLD must be >= 1, got %d", v))
	} else {
		c.DegradeThreshold = v
	}

	if v, err := envInt("TAILFLOW_MAX_HOSTS", c.MaxHosts); err != nil {
		errs = append(errs, fmt.Errorf("TAILFLOW_MAX_HOSTS: %w", err))
	} else if v < 1 {
		errs = append(errs, fmt.Errorf("TAILFLOW_MAX_HOSTS must be >= 1, got %d", v))
	} else {
		c.MaxHosts = v
	}

	if v, err := envInt("TAILFLOW_MAX_SSE_CLIENTS", c.MaxSSEClients); err != nil {
		errs = append(errs, fmt.Errorf("TAILFLOW_MAX_SSE_CLIENTS: %w", err))
	} else if v < 1 {
		errs = append(errs, fmt.Errorf("TAILFLOW_MAX_SSE_CLIENTS must be >= 1, got %d", v))
	} else {
		c.MaxSSEClients = v
	}

	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("TAILFLOW_LOG_LEVEL must be one of debug, info, warn, error; got %q", c.LogLevel))
	}

	switch strings.ToLower(c.LogFormat) {
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("TAILFLOW_LOG_FORMAT must be json or text; got %q", c.LogFormat))
	}

	if !strings.HasPrefix(c.BasePath, "/") {
		errs = append(errs, fmt.Errorf("TAILFLOW_BASE_PATH must start with /, got %q", c.BasePath))
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// env helpers
// ---------------------------------------------------------------------------

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("not a duration (%q): %w", v, err)
	}
	return d, nil
}

func envInt(key string, def int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def, nil
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return 0, fmt.Errorf("not an int (%q): %w", v, err)
	}
	return n, nil
}
