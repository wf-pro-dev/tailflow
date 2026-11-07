// Package tailkitsrc is the production source.Fleet implementation, wrapping
// tailkit.NewClient(srv).Host(name). It is one of only two packages that
// import the root tailkit package (the other is cmd/tailflow) — design §F10.
//
// The discovery function uses tailkit.ListHosts, which requires a live
// *tailkit.Server. We hold the server here and pass it to ListHosts.

package tailkitsrc

import (
	"context"
	"log/slog"

	"github.com/wf-pro-dev/tailkit"
	"github.com/wf-pro-dev/tailkit/client"

	"github.com/wf-pro-dev/tailflow/internal/source"
)

// Fleet is the production source.Fleet, backed by a real tailkit.Server.
type Fleet struct {
	srv    *tailkit.Server
	tc     client.TailnetClient
	logger *slog.Logger
}

// New returns a Fleet that reaches the tailnet through the given server.
// The server must already be started (tailkit.NewServer + tsnet up).
func New(srv *tailkit.Server, logger *slog.Logger) *Fleet {
	return &Fleet{
		srv:    srv,
		tc:     tailkit.NewClient(srv),
		logger: logger,
	}
}

// Discover lists online hosts via tailkit.ListHosts. Always passes a non-nil
// &client.HostListOptions{Online: true} to dodge the peers.go:68 nil-deref
// bug documented in the README.
func (f *Fleet) Discover(ctx context.Context) ([]client.Host, error) {
	// NOTE: tailkit.ListHosts filters by hostReachable(host), which checks
	// host.Tailkitd != nil && host.Tailkitd.Peer != nil && host.Tailkitd.Peer.Online.
	// So "online" here means "the tailkitd sidecar is online", not just
	// "the machine peer is online" — this is the agent-reachability signal
	// Tailflow's status state machine uses.
	return tailkit.ListHosts(ctx, f.srv, &client.HostListOptions{Online: true})
}

// Host returns a client.HostScope bound to one host. The scope's Backend
// is tailkit's real httpBackend, which dial the host over the tailnet.
func (f *Fleet) Host(name string) client.HostScope {
	return f.tc.Host(name)
}

// Compile-time interface check.
var _ source.Fleet = (*Fleet)(nil)
