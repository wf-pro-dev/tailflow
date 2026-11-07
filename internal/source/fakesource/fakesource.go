// Package fakesource implements source.Fleet and source.Clock for tests.
//
// It builds real client.HostScope values on top of a scripted ClientBackend,
// so tailkit's real URL building and JSON/SSE decoding run in every test
// (design §8.1). Scripts are keyed by (hostname, method, path). Canned JSON
// is served for Do; canned bytes for DoRaw; channel-backed readers for Stream
// so tests can inject SSE frames, delays, mid-stream disconnects, and errors.
//
// This package is imported only by tests (including the e2e build) and by
// cmd/tailflow in local mode. It must not appear in production binaries.
package fakesource

import (
        "context"
        "errors"
        "fmt"
        "io"
        "net/http"
        "strings"
        "sync"
        "time"

        "github.com/wf-pro-dev/tailkit/client"

        "github.com/wf-pro-dev/tailflow/internal/source"
)

// ---------------------------------------------------------------------------
// Scripted backend
// ---------------------------------------------------------------------------

// Response is one canned entry in a Backend script. Exactly one of JSON, Raw,
// or Stream should be populated per (method, path). If Stream is non-nil, the
// caller receives an io.ReadCloser that drains the channel frame by frame.
type Response struct {
        // Status sets the HTTP status code that the tailkit error mapper will see.
        // Defaults to 200 when JSON or Raw is set; for error responses set this
        // and ErrorBody together so the mapper translates to the right sentinel.
        Status    int
        JSON      any   // marshalled with json.Marshal and returned by Do
        Raw       []byte // returned verbatim by DoRaw (and by Do when JSON is nil)
        ErrorBody string // body to send when Status >= 400 (decoded by tailkit's mapAPIError)

        // Stream returns a ReadCloser that emits the given SSE frames. Each frame
        // is written as `event: <name>\nid: <id>\ndata: <json>\n\n`. Use a buffered
        // channel and close it to end the stream; send an error to inject a
        // mid-stream failure.
        Stream func() (io.ReadCloser, error)
}

// Script maps (hostname, method, path) to a Response builder.
//
// The path key is the path tailkit asks the backend for, e.g.
// "/integrations/metrics/all/stream" or "/integrations/docker/containers".
// Tests build a Script with NewScript() and Add(host, method, path, resp).
//
// A wildcard host "*" matches any host that doesn't have an explicit entry.
type Script struct {
        mu sync.RWMutex
        // key: hostname + "\x00" + method + "\x00" + path
        entries map[string]func() Response
        // per-host list of (method, path) accesses, in order, for assertion
        accesses map[string][]access
}

type access struct {
        Method string
        Path   string
        At     time.Time
}

// NewScript returns an empty Script.
func NewScript() *Script {
        return &Script{
                entries:  make(map[string]func() Response),
                accesses: make(map[string][]access),
        }
}

// Add registers a static Response for the given (host, method, path).
// The Response is returned by value on every call.
func (s *Script) Add(host, method, path string, resp Response) *Script {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.entries[s.key(host, method, path)] = func() Response { return resp }
        return s
}

// AddFunc registers a Response builder that runs on every call. Use this when
// the canned response needs to mutate (for example, to advance a counter or
// pop the next SSE frame from a queue).
func (s *Script) AddFunc(host, method, path string, fn func() Response) *Script {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.entries[s.key(host, method, path)] = fn
        return s
}

// Accesses returns the recorded (method, path, time) accesses for one host,
// in order. Useful for asserting "exactly one Containers().List call".
func (s *Script) Accesses(host string) []access {
        s.mu.RLock()
        defer s.mu.RUnlock()
        out := make([]access, len(s.accesses[host]))
        copy(out, s.accesses[host])
        return out
}

func (s *Script) lookup(host, method, path string) (func() Response, bool) {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.accesses[host] = append(s.accesses[host], access{Method: method, Path: path, At: time.Now()})
        if fn, ok := s.entries[s.key(host, method, path)]; ok {
                return fn, true
        }
        if fn, ok := s.entries[s.key("*", method, path)]; ok {
                return fn, true
        }
        return nil, false
}

