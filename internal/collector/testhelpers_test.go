package collector

import (
        "io"
        "log/slog"
        "os"
)

// testLogger returns a slog.Logger that writes to io.Discard so tests don't
// spam stderr. Set TAILFLOW_TEST_LOG=1 to see the output for debugging.
func testLogger() *slog.Logger {
        if os.Getenv("TAILFLOW_TEST_LOG") != "" {
                return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
        }
        return slog.New(slog.NewTextHandler(io.Discard, nil))
}
