# Tailflow

Tailflow discovers and streams service topology across a Tailscale tailnet. The repository now contains a Go backend plus a React UI: the backend fans out to online nodes through `tailkit`, collects snapshots of listening ports, containers, services, and proxy configs, resolves upstream relationships into topology edges, and exposes the result over a REST API and SSE streams; the UI renders the live graph and node details in the browser.

## What It Does

- Collects per-node snapshots across the tailnet
- Parses proxy configs such as Nginx and Caddy
- Resolves service-to-service edges from ports, containers, and upstreams
- Persists current and historical runs in SQLite
- Streams node, port, and topology changes over SSE

## Architecture

The backend is split across these internal packages:

- `internal/core` for shared IDs, timestamps, events, filters, and the event bus
- `internal/collector` for fan-out collection and node status tracking
- `internal/parser` for proxy parser strategies and registry-based dispatch
- `internal/resolver` for building topology edges from collected snapshots
- `internal/store` for GORM-backed SQLite persistence
- `internal/runtime` and `internal/api` for topology orchestration, HTTP endpoints, and SSE

Primary entrypoints and app layout:

```text
cmd/tailflow/main.go
internal/core
internal/collector
internal/parser
internal/resolver
internal/store
internal/runtime
internal/api
internal/sse
tailflow-ui
```

## Capabilities

- Collects per-node snapshots across the tailnet
- Parses proxy configs such as Nginx and Caddy
- Resolves service-to-service edges from ports, containers, services, and upstreams
- Persists current and historical runs in SQLite
- Streams node, port, and topology changes over SSE
- Serves the API on plain HTTP and over the tailnet
- Ships a browser UI for graph, node inventory, run status, and proxy-config inspection

## Development Notes

- Backend entrypoint: [`cmd/tailflow/main.go`](./cmd/tailflow/main.go)
- UI app: [`tailflow-ui`](./tailflow-ui)
- Backend design notes: [`tailflow-implementation.md`](./tailflow-implementation.md)
- UI implementation notes: [`tailflow-ui-implementation.md`](./tailflow-ui-implementation.md)

## Deployment Notes

- The UI compose service binds its published port to `TAILFLOW_UI_BIND_IP` when set, and otherwise falls back to `127.0.0.1`.
- Run [`docker/compose.sh`](./docker/compose.sh) instead of `docker compose` to auto-detect the host's Tailscale IPv4 address for normal startup.
- If you want to invoke `docker compose` directly, export `TAILFLOW_UI_BIND_IP` yourself first when you need the UI exposed on the tailnet.

## License

MIT. See [`LICENSE`](./LICENSE).
