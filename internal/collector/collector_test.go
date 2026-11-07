package collector

import (
        "context"
        "testing"
        "time"

        "github.com/wf-pro-dev/tailkit/client"

        "github.com/wf-pro-dev/tailflow/internal/source"
        "github.com/wf-pro-dev/tailflow/internal/source/fakesource"
        "github.com/wf-pro-dev/tailflow/internal/state"
)

// testClock wraps fakesource.Clock to satisfy source.Clock. We need this
// because the collector takes source.Clock, and fakesource.Clock is the
// concrete fake. The wrapper just embeds the fake and forwards.
type testClock struct {
        *fakesource.Clock
}

func (t testClock) NewTimer(d time.Duration) source.Timer {
        return t.Clock.NewTimer(d)
}

// F1-01 (component): A new online host appears in Discover → supervisor
// starts within one interval, host.updated emitted, host appears in store.
func TestF1_01_NewHostAppears(t *testing.T) {
        store := state.New(720)
        clk := testClock{Clock: fakesource.NewClock(time.Unix(1000, 0))}
        logger := testLogger()

        // Start with no hosts; then add one.
        hosts := []client.Host{}
        b := &fakesource.Backend{Script: fakesource.NewScript()}
        fleet := fakesource.NewFleet(hosts, b)

        cfg := Config{
                DiscoveryInterval: 5 * time.Second,
                HistoryStep:       5 * time.Second,
                CallTimeout:       2 * time.Second,
                StaleFactor:       3,
                DegradeThreshold:  3,
                MaxHosts:          100,
        }
        col := New(fleet, store, clk, logger, cfg)

        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()
        go col.Run(ctx)

        // Wait for the first discovery cycle (immediate) to complete.
        time.Sleep(100 * time.Millisecond)
        if got := len(store.Hosts()); got != 0 {
                t.Fatalf("initial Hosts() = %d, want 0", got)
        }

        // Add a host and trigger discovery by advancing the clock past the
        // discovery interval.
        newHost := client.Host{Name: "vps-1", Online: true}
        fleet.SetHosts([]client.Host{newHost})
        // Add a script entry for the host's capability probe (Ports() returns
        // ErrMetricsUnavailable so the host has no metrics capability).
        b.Script.Add("vps-1", "GET", "/integrations/metrics/ports", fakesource.Response{
                Status:    503,
                ErrorBody: `{"error":"metrics not configured"}`,
        })
        b.Script.Add("vps-1", "GET", "/docker/config", fakesource.Response{
                Status:    503,
                ErrorBody: `{"error":"docker not configured"}`,
        })

        clk.Advance(cfg.DiscoveryInterval)
        time.Sleep(200 * time.Millisecond)

        hosts2 := store.Hosts()
        if len(hosts2) != 1 {
                t.Fatalf("after discovery: Hosts() = %d, want 1", len(hosts2))
        }
        if hosts2[0].Name != "vps-1" {
                t.Errorf("host name = %q, want vps-1", hosts2[0].Name)
        }
}

// F1-02 (component): A host disappears → supervisor context cancelled,
// host.removed emitted, host gone from store.
func TestF1_02_HostDisappears(t *testing.T) {
        store := state.New(720)
        clk := testClock{Clock: fakesource.NewClock(time.Unix(1000, 0))}
        logger := testLogger()

        host := client.Host{Name: "vps-1", Online: true}
        b := &fakesource.Backend{Script: fakesource.NewScript()}
        b.Script.Add("vps-1", "GET", "/integrations/metrics/ports", fakesource.Response{
                Status: 503, ErrorBody: `{"error":"metrics not configured"}`,
        })
        b.Script.Add("vps-1", "GET", "/docker/config", fakesource.Response{
                Status: 503, ErrorBody: `{"error":"docker not configured"}`,
        })
        fleet := fakesource.NewFleet([]client.Host{host}, b)

        cfg := Config{DiscoveryInterval: 5 * time.Second, HistoryStep: 5 * time.Second, CallTimeout: 2 * time.Second, StaleFactor: 3, DegradeThreshold: 3, MaxHosts: 100}
        col := New(fleet, store, clk, logger, cfg)

        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()
        go col.Run(ctx)

        // Wait for initial discovery.
        time.Sleep(200 * time.Millisecond)
        if len(store.Hosts()) != 1 {
                t.Fatalf("initial: Hosts() = %d, want 1", len(store.Hosts()))
        }

        // Remove the host and advance the clock.
        fleet.SetHosts(nil)
        clk.Advance(cfg.DiscoveryInterval)
        time.Sleep(200 * time.Millisecond)

        if len(store.Hosts()) != 0 {
                t.Fatalf("after removal: Hosts() = %d, want 0", len(store.Hosts()))
        }
}

