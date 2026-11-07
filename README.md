# Tailflow v1

A read-only fleet dashboard for [tailkitd](https://github.com/wf-pro-dev/tailkitd). Tailflow gives an operator a live, read-only view of every machine on a Tailscale tailnet that runs `tailkitd`: which hosts are up, how loaded they are (CPU, memory, network), what is listening on which port, and what Docker containers and Swarm services are running.

Tailflow is a **stateless, ephemeral aggregator**. It stores nothing durable. Everything it shows is either fetched live through [`tailkit`](https://github.com/wf-pro-dev/tailkit) or held in bounded in-memory ring buffers that are rebuilt after a restart. It is written as a standalone Go service with an embedded React single-page app, behind interfaces designed so that the whole thing can later be moved into the `tailkitd` daemon without a rewrite.

See [`docs/Tailflow-v1-design.md`](docs/Tailflow-v1-design.md) for the full design RFC.

## Status

**Milestone M0 — Foundations.** Repository skeleton, OpenAPI spec, codegen, fake source, build pipeline. Collection core (M1) is the next milestone.

## What this branch is

The `v1-rewrite` branch replaces the first Tailflow prototype (preserved on `main`). The prototype was a topology-graph viewer backed by SQLite; v1 is a read-only fleet dashboard with no persistence. See the design RFC for the rationale.

## Toolchain notes

- **Go**: The design targets Go 1.26; this branch is currently developed against Go 1.23.4 (the latest stable release available in CI at the time of writing). Set `GOTOOLCHAIN=local` if your Go is older than the version pinned in `tailkit`'s `go.mod`.
- **tailkit**: pinned to `v0.5.4` (commit `5d4ab42`, the head of branch `0.6.0` at the time of pinning — see design decision D4).
- **tailkitd**: targets `v0.1.x`.

## Repository layout

```
tailflow/
  cmd/tailflow/                 main: config, tsnet server, wiring, healthcheck subcommand
  internal/
    api/                        HTTP handlers, middleware, generated OpenAPI types
    config/                     env parsing and validation
    state/                      in-memory store: hosts, ring buffers, derived status, aggregation
    collector/                  discovery loop, per-host supervisors, mappers
    hub/                        SSE fan-out, replay buffer
    source/                     Fleet interface, Discoverer, Clock
      tailkitsrc/               production Fleet (the only importer of root tailkit)
      fakesource/               fake ClientBackend, SSE helpers, fake clock
  api/openapi.yaml              source of truth for the HTTP contract
  web/                          React app (src/, dist/ embedded via web/embed.go)
  testdata/                     recorded JSON and SSE fixtures per tailkitd endpoint
  scripts/                      codegen, fixture capture, load generator
  deploy/                       Dockerfile, docker-compose.yml, compose.test.yml
  Makefile  .golangci.yml  .github/workflows/ci.yml
```

## Quick start

```bash
# Run the server in local mode against a scripted fake fleet (no tailnet needed)
make dev

# Build the single binary with the SPA embedded
make build
./bin/tailflow

# Regenerate Go and TypeScript types from the OpenAPI spec
make codegen

# Run all tests with the race detector
make test-race
```

See [Configuration reference](docs/config.md) for the full set of environment variables.

## Known gaps and resolutions

The design RFC was written against an idealised view of the upstream code; the following deviations are documented here so the v1 implementation can ship without waiting for upstream changes.

| # | Gap | Resolution |
|---|---|---|
| 1 | `tailkitd/go.mod` pins `tailkit v0.5.0`, not `v0.5.4` | Tailflow pins `v0.5.4` per D4. The client/daemon combination (v0.5.4 client ↔ v0.5.0 daemon) is API-compatible at the endpoints Tailflow uses. |
| 2 | The design (§16.7) maps `ErrDockerUnavailable` and "Swarm not permitted" to HTTP 409. The daemon actually returns 503 (unavailable) and 403 (not-permitted). | Tailflow maps 503 → `capability-unavailable` and 403 → `permission-denied`. HTTP 409 is reserved for `/admin/*` epoch conflicts in tailkitd and is never emitted for capability issues. |
| 3 | `client.Docker().Compose().Services(ctx)` calls `GET /integrations/docker/compose/Services` (capital S), which does not exist on the daemon. | v1 uses `Compose().Projects()` (`GET /integrations/docker/compose/projects`) instead. Upstream issue filed. |
| 4 | `client.Docker().Config(ctx)` calls `GET /docker/config`, but the daemon exposes `/integrations/docker/config`. | Tailflow bypasses the SDK for this single call: `Backend.Do(ctx, host, "GET", "/integrations/docker/config", ...)`. Upstream issue filed. |
| 5 | The `tailkitd` installer's skeleton `metrics.toml` omits the `[ports]` section. | Documented in the README and integration test setup. Operators must add `[ports] enabled = true` to enable the ports tab. |
| 6 | The daemon's SSE `Last-Event-ID` handling is a stub: `ResumeFrom` parses the header but no handler consumes it; the sequence resets on every reconnect. | Tailflow's `internal/hub` implements its own replay buffer (per design §F8 / §16.6). It does not depend on the daemon-side resume. |
| 7 | `HostResponse.Arch` is populated from the Tailscale daemon version, not OS architecture. | Tailflow does not display `Arch`; the UI shows `OS` only. |
| 8 | `/integrations/docker/swarm/stream` reuses the `docker.container` event name (copy-paste bug in tailkitd). | Tailflow filters swarm events by source stream, not by event name. |

## License

MIT
