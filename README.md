# Tailflow

Tailflow is a Go backend for discovering and streaming service topology across a Tailscale tailnet. It fans out to online nodes through `tailkit`, collects snapshots of listening ports, containers, and proxy configs, resolves upstream relationships into topology edges, and exposes the result over a REST API and SSE streams.

## What It Does

- Collects per-node snapshots across the tailnet
- Parses proxy configs such as Nginx and Caddy
- Resolves service-to-service edges from ports, containers, and upstreams
- Persists current and historical runs in SQLite
- Streams node, port, and topology changes over SSE

## Planned Architecture

The implementation guide defines six internal packages:

- `internal/core` for shared IDs, timestamps, events, filters, and the event bus
- `internal/collector` for fan-out collection and node status tracking
- `internal/parser` for proxy parser strategies and registry-based dispatch
- `internal/resolver` for building topology edges from collected snapshots
- `internal/store` for GORM-backed SQLite persistence
- `internal/scheduler` and `internal/api` for orchestration, HTTP endpoints, and SSE

Expected entrypoint and layout:

```text
cmd/tailflow/main.go
internal/core
internal/collector
internal/parser
internal/resolver
internal/store
internal/scheduler
internal/api
internal/sse
```

## API Scope

The backend is designed to expose:

- node inventory and latest snapshots
- resolved topology and unresolved edges
- collection run history and manual triggers
- proxy config CRUD
- health and live event streams

## Status

This repository currently contains the implementation plan in [`tailflow-implementation.md`](./tailflow-implementation.md). The codebase is intended to be built around that design.

## License

MIT. See [`LICENSE`](./LICENSE).
