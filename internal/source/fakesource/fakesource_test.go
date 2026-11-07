package fakesource

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/wf-pro-dev/tailkit/client"
)

// TestBackendDo verifies the canned JSON path of Backend.Do: a scripted
// Response with a JSON body lands in the typed out parameter.
func TestBackendDo(t *testing.T) {
	script := NewScript().Add("vps-1", "GET", "/host", Response{
		JSON: map[string]any{"name": "vps-1", "online": true},
	})
	b := &Backend{Script: script}

	var out map[string]any
	if err := b.Do(context.Background(), "vps-1", "GET", "/host", nil, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if out["name"] != "vps-1" {
		t.Errorf("out.name = %v, want vps-1", out["name"])
	}
}

// TestBackendDoNotFound verifies that an unscripted (host, method, path)
// returns *NotFoundError so test authors get a clear message.
func TestBackendDoNotFound(t *testing.T) {
	b := &Backend{Script: NewScript()}
	var out map[string]any
	err := b.Do(context.Background(), "vps-1", "GET", "/host", nil, &out)
	if err == nil {
		t.Fatal("Do: expected NotFoundError, got nil")
	}
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("Do: error type %T, want *NotFoundError", err)
	}
}

// TestBackendDoErrorMapping verifies that an HTTP 503 with a docker-not-
// configured body translates to client.ErrDockerUnavailable, mirroring
// tailkit's real httpBackend. This is the test-seam contract that lets
// collector tests use errors.Is(err, client.ErrDockerUnavailable) just like
// production code.
func TestBackendDoErrorMapping(t *testing.T) {
	script := NewScript().Add("vps-1", "GET", "/integrations/docker/containers", Response{
		Status:    503,
		ErrorBody: `{"error":"docker not configured on this node","hint":"create /etc/tailkitd/integrations/docker.toml to enable"}`,
	})
	b := &Backend{Script: script}

	var out any
	err := b.Do(context.Background(), "vps-1", "GET", "/integrations/docker/containers", nil, &out)
	if !errors.Is(err, client.ErrDockerUnavailable) {
		t.Fatalf("Do: err = %v, want errors.Is ErrDockerUnavailable", err)
	}
}

// TestBackendStreamFrames verifies that the channel-backed Stream func emits
// SSE frames that decode cleanly through SSEFrame's wire format. (The full
// "decode through tailkit's real SSE parser" test lands in M1 once the
// collector is in place; here we just check our own framing.)
func TestBackendStreamFrames(t *testing.T) {
	frames := make(chan any, 2)
	frames <- Frame{Event: "metrics.all", ID: 1, Data: map[string]any{"cpu_total": 12.4}}
	frames <- Frame{Event: "metrics.all", ID: 2, Data: map[string]any{"cpu_total": 15.1}}
	close(frames)

	script := NewScript().AddFunc("vps-1", "GET", "/integrations/metrics/all/stream", func() Response {
		return Response{Stream: FrameChan(frames)}
	})
	b := &Backend{Script: script}

	rc, err := b.Stream(context.Background(), "vps-1", "GET", "/integrations/metrics/all/stream")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	want := `event: metrics.all
id: 1
data: {"cpu_total":12.4}

event: metrics.all
id: 2
data: {"cpu_total":15.1}

`
	if string(got) != want {
		t.Errorf("Stream output mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFleetHostScope verifies that the source.Fleet implementation builds a
// real client.HostScope over the fake backend, exercising tailkit's actual
// struct (not a hand-rolled imitation). This is the design §8.1 contract.
func TestFleetHostScope(t *testing.T) {
	hosts := []client.Host{
		{Name: "vps-1", Online: true},
		{Name: "vps-2", Online: true},
	}
	b := &Backend{Script: NewScript()}
	f := NewFleet(hosts, b)

	got, err := f.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 2 || got[0].Name != "vps-1" || got[1].Name != "vps-2" {
		t.Errorf("Discover = %+v, want [vps-1, vps-2]", got)
	}

	scope := f.Host("vps-1")
	if scope.Hostname != "vps-1" {
		t.Errorf("Host(vps-1).Hostname = %q, want vps-1", scope.Hostname)
	}
	if scope.Backend == nil {
		t.Error("Host(vps-1).Backend is nil")
	}
}

// TestClockAdvance verifies the fake clock fires timers in deadline order.
func TestClockAdvance(t *testing.T) {
	c := NewClock(time.Unix(1000, 0))
	t1 := c.NewTimer(5 * time.Second)
	t2 := c.NewTimer(2 * time.Second)

	c.Advance(3 * time.Second) // t2 should fire (deadline 1002 <= 1003)
	select {
	case <-t2.C():
	default:
		t.Fatal("expected t2 to fire after advancing 3s")
	}
	select {
	case <-t1.C():
		t.Fatal("t1 should not have fired yet")
	default:
	}

	c.Advance(3 * time.Second) // t1 should fire (deadline 1005 <= 1006)
	select {
	case <-t1.C():
	default:
		t.Fatal("expected t1 to fire after advancing another 3s")
	}
}

// TestClockStop verifies Stop prevents firing.
func TestClockStop(t *testing.T) {
	c := NewClock(time.Unix(0, 0))
	t1 := c.NewTimer(5 * time.Second)
	if !t1.Stop() {
		t.Fatal("Stop returned false on a fresh timer")
	}
	c.Advance(10 * time.Second)
	select {
	case <-t1.C():
		t.Fatal("stopped timer should not fire")
	default:
	}
}

// TestAccesses verifies the script records accesses for assertion.
func TestAccesses(t *testing.T) {
	script := NewScript().Add("vps-1", "GET", "/host", Response{JSON: map[string]any{"name": "vps-1"}})
	b := &Backend{Script: script}

	var out map[string]any
	_ = b.Do(context.Background(), "vps-1", "GET", "/host", nil, &out)
	_ = b.Do(context.Background(), "vps-1", "GET", "/host", nil, &out)

	accesses := script.Accesses("vps-1")
	if len(accesses) != 2 {
		t.Fatalf("Accesses = %d, want 2", len(accesses))
	}
	if accesses[0].Path != "/host" {
		t.Errorf("Accesses[0].Path = %q, want /host", accesses[0].Path)
	}
}

// TestSSEFrameMultiLineData verifies that data containing a newline is split
// across multiple `data:` lines per the SSE spec.
func TestSSEFrameMultiLineData(t *testing.T) {
	frame := Frame{Event: "log.line", ID: 7, Data: map[string]any{"line": "hello\nworld"}}
	got := string(SSEFrame(frame))
	if !strings.Contains(got, "data: {\"line\":\"hello\\nworld\"}\n\n") {
		t.Errorf("SSEFrame output missing expected data line:\n%s", got)
	}
}
