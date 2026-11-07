// Package source defines the seam between Tailflow and the tailnet.
//
// This is the only package (besides internal/source/tailkitsrc and cmd/tailflow)
// that knows how Tailflow reaches remote hosts. The rest of Tailflow depends on
// these interfaces, which makes the whole aggregator testable without a real
// tailnet (see internal/source/fakesource) and lets the same code later move
// into tailkitd as an in-process local fleet (design §20).
//
// Import graph (enforced by .golangci.yml depguard):
//
//	internal/source            -> tailkit/client (interfaces only)
//	internal/source/tailkitsrc -> tailkit (root)         [production only]
//	internal/source/fakesource -> tailkit/client          [tests only]
//	internal/collector         -> tailkit/client
//	internal/state, internal/hub, internal/api -> neither
//	cmd/tailflow               -> tailkit (root)
//
// See design §4.1 and §16.1 for the rationale.
package source

import (
	"context"
	"time"

	"github.com/wf-pro-dev/tailkit/client"
)

// Fleet is the only way the rest of Tailflow reaches the tailnet.
//
// Production implementations build real tailkit scopes; the fake
// implementation in internal/source/fakesource builds scopes on top of a
// scripted ClientBackend so tests exercise tailkit's real URL building and
// JSON/SSE decoding instead of a hand-written imitation.
type Fleet interface {
	// Discover lists online hosts.
	//
	// Production: tailkit.ListHosts(ctx, srv, &client.HostListOptions{Online: true}).
	// Fake: returns a scripted list (the "Discoverer" of design §8.1).
	//
	// Note: tailkit.ListHosts has a nil-deref bug at peers.go:68 if opts is nil,
	// so production callers MUST pass a non-nil &client.HostListOptions{Online: true}.
	Discover(ctx context.Context) ([]client.Host, error)

	// Host returns a scope bound to one host.
	//
	// Production: tailkit.NewClient(srv).Host(name) → client.HostScope.
	// Fake: client.HostScope{Backend: fakeBackend, Hostname: name}.
	Host(name string) client.HostScope
}

// Discoverer is the function shape of Fleet.Discover, extracted so tests can
// inject a fixture without standing up a full Fleet implementation. The
// production Discoverer is tailkit.ListHosts bound to a live *tailkit.Server;
// it cannot live in the Fleet interface because tailkit.ListHosts needs the
// server, and we want internal/source to import only tailkit/client.
type Discoverer func(ctx context.Context) ([]client.Host, error)

// Clock is the abstract time source used by the collector and aggregator so
// tests can drive backoff schedules, debounce windows, and bucket boundaries
// deterministically. Production code uses SystemClock; tests use fakesource.
type Clock interface {
	// Now returns the current time.
	Now() time.Time

	// NewTimer returns a Timer that fires once after d. Reset and Stop follow
	// the same semantics as time.Timer.
	NewTimer(d time.Duration) Timer

	// After fires after d. Implemented in terms of NewTimer so fakes only need
	// to override NewTimer.
	After(d time.Duration) <-chan time.Time
}

// Timer is the abstract time.Timer used by Clock. Tests provide a fake that
// fires when the test advances the clock.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// SystemClock is the production Clock, wrapping the time package.
type SystemClock struct{}

// Now implements Clock.
func (SystemClock) Now() time.Time { return time.Now() }

// NewTimer implements Clock.
func (SystemClock) NewTimer(d time.Duration) Timer {
	t := time.NewTimer(d)
	return &systemTimer{t: t}
}

// After implements Clock.
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type systemTimer struct{ t *time.Timer }

func (s *systemTimer) C() <-chan time.Time     { return s.t.C }
func (s *systemTimer) Stop() bool              { return s.t.Stop() }
func (s *systemTimer) Reset(d time.Duration) bool { return s.t.Reset(d) }