func (s *Script) key(host, method, path string) string {
        return host + "\x00" + method + "\x00" + path
}

// Backend implements client.ClientBackend against a Script. Build a HostScope
// with client.HostScope{Backend: backend, Hostname: name}.
type Backend struct {
        Script *Script

        // HostsList, PeersList, AgentsList override the corresponding methods.
        // Tests typically only set HostsList since Fleet.Discover uses a
        // Discoverer function rather than the backend.
        HostsList  []string
        PeersList  []string
        AgentsList []string
}

// Do implements client.ClientBackend. It looks up the canned Response,
// marshals JSON (or uses Raw), and translates HTTP status >= 400 to tailkit
// sentinel errors via the same map tailkit's real httpBackend uses.
func (b *Backend) Do(ctx context.Context, hostname, method, path string, body io.Reader, out any) error {
        resp, err := b.lookup(ctx, hostname, method, path)
        if err != nil {
                return err
        }
        if resp.Status >= 400 {
                return mapAPIError(resp.Status, resp.ErrorBody)
        }
        if resp.JSON != nil {
                return jsonMarshalInto(resp.JSON, out)
        }
        if len(resp.Raw) > 0 {
                return jsonUnmarshalInto(resp.Raw, out)
        }
        // 200 with no body — leave out as zero value.
        return nil
}

// DoRaw implements client.ClientBackend.
func (b *Backend) DoRaw(ctx context.Context, hostname, method, path string, body io.Reader, accept string) ([]byte, error) {
        resp, err := b.lookup(ctx, hostname, method, path)
        if err != nil {
                return nil, err
        }
        if resp.Status >= 400 {
                return nil, mapAPIError(resp.Status, resp.ErrorBody)
        }
        if len(resp.Raw) > 0 {
                return resp.Raw, nil
        }
        if resp.JSON != nil {
                return jsonMarshal(resp.JSON)
        }
        return nil, nil
}

// Stream implements client.ClientBackend.
func (b *Backend) Stream(ctx context.Context, hostname, method, path string) (io.ReadCloser, error) {
        resp, err := b.lookup(ctx, hostname, method, path)
        if err != nil {
                return nil, err
        }
        if resp.Status >= 400 {
                return nil, mapAPIError(resp.Status, resp.ErrorBody)
        }
        if resp.Stream == nil {
                return io.NopCloser(strings.NewReader("")), nil
        }
        return resp.Stream()
}

// Peers implements client.ClientBackend.
func (b *Backend) Peers(ctx context.Context, opts *client.PeerListOptions) ([]string, error) {
        return b.PeersList, nil
}

// Hosts implements client.ClientBackend.
func (b *Backend) Hosts(ctx context.Context, opts *client.HostListOptions) ([]string, error) {
        return b.HostsList, nil
}

// Agents implements client.ClientBackend.
func (b *Backend) Agents(ctx context.Context, opts *client.AgentListOptions) ([]string, error) {
        return b.AgentsList, nil
}

func (b *Backend) lookup(ctx context.Context, hostname, method, path string) (Response, error) {
        if b.Script == nil {
                return Response{}, fmt.Errorf("fakesource: script is nil")
        }
        fn, ok := b.Script.lookup(hostname, method, path)
        if !ok {
                return Response{}, &NotFoundError{Host: hostname, Method: method, Path: path}
        }
        return fn(), nil
}

// NotFoundError is returned when no script entry matches a (host, method, path).
// Tests should treat this as a test-authoring bug, not as a real upstream 404.
type NotFoundError struct {
        Host, Method, Path string
}

func (e *NotFoundError) Error() string {
        return fmt.Sprintf("fakesource: no script entry for %s %s on host %q", e.Method, e.Path, e.Host)
}

// ---------------------------------------------------------------------------
// Fleet implementation
// ---------------------------------------------------------------------------

