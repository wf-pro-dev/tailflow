package config

import (
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.Mode != ModeLocal {
		t.Errorf("Default().Mode = %q, want %q", c.Mode, ModeLocal)
	}
	if c.HistoryWindow != time.Hour {
		t.Errorf("Default().HistoryWindow = %v, want 1h", c.HistoryWindow)
	}
	if c.StaleFactor != 3 {
		t.Errorf("Default().StaleFactor = %d, want 3", c.StaleFactor)
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("TAILFLOW_MODE", "local")
	// Force a clean environment so defaults apply for everything else.
	t.Setenv("TS_AUTHKEY", "")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Mode != ModeLocal {
		t.Errorf("Mode = %q, want local", c.Mode)
	}
	if c.HistoryStep != 5*time.Second {
		t.Errorf("HistoryStep = %v, want 5s", c.HistoryStep)
	}
}

func TestLoadValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string // substring of the joined error
	}{
		{
			name: "bad mode",
			env:  map[string]string{"TAILFLOW_MODE": "wat"},
			want: "TAILFLOW_MODE",
		},
		{
			name: "discovery interval below 5s",
			env:  map[string]string{"TAILFLOW_DISCOVERY_INTERVAL": "1s"},
			want: "TAILFLOW_DISCOVERY_INTERVAL",
		},
		{
			name: "history window out of range",
			env:  map[string]string{"TAILFLOW_HISTORY_WINDOW": "1m"},
			want: "TAILFLOW_HISTORY_WINDOW",
		},
		{
			name: "bad log level",
			env:  map[string]string{"TAILFLOW_LOG_LEVEL": "trace"},
			want: "TAILFLOW_LOG_LEVEL",
		},
		{
			name: "bad base path",
			env:  map[string]string{"TAILFLOW_BASE_PATH": "ui"},
			want: "TAILFLOW_BASE_PATH",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TAILFLOW_MODE", "local") // baseline valid
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if err == nil {
				t.Fatalf("Load: expected error containing %q, got nil", tc.want)
			}
			if !contains(err.Error(), tc.want) {
				t.Errorf("Load: error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