// F1-03 (component): Discover returns an unchanged list → zero events, host
// stays in store.
func TestF1_03_UnchangedListNoEvents(t *testing.T) {
        store := state.New(720)
        clk := testClock{Clock: fakesource.NewClock(time.Unix(1000, 0))}

        host := client.Host{Name: "vps-1", Online: true}
        b := &fakesource.Backend{Script: fakesource.NewScript()}
        b.Script.Add("vps-1", "GET", "/integrations/metrics/ports", fakesource.Response{Status: 503, ErrorBody: `{"error":"metrics not configured"}`})
        b.Script.Add("vps-1", "GET", "/docker/config", fakesource.Response{Status: 503, ErrorBody: `{"error":"docker not configured"}`})
        fleet := fakesource.NewFleet([]client.Host{host}, b)

        cfg := Config{DiscoveryInterval: 5 * time.Second, HistoryStep: 5 * time.Second, CallTimeout: 2 * time.Second, StaleFactor: 3, DegradeThreshold: 3, MaxHosts: 100}
        col := New(fleet, store, clk, testLogger(), cfg)

        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()
        go col.Run(ctx)

        time.Sleep(200 * time.Millisecond)
        // Advance twice; host should still be there.
        clk.Advance(cfg.DiscoveryInterval)
        time.Sleep(100 * time.Millisecond)
        clk.Advance(cfg.DiscoveryInterval)
        time.Sleep(100 * time.Millisecond)

        if len(store.Hosts()) != 1 {
                t.Errorf("Hosts() = %d, want 1 (host should persist across unchanged discoveries)", len(store.Hosts()))
        }
}

// F6-02 (unit): Docker event actions exec_start, attach, top are ignored.
func TestF6_02_NoisyEventsIgnored(t *testing.T) {
        ignored := []string{"exec_start", "exec_create", "exec_die", "attach", "detach", "top", "resize", "archive-path", "extract-to-dir"}
        for _, action := range ignored {
                if shouldRefreshDockerInventory(action) {
                        t.Errorf("shouldRefreshDockerInventory(%q) = true, want false", action)
                }
        }
        accepted := []string{"create", "start", "restart", "stop", "kill", "die", "oom", "pause", "unpause", "update", "rename", "destroy", "remove", "health_status: healthy"}
        for _, action := range accepted {
                if !shouldRefreshDockerInventory(action) {
                        t.Errorf("shouldRefreshDockerInventory(%q) = false, want true", action)
                }
        }
}

// mapHost smoke test: confirm the tailkit → state mapping trims the trailing
// dot from TSDNSName and copies tags/TSIPs.
func TestMapHost(t *testing.T) {
        h := client.Host{
                Name:      "vps-1",
                Role:      "web",
                TSDNSName: "vps-1.tailnet.ts.net.",
                Tags:      []string{"edge", "prod"},
                TSIPs:     []string{"100.64.0.5"},
                OS:        "linux",
                Online:    true,
        }
        got := mapHost(h, 2*time.Second)
        if got.DNSName != "vps-1.tailnet.ts.net" {
                t.Errorf("DNSName = %q, want trailing dot trimmed", got.DNSName)
        }
        if len(got.Tags) != 2 || got.Tags[0] != "edge" {
                t.Errorf("Tags = %v, want [edge prod]", got.Tags)
        }
        if got.StreamInterval != 2*time.Second {
                t.Errorf("StreamInterval = %v, want 2s", got.StreamInterval)
        }
}