// Fleet is a source.Fleet that returns client.HostScope values backed by a
// Backend. Discover returns a fixed list of client.Host values supplied at
// construction time.
type Fleet struct {
        Hosts []client.Host
        B     *Backend
}

// NewFleet returns a Fleet whose Discover returns the given hosts and whose
// Host(name) returns a HostScope over the given Backend.
func NewFleet(hosts []client.Host, b *Backend) *Fleet {
        return &Fleet{Hosts: hosts, B: b}
}

// Discover implements source.Fleet.
func (f *Fleet) Discover(ctx context.Context) ([]client.Host, error) {
        return f.Hosts, nil
}

// Host implements source.Fleet.
func (f *Fleet) Host(name string) client.HostScope {
        return client.HostScope{Backend: f.B, Hostname: name}
}

// Compile-time interface check.
var _ source.Fleet = (*Fleet)(nil)

// ---------------------------------------------------------------------------
// SSE helpers
// ---------------------------------------------------------------------------

// Frame is one SSE event. ID is optional (zero means "no id line").
type Frame struct {
        Event string
        ID    int64
        Data  any // marshalled to JSON; if []byte, used verbatim
}

// FrameChan returns a Stream func that builds an io.ReadCloser emitting the
// given frames one by one. The channel is drained in order; closing it ends
// the stream cleanly. Sending an error value (any error type) injects a
// mid-stream read error.
func FrameChan(frames <-chan any) func() (io.ReadCloser, error) {
        return func() (io.ReadCloser, error) {
                r, w := io.Pipe()
                go func() {
                        defer w.Close()
                        for v := range frames {
                                if err, ok := v.(error); ok {
                                        _ = w.CloseWithError(err)
                                        return
                                }
                                frame, ok := v.(Frame)
                                if !ok {
                                        _ = w.CloseWithError(fmt.Errorf("fakesource: bad frame type %T", v))
                                        return
                                }
                                if _, err := w.Write(SSEFrame(frame)); err != nil {
                                        return
                                }
                        }
                }()
                return r, nil
        }
}

// SSEFrame renders one Frame in SSE wire format: `event: <name>\nid: <id>\ndata: <json>\n\n`.
// The ID line is omitted when frame.ID is zero.
func SSEFrame(frame Frame) []byte {
        var b strings.Builder
        if frame.Event != "" {
                fmt.Fprintf(&b, "event: %s\n", frame.Event)
        }
        if frame.ID != 0 {
                fmt.Fprintf(&b, "id: %d\n", frame.ID)
        }
        var data []byte
        if raw, ok := frame.Data.([]byte); ok {
                data = raw
        } else if s, ok := frame.Data.(string); ok {
                data = []byte(s)
        } else if frame.Data != nil {
                data, _ = jsonMarshal(frame.Data)
        } else {
                data = []byte("{}")
        }
        // data: lines must not contain raw newlines; split on \n and prefix each.
        for _, line := range strings.Split(string(data), "\n") {
                fmt.Fprintf(&b, "data: %s\n", line)
        }
        b.WriteByte('\n')
        return []byte(b.String())
}

// ---------------------------------------------------------------------------
// Fake clock
// ---------------------------------------------------------------------------

// Clock is a source.Clock whose Now is advanced manually by tests. NewTimer
// returns a fake Timer that fires when the clock is advanced past its
// deadline; After is implemented in terms of NewTimer.
type Clock struct {
        mu     sync.Mutex
        now    time.Time
        timers []*fakeTimer
}

// NewClock returns a fake clock starting at the given time. Most tests use
// NewClockFixed(time.Unix(0, 0)) or NewClockAtRealStart().
func NewClock(now time.Time) *Clock {
        return &Clock{now: now}
}

// Now implements source.Clock.
func (c *Clock) Now() time.Time {
        c.mu.Lock()
        defer c.mu.Unlock()
        return c.now
}

// NewTimer implements source.Clock.
func (c *Clock) NewTimer(d time.Duration) source.Timer {
        c.mu.Lock()
        defer c.mu.Unlock()
        t := &fakeTimer{
                deadline: c.now.Add(d),
                c:        make(chan time.Time, 1),
        }
        c.timers = append(c.timers, t)
        return t
}

