# Migration plan: move Tailflow into tailkitd

**Design ref: §20 of the Tailflow v1 design RFC.**

Tailflow v1 is a standalone Go service. The architecture is designed so that the whole thing can later be moved into the `tailkitd` daemon without a rewrite. This document describes the migration steps.

## Preconditions

- Milestones M0 to M5 complete
- Upstream U3 released (tailkit.AuthMiddleware X-Forwarded-For fix)
- API v1 frozen
- tailkitd built on a tailkit version that contains the `client` package

## Steps

### 1. Move packages

Move `internal/{state,collector,hub,api,source}` and `web/` into tailkitd as `internal/ui/...` and rewrite import paths. `state`, `hub` and `api` need no other change by design — they don't import tailkit or tailkit/client.

### 2. Replace tailkitsrc with a localfleet

`tailkitsrc` (the production Fleet that wraps `tailkit.NewClient(srv)`) is replaced by a `localfleet`:

- `Discover` uses the daemon's existing tsnet server
- `Host(name)` returns the normal scope for remote hosts
- For the local host, returns a scope over a `ClientBackend` that calls the daemon's own handlers in-process (a `ResponseWriter` implementing `Flusher` over an `io.Pipe`, so SSE works)

### 3. Mount the handler

Mount the handler at `/ui/` behind the daemon's existing `tailkit.AuthMiddleware`, enabled by a new `ui.toml`:

```toml
# /etc/tailkitd/ui.toml
enabled = false  # stable default until the migration is verified
```

### 4. Release pipeline

Build the SPA before GoReleaser and embed it via `//go:embed`. Budget at most 5 MB of extra binary size.

### 5. Version skew

Add an integration job that runs the UI against the previous two tailkitd releases.

### 6. Keep the standalone image

Keep the standalone image for one release as a thin wrapper with a deprecation notice.

## Acceptance

- Opening the UI from any node lists every host including itself
- The local host is served without a network round-trip
- The e2e suite from §18.4 passes against the embedded build