// After implements source.Clock.
func (c *Clock) After(d time.Duration) <-chan time.Time {
        return c.NewTimer(d).C()
}

// Advance moves the clock forward by d, firing any timer whose deadline has
// been reached in order. Timers fire exactly once.
func (c *Clock) Advance(d time.Duration) {
        c.mu.Lock()
        target := c.now.Add(d)
        c.now = target
        var pending []*fakeTimer
        for _, t := range c.timers {
                if t.fired || t.stopped {
                        continue
                }
                if !t.deadline.After(target) {
                        t.fired = true
                        select {
                        case t.c <- t.deadline:
                        default:
                        }
                } else {
                        pending = append(pending, t)
                }
        }
        c.timers = pending
        c.mu.Unlock()
}

// SetTime moves the clock to exactly t (must be >= Now). Useful for tests that
// want absolute timestamps rather than relative advances.
func (c *Clock) SetTime(t time.Time) {
        c.mu.Lock()
        defer c.mu.Unlock()
        if t.Before(c.now) {
                return
        }
        c.now = t
        var pending []*fakeTimer
        for _, tm := range c.timers {
                if tm.fired || tm.stopped {
                        continue
                }
                if !tm.deadline.After(t) {
                        tm.fired = true
                        select {
                        case tm.c <- tm.deadline:
                        default:
                        }
                } else {
                        pending = append(pending, tm)
                }
        }
        c.timers = pending
}

type fakeTimer struct {
        deadline time.Time
        c        chan time.Time
        fired    bool
        stopped  bool
        mu       sync.Mutex
}

func (t *fakeTimer) C() <-chan time.Time        { return t.c }
func (t *fakeTimer) Stop() bool {
        t.mu.Lock()
        defer t.mu.Unlock()
        was := !t.fired && !t.stopped
        t.stopped = true
        return was
}
func (t *fakeTimer) Reset(d time.Duration) bool {
        t.mu.Lock()
        defer t.mu.Unlock()
        was := !t.fired && !t.stopped
        t.fired = false
        t.stopped = false
        t.deadline = t.deadline.Add(d) // not strictly correct but good enough for tests
        return was
}

// Compile-time interface check.
var _ source.Clock = (*Clock)(nil)

// ---------------------------------------------------------------------------
// Error mapping (mirrors tailkit's internal mapAPIError)
// ---------------------------------------------------------------------------

// mapAPIError mirrors the tailkit client's HTTP-status → sentinel mapping
// (tailkit/client.go mapAPIError) so tests get the same errors.Is behaviour.
//
// We replicate it here rather than import the unexported function. If tailkit
// ever exports it, switch to the real one.
func mapAPIError(status int, body string) error {
        switch status {
        case http.StatusUnauthorized:
                return client.ErrUnauthorized
        case http.StatusForbidden:
                return client.ErrPermissionDenied
        case http.StatusConflict:
                return client.ErrConflict
        case http.StatusNotFound:
                // tailkit maps 404 from /integrations/* to ErrXUnavailable.
                if strings.Contains(body, "docker") {
                        return client.ErrDockerUnavailable
                }
                if strings.Contains(body, "metrics") {
                        return client.ErrMetricsUnavailable
                }
                if strings.Contains(body, "systemd") {
                        return client.ErrSystemdUnavailable
                }
                return client.ErrToolNotFound
        case http.StatusServiceUnavailable:
                // tailkitd returns 503 with body `{"error":"docker not configured on this node",...}`.
                if strings.Contains(body, "docker") {
                        return client.ErrDockerUnavailable
                }
                if strings.Contains(body, "metrics") {
                        return client.ErrMetricsUnavailable
                }
                if strings.Contains(body, "systemd") {
                        return client.ErrSystemdUnavailable
                }
                return errors.New(body)
        default:
                if body == "" {
                        return fmt.Errorf("upstream HTTP %d", status)
                }
                return errors.New(body)
        }
}
