# Tailflow v1: A Read-Only Fleet Dashboard for tailkitd

**Design document / RFC** · Status: Draft for team review · Date: 2026-11-07 · Target dependency: `tailkit v0.5.4` (git tag; same commit as the head of branch 0.6.0, decision D4)

## 1. Summary

Tailflow v1 is a web dashboard that gives an operator a live, read-only view of every machine on a Tailscale tailnet that runs `tailkitd`: which hosts are up, how loaded they are (CPU, memory, network), what is listening on which port, and what Docker containers and Swarm services are running.

It is a **stateless, ephemeral aggregator**. It stores nothing durable. Everything it shows is either fetched live through `tailkit` or held in bounded in-memory ring buffers that are rebuilt after a restart. It is written as a standalone Go service with an embedded React single-page app, behind interfaces designed so that the whole thing can later be moved into the `tailkitd` daemon without a rewrite.

This document proposes the scope, architecture, data model, features (with the algorithms and libraries for each), the fixed v1 API contract, and the test strategy. It deliberately excludes the topology graph and any configuration editing; both are follow-ups (section 12).

## 2. Background and problem statement

The first Tailflow prototype proved the idea but is not a sound base to extend:

- **Undefined data model.** It persisted "collection runs" and snapshots in SQLite while also keeping live state in memory. Two sources of truth made every change risky, and the recent tailkit migration broke compilation (`store.RunStore`, `store.CollectionRun` undefined).
- **Wrong abstraction for its inputs.** It polled in batch cycles, but tailkitd already offers push-style SSE streams. Polling plus streaming plus persistence is three mechanisms doing one job.
- **No usage data.** It showed structure (ports, edges) but not load, so it could not answer the first question an operator asks: "which machine is struggling?"
- **Thin UI.** No information hierarchy, no tables, no time series, no degraded-state design.

The rewrite keeps the good parts of the prototype (container and swarm mapping logic, the Docker-event filtering heuristic, SSE client patterns) and replaces the architecture.

## 3. Goals and non-goals

**Goals**

- G1. Fleet overview: every host, its health and key vitals, at a glance, updating live.
- G2. Host detail: CPU, memory, network history charts, processes, listening ports.
- G3. Docker tab: containers, per-container live stats and logs, Swarm services, Compose services.
- G4. A **fixed, versioned HTTP API** (`/api/v1`) specified in OpenAPI, so UI and backend evolve independently and the app can be embedded later.
- G5. Rely on `tailkit`/`tailkitd` for every piece of remote data and identity; no direct Docker, SSH or `/proc` access from Tailflow.
- G6. Graceful degradation: a host with Docker disabled, an old agent, or a flapping connection is a first-class UI state, not an error page.

**Non-goals (v1)**

- Topology graph and proxy-config parsing (deferred; see section 12).
- Any write operation: no container start/stop, no config edits, no file sends.
- Long-term metrics storage, alerting, or multi-user accounts.
- Disk I/O throughput (decision D1: v1 ships without it; disk space usage is included; see upstream requirement U4).
- Non-Linux hosts (tailkitd is Linux-only today).

## 4. Architecture

### 4.1 Layers

The service follows a ports-and-adapters layout so that only one package knows about the network.

```
 Browser (React SPA, embedded via embed.FS)
    |  REST (/api/v1/*) + one SSE connection (/api/v1/stream)
    v
 internal/api        HTTP handlers, OpenAPI-validated, problem+json errors
    |
 internal/state      in-memory store: hosts, ring buffers, docker inventory
    ^                 ^
    | writes          | publishes change events
 internal/collector   internal/hub  (fan-out to SSE subscribers)
    |
 internal/source     Fleet interface (Discover + HostScope)  <-- the only seam
    |
 internal/source/tailkitsrc   production Fleet: tailkit.ListHosts + tailkit.NewClient(srv)
```

`internal/source` defines one small interface, `Fleet`, with two methods: `Discover(ctx) ([]client.Host, error)` and `Host(name) client.HostScope`. `client.HostScope` is a plain struct holding a `client.ClientBackend` (an interface with `Do`, `DoRaw`, `Stream`, `Peers`, `Hosts`, `Agents`), so tests build real tailkit scopes on top of a fake backend that returns recorded JSON and SSE bytes. That exercises tailkit's real path building and decoding instead of a hand-written imitation of it. Only `internal/source/tailkitsrc` and `cmd/tailflow` import the root `tailkit` package; `collector` and `source` may import `tailkit/client`; `state`, `hub` and `api` import neither and use Tailflow's own types. When Tailflow moves into tailkitd, a `ClientBackend` that calls the daemon's own handlers in-process can serve the local host.

### 4.2 Data flow

1. A **discovery loop** lists hosts and reconciles a set of per-host supervisors.
2. Each **host supervisor** owns that host's long-lived streams and writes into `state`.
3. `state` notifies the **hub**, which pushes typed events to connected browsers.
4. REST endpoints read `state` snapshots; they never call tailkit directly, except two on-demand streams (container logs, container stats) which are proxied with reference counting.

### 4.3 Technology choices

| Concern | Choice | Reason |
| --- | --- | --- |
| Language and HTTP | Go 1.26, stdlib `net/http` with 1.22+ pattern routing | Matches tailkitd; no framework to embed later |
| Remote access | `tailkit` v0.5.4 | The whole point of the project |
| Concurrency | `golang.org/x/sync/errgroup`, `singleflight` | Structured goroutine lifetimes; request coalescing |
| Logging | `log/slog` (JSON) | Stdlib; structured; swap-friendly with tailkitd's zap |
| API contract | OpenAPI 3.1, `oapi-codegen` (Go), `openapi-typescript` (TS) | Single source of truth; generated types both sides |
| Frontend | React 18, Vite, TypeScript, TanStack Query/Router/Table, Tailwind, shadcn/ui | Mature, testable, professional table and layout primitives |
| Charts | `uPlot` | Canvas-based; handles thousands of points at 60 fps |

## 5. Data model: ephemeral by design

**Decision: Tailflow persists nothing.** Observations of the present are always re-derivable from the fleet; a database copy can only go stale and adds migrations, retention and corruption handling to what should be a single binary. History is a bounded in-memory window, lost on restart; users who want long-term retention should scrape it elsewhere.

### 5.1 Entities (in-memory)

| Entity | Source | Lifetime | Notes |
| --- | --- | --- | --- |
| `Host` | `tailkit.ListHosts` → `client.Host` | While discovered | Adds derived `Status` and `Capabilities` |
| `MetricSeries` | `HostScope.Metrics().All` stream | Ring buffer, default 1 h at 5 s (720 points per series) | CPU %, mem %, swap %, net rx/tx rate per interface |
| `Process` list | Same `All` payload | Latest only | Sorted on read |
| `Port` set | `Metrics().Stream().Ports` | Latest only | Snapshot + bound/released deltas |
| `ContainerInventory` | `Docker().Containers().List`, refreshed on events | Latest only | Plus Swarm and Compose service lists |
| `ContainerStats` | `Container(id).StreamStats` | Ring buffer, only while subscribed | Lazy; reference counted |

### 5.2 Derived host status

Status is a small state machine, computed from three inputs: `Host.Online`, agent reachability (`Host.Tailkitd.Peer.Online`), and sample freshness.

| Status | Condition |
| --- | --- |
| `offline` | `Host.Online == false` |
| `no-agent` | Online, but tailkitd peer absent or offline |
| `stale` | Agent reachable but last sample older than 3x the stream interval |
| `degraded` | N consecutive stream failures (N=3, with hysteresis to avoid flapping) |
| `healthy` | Otherwise |

### 5.3 Concurrency model

`state` is guarded by a `sync.RWMutex` per host (not one global lock), so a slow host never blocks reads for another. API handlers copy out immutable snapshot structs; they never hand out references into mutable state.

## 6. Features

Each feature lists its purpose, the tailkit surface it uses, the algorithm and libraries, and acceptance criteria.

### F1. Host discovery and reconciliation

**Purpose.** Know which hosts exist and keep a supervisor running for each reachable one.

**tailkit surface.** `tailkit.ListHosts(ctx, srv, &client.HostListOptions{Online: true})` returning `[]client.Host` (fields `Name`, `Role`, `Environment`, `Tags`, `TSIPs`, `TSDNSName`, `Online`, `Tailkitd`). tailkit already caches peers internally; Tailflow polls on a 15 s interval.

**Algorithm.** Set reconciliation: compute `added = discovered − running` and `removed = running − discovered` by host name; start a supervisor for each added host and cancel the context of each removed host. Run `ListHosts` through `singleflight` so concurrent triggers share one call. Emit `host.updated` only when a deep-equal comparison of the new `Host` against the stored one differs (avoids event storms).

**Libraries.** `golang.org/x/sync/errgroup`, `singleflight`, `reflect.DeepEqual` or `go-cmp` for diffing.

**Acceptance.** Adding or removing a node is reflected within one discovery interval; no goroutine leak after a host disappears (verified with `goleak`).

### F2. Live metrics ingestion

**Purpose.** Continuously capture CPU, memory, disk usage, network counters and processes per host.

**tailkit surface.** `HostScope.Metrics().All(ctx, fn)` — a single SSE stream per host whose payload is `client.Metrics` (`CPU`, `Memory`, `Disk`, `Network`, `Processes`, `Ports`). Using `All` instead of separate CPU/Memory streams means one connection per host, not three.

**Algorithm.**

- *Reconnect with backoff.* Wrap the stream in a loop with exponential backoff and full jitter (`delay = rand(0, min(cap, base·2^attempt))`, base 1 s, cap 30 s). Reset the attempt counter after a stream stays healthy for 30 s.
- *Rate computation.* Network counters are cumulative (`IOCountersStat.BytesSent/BytesRecv`). Rate = `(c_now − c_prev) / Δt`. Handle counter reset (negative delta) by discarding that sample, and use the monotonic receive time, not the payload timestamp, for Δt.
- *Ring buffer.* A generic fixed-capacity circular buffer `Ring[T]` with O(1) append and O(n) ordered snapshot. No allocation on the hot path after construction.
- *Smoothing (optional, display only).* Exponentially weighted moving average, `s_t = α·x_t + (1−α)·s_{t−1}`, computed in the UI, never stored.

**Libraries.** stdlib only for ring buffer and rates; `cenkalti/backoff/v4` is acceptable instead of the hand-rolled backoff.

**Acceptance.** Ring never exceeds capacity; reconnect delays follow the bounded schedule (tested with an injected clock); a counter reset does not produce a negative or spike rate.

**Sampling interval and retention.** tailkitd v0.1 emits `All` events every 2 s (a fixed `streamInterval`; decision D2). Tailflow reads the interval from the metrics config once upstream requirement U1 ships and assumes 2 s until then. Raw events are never stored: the aggregator (section 16.5) reduces them to one sample per `TAILFLOW_HISTORY_STEP` (default 5 s), so a 1 h window holds 720 points per series whatever the stream interval. A host is `stale` after `TAILFLOW_STALE_FACTOR` missed intervals (6 s by default). Network rates come from interface counters. Disk throughput is out of scope for v1 (decision D1); disk space usage from `client.Metrics.Disk` is shown.

### F3. Fleet overview

**Purpose.** The landing page: every host in a sortable, filterable table with status, CPU, memory, network sparkline, container count, and last-seen.

**Data.** `GET /api/v1/hosts` plus live `host.updated` and `metrics.sample` events.

**Algorithm.** Server-side: filter by `status`, `role`, `environment`, `tag`, and free-text `q` (case-insensitive substring over name, DNS name, tags); stable sort on any column. Client-side: TanStack Table with column sorting and virtualised rows (`@tanstack/react-virtual`) so 200+ hosts stay smooth. Sparklines are drawn from the last 60 points of each host's series, downsampled client-side.

**Acceptance.** Table remains interactive with 500 simulated hosts; status colours are accompanied by text or icons (not colour alone).

### F4. Host detail: time-series charts

**Purpose.** Show CPU (total and per-core), memory and swap, and network rx/tx for a chosen window (5 m / 15 m / 1 h).

**Data.** `GET /api/v1/hosts/{host}/metrics?window=15m&step=5s`.

**Algorithm.** Server returns at most N points (default 300) regardless of window. When the window holds more points than N, apply **LTTB** (Largest-Triangle-Three-Buckets) downsampling, which preserves visual peaks far better than averaging or decimation, and is O(n). Charts render with `uPlot` using typed arrays; the cursor is synchronised across charts on the page using uPlot's sync feature.

**Acceptance.** A 1 h window returns ≤ N points; a spike present in the raw data is still visible after downsampling (property test: the maximum of the downsampled series equals the maximum of the raw series within a bucket).

### F5. Processes and listening ports

**Purpose.** Top processes by CPU or memory; a table of listening sockets (address, port, protocol, PID, process).

**tailkit surface.** Processes arrive in the `All` payload. Ports: `Metrics().Ports(ctx)` for the initial snapshot and `Metrics().Stream().Ports(ctx, fn)` for `snapshot` / `bound` / `released` deltas (`client.PortUpdate{Kind, Port, Ports}`).

**Algorithm.** Port state is a map keyed by the tuple `(addr, port, proto, pid)`; a `snapshot` event replaces the map, `bound` upserts, `released` deletes. Processes arrive already limited by tailkitd (`[processes] limit` in `metrics.toml`: default 20, maximum 100, sorted by CPU descending), so Tailflow does no top-K selection. Sorting by memory only reorders that limited set, and the UI labels the table "top N by CPU on this host" so nobody reads it as a complete process list.

**Acceptance.** Replaying a recorded sequence of snapshot/bound/released events yields the same port set as the final snapshot (golden-file test).

### F6. Docker inventory

**Purpose.** Per host: containers with state, image, ports, health, uptime; Swarm services with replica counts; Compose services.

**tailkit surface.** `Docker().Config(ctx)` (is Docker enabled, Swarm read permitted), `Docker().Containers().List(ctx)`, `Docker().Swarm().Services().List(ctx)`, `Docker().Compose().Services(ctx)`, and `Docker().Stream(ctx, fn)` for lifecycle events. Map `client.ErrDockerUnavailable` to a `docker: unavailable` capability, not an error.

**Algorithm.** *Event-driven refresh with debounce.* Subscribe to Docker events; ignore noisy actions (`exec_*`, `attach`, `detach`, `top`, `resize`, `archive-path`) using the filter carried over from the prototype; on a relevant event, trigger a refetch, debounced 300 ms trailing-edge so a burst of events (for example a Compose restart) causes one list call. Diff the new inventory against the old by container ID and emit `docker.container.changed` for each change plus a single `docker.inventory.replaced` for bulk updates. Container state transitions are rendered with a deterministic status-to-colour/icon map.

**Libraries.** `singleflight` (dedupe refetches), a small hand-written debouncer on `time.AfterFunc`.

**Acceptance.** 50 events in 100 ms produce exactly one list call; a host with Docker disabled shows a clear empty state with the reason.

### F7. Container drill-down: live stats and logs

**Purpose.** Open a container and see live CPU %, memory, network and block I/O, plus a tailing log viewer.

**tailkit surface.** `Docker().Container(id).StreamStats(ctx, fn)` (`client.Event[container.StatsResponse]`) and `StreamLogs(ctx, tail, fn)` (`client.LogLine`).

**Algorithm.**

- *Lazy subscription with reference counting.* A stream to tailkitd opens when the first browser subscribes to a container and closes (after a 10 s grace period) when the last unsubscribes. This bounds remote load to what people are actually watching.
- *Docker stats maths.* CPU % = `(Δcpu_total / Δsystem_cpu) · online_cpus · 100`, using the difference between `cpu_stats` and `precpu_stats`. Memory used = `usage − inactive_file` (cache-adjusted, matching `docker stats`). Block and network I/O are cumulative counters, converted to rates as in F2.
- *Log viewer.* Bounded buffer (last 5,000 lines) with virtualised rendering; ANSI colour parsing with `anser` or `ansi-to-react`; follow mode that pauses on user scroll-up.

**Acceptance.** Closing the browser tab closes the upstream stream within the grace period (verified by counting open streams in a fake source); computed CPU % matches `docker stats` output for a fixture.

### F8. Real-time delivery to the browser

**Purpose.** Push state changes to the UI over one connection.

**Design.** A single `GET /api/v1/stream` SSE endpoint. The client chooses topics (`hosts`, `metrics:{host}`, `docker:{host}`). The hub keeps one bounded channel per subscriber.

**Algorithm.**

- *Backpressure by coalescing.* If a subscriber's channel is full, metric samples are replaced by the latest value (they are idempotent); structural events (`host.removed`) are never dropped, and a subscriber that cannot keep up is disconnected with a `resync` instruction.
- *Resume.* Every event carries a monotonic `id`. The hub retains the last 1,000 events; on reconnect with `Last-Event-ID` the server replays the gap, or sends `resync` if the gap is too old.
- *Keep-alive.* A comment line every 15 s to defeat idle-timeout proxies. Reuses the SSE framing approach already proven in `tailkitd/internal/sse`.

**Libraries.** stdlib `http.Flusher`; client side `EventSource` wrapped in a hook that feeds the TanStack Query cache (`queryClient.setQueryData`).

**Acceptance.** A slow consumer cannot grow server memory unboundedly; reconnect after a 5 s network drop loses no structural events.

### F9. Capability and version negotiation

**Purpose.** Hosts run different tailkitd versions and have different integrations enabled. The UI must adapt per host.

**Design.** Each `Host` carries a `capabilities` object: `metrics`, `docker`, `swarm`, `compose` (booleans, derived from `Docker().Config`, `Metrics()` availability and typed errors such as `ErrDockerUnavailable`, `ErrMetricsUnavailable`, `ErrPermissionDenied`) plus the reported agent version if available. The UI hides or disables tabs rather than showing failures, with a tooltip stating the reason.

**Acceptance.** A host with no `docker.toml` renders a disabled Docker tab; a permission error is distinguishable from "not installed".

### F10. Embeddability and identity

**Purpose.** Make the later move into tailkitd mechanical.

**Design rules.**

- The SPA is built to `web/dist` and served with `//go:embed` and an SPA fallback handler (unknown non-API paths return `index.html`).
- All routes are mounted on an injectable `http.Handler` under a configurable base path (default `/`), so tailkitd can mount it at `/ui`.
- Only `internal/source/tailkitsrc` and `cmd/tailflow` may import the root `tailkit` package; `tailkit/client` types may appear only in `internal/source` and `internal/collector`. CI enforces this with a `depguard` rule.
- Auth is delegated: standalone, the handler is wrapped in `tailkit.AuthMiddleware` and served on tsnet; embedded, tailkitd's existing middleware applies. v1 has only GET endpoints, so authorization is "any authenticated tailnet caller" until tailkitd gains capability checks. **Dependency:** the `X-Forwarded-For` trust issue in `tailkit.AuthMiddleware` must be fixed (upstream requirement U3) before this is exposed beyond a trusted tailnet.

## 7. API specification (v1, fixed)

Base path `/api/v1`. JSON, UTF-8. Errors use `application/problem+json` (RFC 9457): `{type, title, status, detail, host?}`. The API is additive within v1; breaking changes require `/api/v2`.

### 7.1 REST endpoints

| Method and path | Description | Notes |
| --- | --- | --- |
| `GET /healthz` | Liveness | `200 {"status":"ok"}` |
| `GET /meta` | App version, tailkit version, uptime, buffer config | For the About panel |
| `GET /fleet/summary` | Counts by status; fleet-wide CPU and memory means | Drives overview header cards |
| `GET /hosts` | List hosts | Query: `status`, `role`, `environment`, `tag`, `q`, `sort`, `order` |
| `GET /hosts/{host}` | One host with status and capabilities | `404` if unknown |
| `GET /hosts/{host}/metrics` | Time series | Query: `window` (`5m`,`15m`,`1h`), `step`, `series` (`cpu`,`mem`,`net`) |
| `GET /hosts/{host}/metrics/latest` | Latest sample | Cheap polling fallback |
| `GET /hosts/{host}/processes` | Top processes | Query: `sort` (`cpu`,`mem`), `limit` (≤200) |
| `GET /hosts/{host}/ports` | Listening sockets |  |
| `GET /hosts/{host}/docker/containers` | Container list | Query: `state`, `q` |
| `GET /hosts/{host}/docker/containers/{id}` | Inspect summary |  |
| `GET /hosts/{host}/docker/containers/{id}/stats` | Live stats | `Accept: text/event-stream` required |
| `GET /hosts/{host}/docker/containers/{id}/logs` | Logs | Query: `tail` (≤5000); SSE when `Accept: text/event-stream` |
| `GET /hosts/{host}/docker/swarm/services` | Swarm services | `409` if Swarm read not permitted |
| `GET /hosts/{host}/docker/compose/services` | Compose services |  |
| `GET /stream` | Global SSE | Query: `topics` |

### 7.2 Core schemas (abridged)

```json
// Host
{
  "name": "vps-1", "role": "web", "environment": "prod", "tags": ["edge"],
  "ts_ips": ["100.64.0.5"], "os": "linux", "arch": "amd64",
  "status": "healthy",
  "last_sample_at": "2026-11-07T10:15:30Z",
  "stream_interval_s": 2, "capabilities": {"metrics": true, "docker": true, "swarm": false, "compose": true, "reasons": {"swarm": "not-configured"}}
}

// MetricsResponse
{
  "host": "vps-1", "window_s": 900, "step_s": 5,
  "t": [1762510000, 1762510005],          // unix seconds
  "series": {
    "cpu_total": [12.4, 15.1],
    "mem_used_pct": [41.0, 41.2],
    "net_rx_bps": [1200, 3400],
    "net_tx_bps": [800, 900]
  }
}
```

### 7.3 SSE events

| Event | Payload | Dropped under backpressure? |
| --- | --- | --- |
| `host.updated` | `Host` | No |
| `host.removed` | `{name}` | No |
| `metrics.sample` | `{host, t, cpu_total, mem_used_pct, net_rx_bps, net_tx_bps}` | Coalesced |
| `port.bound` / `port.released` | `{host, port}` | No |
| `docker.container.changed` | `{host, container}` | No |
| `docker.inventory.replaced` | `{host, containers}` | No |
| `resync` | `{reason}` | n/a; client refetches all |

## 8. Test strategy

The goal is that every behavioural claim in section 6 has an automated check, and that no test needs a real tailnet.

### 8.1 The test seam

The mock boundary is tailkit's own `client.ClientBackend` interface. The `fakesource` package implements it with scripted responses keyed by `(hostname, method, path)`: canned JSON for `Do`, canned bytes for `DoRaw`, and a channel-backed reader for `Stream` so tests can inject SSE frames, delays, mid-stream disconnects and errors. Production code builds `client.HostScope{Backend: b, Hostname: name}`, so tailkit's real URL building and JSON/SSE decoding run in every test. Discovery is a separate `Discoverer` function (`func(ctx) ([]client.Host, error)`) because `tailkit.ListHosts` needs a live `*tailkit.Server`; tests pass a fixture. Time is injected through a `Clock` interface (`Now`, `NewTimer`, `After`) with a fake that advances manually.

### 8.2 Layers

| Layer | Scope | Tools | Examples |
| --- | --- | --- | --- |
| Unit | Pure logic | `testing`, table-driven tests, `go-cmp` | Rate maths with counter reset, LTTB, status state machine, Docker CPU % formula, port set replay |
| Property and fuzz | Invariants | `testing/quick`, native Go fuzzing | `Ring[T]` never exceeds capacity and preserves order; LTTB keeps bucket maxima; filter/sort never panics on arbitrary query strings |
| Concurrency | Races and leaks | `go test -race`, `goleak` | Hub with slow subscribers; supervisor shutdown leaves zero goroutines |
| Component | Collector + state + hub with `fakesource` | Injected clock | Backoff schedule; debounce of 50 events into one refetch; refcounted stats stream opens and closes |
| API contract | Handlers vs spec | `httptest`, `kin-openapi` response validation | Every endpoint's response validates against the OpenAPI schema; error shapes are problem+json |
| Golden | Recorded sequences | `testdata/` fixtures | Replay captured port and Docker event streams, compare resulting state |
| Integration | Real tailkitd | Docker Compose: one `tailkitd` container + Tailflow, `testcontainers-go` | Host appears, metrics flow, container list matches `docker ps` |
| Frontend unit | Components, hooks | Vitest, React Testing Library, MSW | Table sorting; SSE hook updates the query cache; degraded-state rendering |
| End to end | Browser | Playwright against Tailflow + fakesource server | Overview loads, host detail charts render, container logs follow and pause |
| Accessibility | UI | `axe-core` in Playwright | No critical violations on main views |

### 8.3 Quality gates (CI, every pull request)

- `go vet`, `staticcheck`, `golangci-lint`; `eslint` and `tsc --noEmit` for the UI.
- `go test -race ./...` with a coverage floor of 80% on `internal/state`, `internal/collector`, `internal/hub` (these hold the logic that can silently go wrong).
- OpenAPI spec lint (`spectral`) and a check that generated Go and TypeScript types are up to date (`git diff --exit-code` after codegen).
- Integration and e2e suites run on merge to main and nightly.

## 9. Non-functional requirements

| Area | Target |
| --- | --- |
| Scale | 200 hosts, 50 containers per host, 10 concurrent browser sessions |
| Memory | ≤ 150 MB at that scale (ring buffers sized by config) |
| Latency | Metric sample to browser ≤ 1 s after receipt; overview first paint ≤ 1.5 s |
| Resilience | Loss of one host never affects others; process restart recovers full live state within one stream interval, history refills over time |
| Security | Read-only; no secrets in logs; strict `Content-Security-Policy`; auth middleware on all routes; no CORS by default |
| Observability | `slog` JSON logs with `host` and `request_id`; own `/metrics` (Prometheus) with stream counts, reconnects, hub drops |

## 10. Repository layout

```
tailflow/
  cmd/tailflow/                 main: config, tsnet server, wiring
  internal/config/              env parsing and validation
  internal/api/                 handlers, middleware, generated OpenAPI types
  internal/state/               host state, Ring[T], derived status, aggregation
  internal/collector/           discovery loop, per-host supervisors, mappers
  internal/hub/                 SSE fan-out, replay buffer
  internal/source/              Fleet interface, Discoverer, Clock
  internal/source/tailkitsrc/   production Fleet (the only importer of root tailkit)
  internal/source/fakesource/   fake ClientBackend, SSE helpers, fake clock
  api/openapi.yaml              source of truth for the contract
  web/                          React app (src/, dist/ embedded via web/embed.go)
  testdata/                     recorded JSON and SSE fixtures per tailkitd endpoint
  scripts/                      codegen, fixture capture, load generator
  deploy/                       Dockerfile, docker-compose.yml, compose.test.yml
  Makefile  .golangci.yml  .github/workflows/ci.yml
```

## 11. Milestones

| # | Deliverable | Exit criteria |
| --- | --- | --- |
| M0 | Skeleton, OpenAPI spec, CI, `fakesource`, codegen | Pipeline green; spec reviewed |
| M1 | F1, F2 backend; `/hosts`, `/metrics` | Component tests pass; race-clean |
| M2 | F3, F4 UI: overview and host detail | Demo against a real 3-node tailnet |
| M3 | F5, F6: ports, processes, Docker inventory | Golden replay tests green |
| M4 | F7, F8, F9: drill-down, SSE hardening, capabilities | Slow-consumer and reconnect tests green |
| M5 | F10, docs, packaging | Mounts under a base path inside a sample host program |

## 12. Out of scope and follow-ups

- **Topology.** A later tab. The stronger design is to derive edges from observed established connections rather than parsing nginx or Caddy configs; the existing parsers can then provide labels only.
- **Configuration editing.** Requires capability-based authorization in tailkitd, atomic validated writes with rollback, and a decision on identity forwarding between nodes. Not before read-only ships.
- **Persistence of intent** (saved views, watched paths): plain files, if ever needed.

## 13. Decisions and remaining risks

The open questions from the first draft are resolved below. Each decision has an ID that later sections refer to.

| ID | Question | Decision |
| --- | --- | --- |
| D1 | Disk I/O throughput | Ship v1 without it. Disk space usage stays. Throughput is a tailkitd follow-up (U4). |
| D2 | Stream sampling interval | Hard-coded at 2 s in tailkitd v0.1 (`streamInterval`, unexported). Make it configurable in tailkitd (U1). Tailflow reads it from the metrics config and defaults to 2 s when absent. |
| D3 | Test seam | `client.ClientBackend` (interface in `client/client.go`). `HostScope` and the other scopes are plain structs, so they can be built on a fake backend. Discovery uses a `Discoverer` function because `tailkit.ListHosts` needs a live server. |
| D4 | tailkit version | Pin `v0.5.4` (git tag). It is the same commit (`5d4ab42`) as the head of branch `0.6.0` when checked. Never depend on the branch name; revisit when `v0.6.0` is tagged. |
| D5 | Process list | Limited by tailkitd (default 20, max 100, by CPU). Tailflow does not select top-K itself. |
| D6 | Agent version | `client.Host` has no version field. Capabilities are inferred from config endpoints and typed errors; U2 optionally adds a version. |

**Remaining risks**

1. **tailkit API churn.** The scoped client API is recent and has rough edges (for example the exported names `SwarwService` and `SwarmUpdate.Servies`, and the file `metrivs.go`). Tailflow maps every tailkit type to its own type at the collector boundary so these never reach `state` or the API, and pins the version behind the integration suite.
2. **Auth hardening dependency (U3).** Exposure beyond a trusted tailnet is blocked on the `AuthMiddleware` fix and, ideally, capability checks.
3. **Discovery latency.** `tailkit.ListHosts` reads tailkit's internal peer cache; how long that cache lives has not been verified. Measure it in M1 and set the discovery interval accordingly.
4. **Interval unknown until U1 ships.** Until tailkitd reports its interval, the stale threshold is a fixed 3 x 2 s = 6 s.
5. **Heterogeneous fleets.** Old agents may lack endpoints. Every endpoint call must treat 404 and typed errors as "capability absent", never as a host failure.

## 14. Upstream requirements on tailkit and tailkitd

Tailflow v1 can be built against tailkit v0.5.4 and tailkitd v0.1 as they are today. The changes below live in other repositories. U1 and U3 should land before milestone M4; the rest are optional.

| ID | Repo | Change | Why | Blocks |
| --- | --- | --- | --- | --- |
| U1 | tailkitd, tailkit | Add `[stream] interval = "2s"` to `metrics.toml` (allowed 1 s to 60 s, default 2 s) and return it from `GET /integrations/metrics/config`; expose it on the tailkit metrics config type | Sets ring-buffer resolution and the stale threshold exactly (D2) | Nothing; Tailflow defaults to 2 s |
| U2 | tailkitd, tailkit | Report agent version and build in `GET /host` as a new `agent_version` field on `client.Host` | Version-gated UI features (D6) | Nothing; capability probing is the fallback |
| U3 | tailkit | `AuthMiddleware` uses `r.RemoteAddr` only; `X-Forwarded-For` honoured only behind an explicit trusted-proxy option | Any peer can currently impersonate another by sending the header | M5, and any exposure beyond a trusted tailnet |
| U4 | tailkitd, tailkit | Add per-device disk I/O counters (`gopsutil` `disk.IOCountersStat`) to the `all` payload behind `[disk] io = true` | Disk throughput charts (D1) | Nothing; post-v1 |
| U5 | tailkit | Fix exported typos (`SwarwService`, `SwarmUpdate.Servies`, file `metrivs.go`); keep deprecated aliases for one release | API hygiene | Nothing |

**Behaviour before the upstream changes land.** Tailflow assumes a 2 s interval (U1), infers capabilities from `Docker().Config`, the metrics `available` and `config` endpoints and typed errors (U2), runs on a trusted tailnet only (U3), and shows no disk throughput (U4).

## 15. Configuration reference

Tailflow is configured by environment variables only (twelve-factor); flags are not needed. All values are validated at startup and every error is reported at once before exiting with status 2.

| Variable | Default | Meaning |
| --- | --- | --- |
| `TAILFLOW_MODE` | `tailnet` | `tailnet` serves over tsnet; `local` serves plain HTTP on `127.0.0.1` for development and tests |
| `TS_AUTHKEY` | none | Tailscale auth key; required on first start in `tailnet` mode, ignored once state exists |
| `TS_STATE_DIR` | `/var/lib/tailflow` | tsnet state directory (a persistent volume in Docker) |
| `TAILFLOW_HOSTNAME` | `tailflow` | tsnet hostname |
| `TAILFLOW_LISTEN` | `:8080` | Listen address (tailnet or local) |
| `TAILFLOW_BASE_PATH` | `/` | URL prefix the app is mounted under (for example `/ui`) |
| `TAILFLOW_DISCOVERY_INTERVAL` | `15s` | How often `Discover` runs (minimum 5 s) |
| `TAILFLOW_HISTORY_WINDOW` | `1h` | Length of each metric ring buffer (5 m to 6 h) |
| `TAILFLOW_HISTORY_STEP` | `5s` | Aggregation bucket width; must be a multiple of the stream interval |
| `TAILFLOW_CALL_TIMEOUT` | `10s` | Timeout for each unary tailkit call (streams are exempt) |
| `TAILFLOW_STALE_FACTOR` | `3` | A host is `stale` after this many missed stream intervals |
| `TAILFLOW_DEGRADE_THRESHOLD` | `3` | Consecutive stream failures before `degraded` |
| `TAILFLOW_MAX_HOSTS` | `500` | Safety cap on supervised hosts |
| `TAILFLOW_MAX_SSE_CLIENTS` | `50` | Browser SSE connections accepted; extras get `503` |
| `TAILFLOW_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `TAILFLOW_LOG_FORMAT` | `json` | `json` or `text` |
| `TAILFLOW_HEALTH_ADDR` | `127.0.0.1:8081` | Loopback-only listener serving /api/v1/healthz in tailnet mode; used by the container health check (section 19.2) |

**Startup contract.** The process must (1) validate config, (2) start tsnet or the local listener, (3) start the discovery loop, (4) serve `/api/v1/healthz` immediately (liveness does not wait for discovery), and (5) on SIGINT or SIGTERM stop accepting requests, cancel all supervisors, close SSE streams with a `resync` hint, and exit within 10 s.

**Log fields.** Every log line carries `time`, `level`, `msg`; request logs add `request_id`, `method`, `path`, `status`, `duration_ms`; collector logs add `host` and `stream` (`metrics`, `ports`, `docker-events`, `container-stats`, `container-logs`). Secrets (`TS_AUTHKEY`) are never logged.

## 16. Backend implementation contracts

This section fixes the Go-level contracts so that two developers can build `collector`, `state`, `hub` and `api` in parallel.

### 16.1 Source seam (`internal/source`)

```go
// Fleet is the only way the rest of Tailflow reaches the tailnet.
type Fleet interface {
    // Discover lists online hosts. Production: tailkit.ListHosts(ctx, srv, &client.HostListOptions{Online: true}).
    // fakesource.Fleet returns a scripted list (the "Discoverer" of section 8.1).
    Discover(ctx context.Context) ([]client.Host, error)
    // Host returns a scope bound to one host. Production: tailkit.NewClient(srv).Host(name).
    // fakesource.Fleet returns client.HostScope{Backend: fakeBackend, Hostname: name}.
    Host(name string) client.HostScope
}

type Clock interface {
    Now() time.Time
    NewTimer(d time.Duration) Timer // Timer: C() <-chan time.Time, Stop() bool, Reset(d)
}
```

### 16.2 State types (`internal/state`)

These types never import tailkit. The collector maps tailkit types onto them.

```go
type Status string // "offline" | "no-agent" | "stale" | "degraded" | "healthy"

type Capabilities struct {
    Metrics, Docker, Swarm, Compose bool
    Reasons map[string]string // capability -> "not-configured" | "permission-denied" | "unavailable"
}

type Host struct {
    Name, DNSName, Role, Environment, OS, Arch string
    Tags, TSIPs        []string
    Status             Status
    Caps               Capabilities
    StreamInterval     time.Duration // 2s until U1
    LastSampleAt       time.Time
    UptimeSeconds      uint64
}

type Sample struct {            // one aggregated bucket
    T                time.Time
    CPUTotal         float64    // percent, 0-100
    CPUPerCore       []float64
    MemUsedPct, SwapUsedPct float64
    MemUsedBytes, MemTotalBytes uint64
    NetRxBps, NetTxBps float64   // summed over reported interfaces, bytes/s
}

type Ring[T any] struct { /* fixed capacity, one mutex */ }
func NewRing[T any](capacity int) *Ring[T]
func (r *Ring[T]) Push(v T)
func (r *Ring[T]) Len() int
func (r *Ring[T]) Snapshot() []T // oldest to newest; a copy
```

`Store` exposes `Hosts() []Host`, `Host(name) (Host, bool)`, `Series(name, window) []Sample`, `Latest(name) *Latest` (processes, ports, disks, per-interface counters), `Containers(name) ContainerInventory`, plus mutators used only by the collector (`UpsertHost`, `RemoveHost`, `PushSample`, `SetPorts`, `ApplyPortUpdate`, `SetContainers`, `SetStatus`). Every mutator publishes the matching event to the hub after releasing the host lock.

### 16.3 Mapping tailkit to state

| tailkit value | State field | Rule |
| --- | --- | --- |
| `client.Metrics.CPU.Total`, `.Percent` | `CPUTotal`, `CPUPerCore` | Copy; clamp to 0 to 100 |
| `Memory.Virtual.UsedPercent`, `.Used`, `.Total` | `MemUsedPct`, `MemUsedBytes`, `MemTotalBytes` | Nil `Virtual` means skip the sample's memory fields |
| `Memory.Swap.UsedPercent` | `SwapUsedPct` | Nil means 0 |
| `Network[i].BytesRecv`, `.BytesSent` | `NetRxBps`, `NetTxBps` | Rate from counter deltas (F2); skip loopback `lo` |
| `Host.TSDNSName` | `DNSName` | Trim trailing dot |
| `Host.Tailkitd.Peer.Online` | `Status` input | See 5.2 |
| `container.Summary.State` | container `State` | Lower-case; one of created, running, paused, restarting, exited, dead |

### 16.4 Per-host supervisor (`internal/collector`)

One goroutine tree per discovered host, owned by an `errgroup` whose context is cancelled when the host disappears or the process stops.

1. **Probe capabilities** once and then every 60 s: `Docker().Config`, the metrics config and available endpoints. Map `ErrDockerUnavailable`, `ErrMetricsUnavailable` and `ErrPermissionDenied` to `Capabilities.Reasons`; a 404 from an old agent means "unavailable".
2. **Metrics loop** (if `Caps.Metrics`): `Metrics().All(ctx, fn)` inside the reconnect wrapper from F2. Each event goes through the aggregator, then `Store.PushSample`.
3. **Ports loop** (if `Caps.Metrics`): `Metrics().Ports(ctx)` for the first snapshot, then `Metrics().Stream().Ports(ctx, fn)`.
4. **Docker loop** (if `Caps.Docker`): initial `Containers().List`, then `Docker().Stream(ctx, fn)` with the filter and 300 ms debounce from F6; refetch Swarm and Compose only when their capability flags are true.
5. **Status loop**: recomputes `Status` every second from last-sample age and failure counts; writes only when it changes.
6. On any loop's context cancellation, return `ctx.Err()`; on stream errors, log at `warn`, increment the failure counter, back off, retry.

On-demand streams (container stats and logs) are created by `collector.Subscribe(host, containerID, kind)` which returns a handle; a map keyed by `(host, id, kind)` holds the reference count and the 10 s close timer.

### 16.5 Aggregator

Input: raw `client.Metrics` at the stream interval. Output: one `Sample` per `HISTORY_STEP` bucket.

1. Compute `bucket = floor(receivedAt / step) * step` using the injected clock, not the payload.
2. For counters (network bytes), compute the instantaneous rate against the previous raw sample; drop negative deltas (counter reset) and rates above 1 TB/s (implausible).
3. Accumulate gauges and rates into running sums with a count.
4. When a sample arrives in a later bucket, emit the previous bucket as the mean of its inputs and start a new one. Empty buckets are not emitted; the series API fills gaps with `null`.

### 16.6 Hub (`internal/hub`)

```go
type Event struct { ID uint64; Topic, Name string; Data any }
func (h *Hub) Publish(e Event)                                   // assigns ID, appends to replay ring
func (h *Hub) Subscribe(topics []string, lastID uint64) (*Sub, error) // ErrGapTooOld -> caller sends resync
func (s *Sub) C() <-chan Event
func (s *Sub) Close()
```

Each subscriber has a buffered channel of 256. When full: if the pending event is `metrics.sample`, replace the newest queued sample for that host; otherwise close the subscription with reason `slow-consumer` so the handler emits `resync` and ends the response.

### 16.7 Error mapping

| Condition | Source | HTTP | Problem `type` suffix | UI behaviour |
| --- | --- | --- | --- | --- |
| Unknown host | state | 404 | `host-not-found` | Not-found page |
| Host offline or no agent | status | 503 | `host-unreachable` | Banner with last-seen time |
| `ErrDockerUnavailable`, `ErrMetricsUnavailable`, Swarm not permitted | tailkit | 409 | `capability-unavailable` | Disabled tab with reason |
| `ErrPermissionDenied` | tailkit | 403 | `permission-denied` | Disabled tab, "blocked by node config or ACL" |
| Upstream timeout | context | 504 | `upstream-timeout` | Retry button |
| `ErrUnauthorized`, other tailkit errors | tailkit | 502 | `upstream-error` | Generic error with request ID |
| Bad query parameter | api | 400 | `invalid-parameter` | Inline validation |
| SSE capacity reached | hub | 503 | `too-many-streams` | Falls back to 10 s polling |

## 17. Frontend specification

### 17.1 Stack and structure

React 18, TypeScript (strict), Vite, Tailwind with shadcn/ui primitives, TanStack Router (file-based), TanStack Query for server state, TanStack Table plus `@tanstack/react-virtual` for tables, `uPlot` for time series, Vitest and React Testing Library for unit tests, MSW for API mocks, Playwright for end-to-end tests. Types are generated from `api/openapi.yaml` with `openapi-typescript`; hand-written API types are not allowed.

```
web/src/
  routes/            __root.tsx  index.tsx  hosts.$host.tsx  hosts.$host.{metrics,processes,ports,docker}.tsx
  features/fleet/    HostTable, FleetSummary, StatusBadge, filters
  features/host/     HostHeader, MetricCharts, ProcessTable, PortTable
  features/docker/   ContainerTable, ContainerDrawer, StatsPanel, LogViewer, SwarmTable, ComposeTable
  lib/api/           generated types, fetch client, query keys
  lib/stream/        useEventStream (SSE) feeding the Query cache
  lib/format/        bytes, rate, percent, duration, relativeTime
  components/        shared UI (Chart, Sparkline, EmptyState, ErrorState, Skeleton)
```

### 17.2 Routes

| Route | Screen | URL state |
| --- | --- | --- |
| `/` | Fleet overview | `?status=&role=&environment=&tag=&q=&sort=cpu&order=desc` |
| `/hosts/:host` | Redirects to `/hosts/:host/metrics` | none |
| `/hosts/:host/metrics` | Charts, vitals, disk usage | `?window=15m` |
| `/hosts/:host/processes` | Top processes | `?sort=cpu` |
| `/hosts/:host/ports` | Listening sockets | `?q=&proto=` |
| `/hosts/:host/docker` | Containers, Swarm, Compose sub-tabs | `?view=containers&state=&q=&container=<id>` |

All filter, sort and selection state lives in the URL so any view can be shared as a link and survives a reload.

### 17.3 Screens

**Fleet overview.** A header strip of four cards (hosts by status, mean CPU, mean memory, containers running) from `GET /fleet/summary`. Below, a virtualised table with columns: Status (icon + text), Name, Role, Environment, CPU % with 60-point sparkline, Memory % with bar, Network (rx/tx rates), Containers, Last sample. Sorting on any column; row click opens the host. A toolbar holds the search box and multi-select filters. Footer shows "N of M hosts".

**Host header (all host tabs).** Name, status badge, tags, tailnet IPs (copyable), OS and arch, uptime, last-sample age, and a tab bar. Tabs for unavailable capabilities are disabled with a tooltip carrying the reason from `capabilities.reasons`.

**Metrics tab.** Window selector (5 m, 15 m, 1 h). Four synchronised charts: CPU total with per-core toggle, memory and swap, network rx/tx, and a disk usage table (mount, used, total, percent bar). Hovering any chart moves a shared cursor and shows a tooltip with all series values at that time. A "live" pill pulses while samples arrive and turns grey with "paused" when the host is stale.

**Processes tab.** Table (PID, name, status, CPU %, memory RSS, command line truncated with tooltip). Heading reads "Top N by CPU on this host" (decision D5).

**Ports tab.** Table (port, protocol, address, PID, process). A recently bound or released row is highlighted for 3 s.

**Docker tab.** Sub-tabs Containers, Swarm services, Compose. Containers table: State chip, Name, Image, Ports, Status text, Created. Row click opens a right-hand **drawer** with Overview (inspect fields), Stats (live CPU %, memory, network and block I/O charts), and Logs (follow toggle, tail length 200 to 5000, text filter, wrap toggle). The drawer URL (`?container=`) is shareable.

### 17.4 Data fetching and live updates

- Query keys: `['fleet','summary']`, `['hosts', filters]`, `['host', name]`, `['metrics', name, window]`, `['processes', name]`, `['ports', name]`, `['containers', name]`.
- `useEventStream(topics)` opens one `EventSource` for the current page's topics. Each event updates the cache with `queryClient.setQueryData` (for example `metrics.sample` appends to the cached series and trims to the window; `host.updated` replaces a host row). No component polls while the stream is connected.
- On `resync`, or on reconnect after more than 30 s, the hook invalidates all active queries.
- If the stream cannot be opened (`503 too-many-streams`) or fails three times in a row, queries fall back to `refetchInterval: 10_000` and a small banner says "Live updates paused".
- Container stats and logs use their own `EventSource` per open drawer, closed on unmount.

### 17.5 Formatting rules

| Value | Format | Example |
| --- | --- | --- |
| Bytes | IEC, 1 decimal above KiB | `1.5 GiB` |
| Rates | bits per second for network, decimal prefixes | `12.4 Mb/s` |
| Percent | 0 decimals in tables, 1 in tooltips | `41%`, `41.2%` |
| Duration | two largest units | `3d 4h` |
| Timestamps | relative in tables, absolute in tooltips, user locale | `12 s ago` |
| Missing value | an em dash, never `0` or `NaN` | `—` |

### 17.6 States every screen must implement

| State | Requirement |
| --- | --- |
| Loading | Skeleton rows or chart placeholders, no layout shift when data arrives |
| Empty | Explains why (for example "No containers" vs "Docker is not enabled on this host: docker.toml missing") |
| Error | Problem title, detail and request ID, with a Retry button |
| Stale host | Data stays visible, dimmed, with a banner "Last sample 2 m ago" |
| Offline host | Last known values shown read-only with an offline banner |
| Stream lost | Non-blocking banner; data refreshes by polling |

### 17.7 Accessibility and design tokens

- WCAG 2.1 AA: all interactive elements reachable by keyboard, visible focus ring, status never conveyed by colour alone, charts have an accessible data-table toggle, `prefers-reduced-motion` disables sparkline and highlight animation.
- Light and dark themes from CSS variables; status palette defined once (`--status-healthy`, `--status-degraded`, `--status-stale`, `--status-offline`, `--status-noagent`) with 4.5:1 contrast against both backgrounds.
- Keyboard shortcuts: `/` focuses search, `g` then `o` goes to overview, `[` and `]` switch host tabs, `Esc` closes the drawer.
- Performance budget: initial JS under 250 KB gzipped, overview interactive under 1.5 s on a laptop with 200 hosts, no main-thread task over 50 ms while charts update.

## 18. Test catalog and procedures

Section 8 defines the layers. This section lists the concrete cases a reviewer can tick off, how fixtures are produced, and how everything runs in CI. A feature is not done until its rows below pass.

### 18.1 Fixtures

- `scripts/capture-fixtures.sh <host>` calls a real tailkitd through the tailnet and saves each response to `testdata/tailkitd/` (`host.json`, `metrics_all.json`, `metrics_config.json`, `ports.json`, `docker_containers.json`, and SSE captures as `*.sse`). It scrubs hostnames, IPs and environment variables before writing.
- `fakesource` serves fixtures by `(hostname, method, path)`. `fakesource.SSE(name, id, v)` builds a valid SSE frame (`event:`, `id:`, `data:` lines and a blank line) so streams decode through tailkit's real parser.
- Golden files are rewritten only with `go test ./... -update` and reviewed in the pull request diff.

### 18.2 Backend cases

| ID | Level | Scenario | Expected |
| --- | --- | --- | --- |
| F1-01 | component | A new online host appears in `Discover` | Supervisor starts within one interval; `host.updated` emitted once |
| F1-02 | component | A host disappears | Supervisor context cancelled; `host.removed` emitted; `goleak` clean |
| F1-03 | component | `Discover` returns an unchanged list | Zero events |
| F1-04 | component | `Discover` returns an error | Previous hosts and statuses retained; error logged; retried next interval |
| F1-05 | concurrency | 20 simultaneous discovery triggers | Exactly one `Discover` call (`singleflight`) |
| F1-06 | component | More hosts than `TAILFLOW_MAX_HOSTS` | Extras ignored with one warning |
| F2-01 | unit | Two counter samples 2 s apart, +2000 bytes | Rate equals 1000 B/s |
| F2-02 | unit | Counter decreases (reset) | Sample dropped; no negative rate |
| F2-03 | unit | Rate above 1 TB/s | Dropped as implausible |
| F2-04 | property | Push N > capacity items | `Len()` equals capacity; order preserved; oldest evicted |
| F2-05 | component | Stream fails repeatedly (fake clock) | Delays within `[0, min(30 s, 1 s * 2^n)]`; attempt counter resets after 30 s healthy |
| F2-06 | unit | Six raw samples across two 5 s buckets | Two emitted samples, each the mean of its inputs |
| F2-07 | unit | Payload with nil `Memory.Virtual` | No panic; memory fields skipped |
| F2-08 | component | 3 consecutive stream failures, then a success | Status `degraded` then `healthy`; counter reset |
| F3-01 | property | Random filter and sort query strings | Never panics; stable order; result count never exceeds host count |
| F4-01 | property | LTTB on random series | Output length at most N; first and last points preserved; global max within a bucket retained |
| F4-02 | unit | Window longer than stored data | Returns what exists; `null` gaps; no error |
| F5-01 | golden | Replay recorded snapshot, bound, released events | Final set equals the last snapshot |
| F5-02 | unit | Duplicate `bound` for the same tuple | Idempotent |
| F6-01 | component | 50 Docker events in 100 ms | Exactly one `Containers().List` call |
| F6-02 | unit | Events `exec_start`, `attach`, `top` | Ignored; no refetch |
| F6-03 | component | `ErrDockerUnavailable` from `Config` | `capabilities.docker=false`, reason `not-configured`; no error status |
| F6-04 | unit | Inventory diff with one added, one removed, one changed container | Three `docker.container.changed` events |
| F7-01 | component | Two subscribers open then close one by one | One upstream stream; closed 10 s after the last unsubscribe |
| F7-02 | unit | Recorded stats frame vs `docker stats` output | CPU % and memory within 0.1 of the reference |
| F7-03 | unit | 10,000 log lines pushed | Buffer holds the last 5,000 |
| F8-01 | concurrency | Subscriber never reads | Server memory bounded; subscription closed with `resync` |
| F8-02 | component | Reconnect with `Last-Event-ID` inside the replay window | Missed events delivered in order, none duplicated |
| F8-03 | component | Reconnect with an ID older than the replay ring | `resync` sent |
| F8-04 | component | 51st SSE client with the default cap | `503` problem `too-many-streams` |
| F9-01 | component | Old agent returns 404 for metrics config | Capability absent, host not failed |
| F9-02 | component | `ErrPermissionDenied` on Docker | Reason `permission-denied` |
| F10-01 | unit | `GET /unknown` and `GET /hosts/x` in a browser (Accept: text/html) | `index.html`; API paths never get the fallback |
| F10-02 | unit | `TAILFLOW_BASE_PATH=/ui` | All routes and asset URLs work under `/ui` |
| F10-03 | lint | Import graph | Only allowed packages import `tailkit` or `tailkit/client` (`depguard`) |
| API-01 | contract | Every endpoint, success and error cases | Response validates against `openapi.yaml` (`kin-openapi`); errors are `application/problem+json` |

### 18.3 Frontend cases

| ID | Level | Scenario | Expected |
| --- | --- | --- | --- |
| UI-01 | unit | Host table receives 500 rows | Only visible rows rendered (virtualisation) |
| UI-02 | unit | Sorting by CPU descending | Rows reorder; URL updates |
| UI-03 | unit | `metrics.sample` event arrives | Cached series appended and trimmed; cell updates without refetch |
| UI-04 | unit | `resync` event | All active queries invalidated |
| UI-05 | unit | Format helpers (bytes, rate, percent, duration, missing) | Match the table in 17.5 |
| UI-06 | unit | Host with `capabilities.docker=false` | Docker tab disabled with the reason as tooltip |
| UI-07 | unit | Stream open fails three times | Banner shown; polling every 10 s starts |

### 18.4 End-to-end scenarios (Playwright)

The e2e build runs Tailflow with `TAILFLOW_MODE=local` against a scripted `fakesource` fleet (build tag `e2e`).

1. Overview loads, shows all hosts, sorts by CPU.
2. A filter in the URL (`?status=degraded`) restores on reload.
3. Host detail renders four charts; switching the window changes the x-axis range.
4. An injected sample updates a CPU cell within 1 s without a page reload.
5. A host goes offline: banner appears and values dim.
6. A host without Docker shows the disabled tab and its reason.
7. Opening a container drawer streams logs; scrolling up pauses follow.
8. Dropping the SSE connection shows the banner, then recovers and clears it.
9. `axe-core` finds no critical violations on overview, metrics, ports and docker pages.
10. The app works when served under `/ui`.

### 18.5 Load and soak

`scripts/loadgen` drives `fakesource` with 200 hosts, 50 containers each, a 2 s metrics interval and 10 simulated browser sessions. Pass criteria after 30 minutes: resident memory at most 150 MB, p95 API latency at most 100 ms, goroutine count flat (no growth over the last 20 minutes), zero dropped structural events. Runs nightly, not on every pull request.

### 18.6 CI pipeline (`.github/workflows/ci.yml`)

| Job | Runs | Triggers |
| --- | --- | --- |
| `lint` | `golangci-lint`, `staticcheck`, `depguard`, `eslint`, `tsc --noEmit`, `spectral lint api/openapi.yaml` | every push |
| `go-test` | `go test -race -coverprofile ./...`; fails if `state`, `collector` or `hub` coverage is under 80% | every push |
| `web-test` | `vitest run --coverage` | every push |
| `contract` | Regenerate Go and TS types; `git diff --exit-code` | every push |
| `integration` | `docker compose -f deploy/compose.test.yml up` with a real tailkitd container, then `go test -tags integration` | merge to main, nightly |
| `e2e` | Build with `-tags e2e`, run Playwright | merge to main, nightly |
| `load` | `scripts/loadgen` | nightly |

**Flake policy.** A test that fails intermittently is quarantined (skipped with a linked issue) within 24 hours and fixed within a week. Automatic retries are not used because they hide real races.

## 19. Build, packaging and security

### 19.1 Developer workflow

| Target | What it does |
| --- | --- |
| `make dev` | Runs the Go server in `local` mode with a scripted fake fleet and the Vite dev server proxying `/api` to it; no tailnet needed |
| `make codegen` | Regenerates Go and TypeScript types from `api/openapi.yaml` |
| `make web` | Builds the SPA into `web/dist` |
| `make build` | Builds `bin/tailflow` with the SPA embedded and the version injected via `-ldflags` |
| `make test` / `make test-race` | Go unit and component tests, with and without the race detector |
| `make lint` | Go and web linters, `depguard`, OpenAPI lint |
| `make e2e`, `make integration`, `make loadgen` | The suites from sections 18.4, 18.2 and 18.5 |
| `make fixtures HOST=vps-1` | Captures fixtures from a real node |
| `make docker` | Builds the container image |

### 19.2 Container image

```dockerfile
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/tailflow ./cmd/tailflow

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tailflow /tailflow
ENTRYPOINT ["/tailflow"]
```

Verify `CGO_ENABLED=0` works with the pinned tailkit in M0; if a dependency forces CGO, switch the build stage to `CGO_ENABLED=1` and the runtime to a glibc-based distroless image. The state directory must be a writable volume for the non-root user.

**Health check.** Distroless has no `curl`, and in `tailnet` mode the app does not listen on loopback. Tailflow therefore adds a `tailflow healthcheck` subcommand that calls a loopback-only listener (`TAILFLOW_HEALTH_ADDR`, default `127.0.0.1:8081`, serving `/api/v1/healthz` only). Docker Compose uses it as the container health check.

```yaml
services:
  tailflow:
    image: ghcr.io/wf-pro-dev/tailflow:${TAILFLOW_VERSION:-latest}
    environment:
      TS_AUTHKEY: ${TS_AUTHKEY}
      TAILFLOW_HOSTNAME: tailflow
    volumes:
      - tailflow-state:/var/lib/tailflow
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "/tailflow", "healthcheck"]
      interval: 30s
volumes:
  tailflow-state:
```

### 19.3 Releases

Semantic versioning with tags `vX.Y.Z`. A tag triggers: full test suite, GoReleaser (linux amd64 and arm64 binaries with the SPA embedded), a multi-arch image pushed to `ghcr.io`, and a changelog generated from conventional commits. The version is reported by `GET /api/v1/meta`. The OpenAPI file is versioned with the app; any change that alters an existing response shape requires `/api/v2`.

### 19.4 Security checklist

- Read-only by construction: the router rejects every method except `GET`, `HEAD` and `OPTIONS` with `405` (tested).
- The `{host}` path parameter must match a host in the current discovered set, otherwise `404`. Tailflow never forwards an arbitrary hostname to tailkit, so it cannot be used to probe other tailnet machines. Container IDs must match `^[a-f0-9]{12,64}$`.
- Response headers: `Content-Security-Policy: default-src 'self'; img-src 'self' data:`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Cache-Control: no-store` on `/api/*`. No CORS headers.
- The log viewer renders text only: ANSI sequences become styled `<span>`s, never raw HTML; `dangerouslySetInnerHTML` is banned by lint.
- Container logs may contain secrets. v1 shows them to any authenticated tailnet caller (see U3); document this in the README.
- Supply chain: `govulncheck` and `npm audit` in CI, Dependabot enabled, images built from pinned base digests.

## 20. Migration plan into tailkitd

**Preconditions:** milestones M0 to M5 complete, upstream U3 released, API v1 frozen, and tailkitd built on a tailkit version that contains the `client` package.

1. Move `internal/{state,collector,hub,api,source}` and `web/` into tailkitd as `internal/ui/...` and rewrite import paths. `state`, `hub` and `api` need no other change by design.
2. Replace `tailkitsrc` with a `localfleet`: `Discover` uses the daemon's existing tsnet server; `Host(name)` returns the normal scope for remote hosts and, for the local host, a scope over a `ClientBackend` that calls the daemon's own handlers in-process (a `ResponseWriter` implementing `Flusher` over an `io.Pipe`, so SSE works).
3. Mount the handler at `/ui/` behind the daemon's existing `tailkit.AuthMiddleware`, enabled by a new `ui.toml` (`enabled = false` by default until stable).
4. Release pipeline: build the SPA before GoReleaser and embed it; budget at most 5 MB of extra binary size.
5. Version skew: add an integration job that runs the UI against the previous two tailkitd releases.
6. Keep the standalone image for one release as a thin wrapper with a deprecation notice.

**Acceptance:** opening the UI from any node lists every host including itself; the local host is served without a network round-trip; the e2e suite from section 18.4 passes against the embedded build.

## 21. Delivery plan and definition of done

Estimates are rough, for one developer working part-time; treat them as ordering, not commitments.

### 21.1 Milestone checklists

**M0: foundations (about 1 week)**

- [ ] Repository skeleton, `go.mod` pinned to `tailkit v0.5.4`, `Makefile`, lint config, CI skeleton
- [ ] `api/openapi.yaml` covering every endpoint in section 7, spectral-clean
- [ ] Codegen for Go and TypeScript; CI check that generated code is current
- [ ] `source.Fleet`, `Clock`, and `fakesource` (fake `ClientBackend`, SSE helpers, fake clock)
- [ ] `CGO_ENABLED=0` build verified; fixtures captured from one real node

**M1: collection core (about 1.5 weeks)**

- [ ] `state.Ring`, `Store`, aggregator; cases F2-01 to F2-08
- [ ] Discovery loop and supervisors; cases F1-01 to F1-06
- [ ] `/hosts`, `/hosts/{host}`, `/metrics`, `/fleet/summary` handlers with API-01 contract tests
- [ ] Measure the tailkit peer-cache latency (risk 3) and record the result

**M2: overview and host detail UI (about 2 weeks)**

- [ ] Router, layout, theme, generated API client
- [ ] Fleet overview table with filters and sparklines; UI-01, UI-02
- [ ] Metrics tab with synchronised uPlot charts and LTTB (F4-01, F4-02)
- [ ] SSE hub and `useEventStream`; F8-01 to F8-04, UI-03, UI-04
- [ ] e2e scenarios 1 to 4 pass

**M3: ports, processes, Docker inventory (about 1.5 weeks)**

- [ ] Ports and processes tabs; F5-01, F5-02
- [ ] Docker inventory, Swarm and Compose tables; F6-01 to F6-04
- [ ] Capability probing and disabled-tab UX; F9-01, F9-02, UI-06

**M4: drill-down and hardening (about 2 weeks)**

- [ ] Container drawer with live stats and logs; F7-01 to F7-03
- [ ] Reconnect, backoff and degraded states end to end; UI-07, e2e scenarios 5 to 8
- [ ] Load test passes the section 18.5 criteria; accessibility scan clean (scenario 9)
- [ ] Upstream U1 consumed if released

**M5: packaging and handover (about 1 week)**

- [ ] Base-path support and embed; F10-01 to F10-03, e2e scenario 10
- [ ] Dockerfile, compose file, health check, release workflow, changelog
- [ ] README with architecture diagram, screenshots, configuration reference, and a "how to move into tailkitd" page (section 20)
- [ ] Security checklist (19.4) verified; U3 released

### 21.2 Definition of done (every change)

- Merged through a pull request with passing CI, including the test rows from section 18 that the change claims.
- `api/openapi.yaml` updated and generated types committed when the API changes.
- No `TODO` without a linked issue; no skipped test without a linked issue.
- Logs for the new code reviewed at `info` and `debug` for signal versus noise.
- UI changes checked with keyboard only and in both themes.
- A one-paragraph note in the changelog; an ADR (short decision record in `docs/adr/`) if a decision in section 13 is added or changed.

### 21.3 Release acceptance demo

On a real tailnet with at least three nodes (one with Docker, one without, one offline):

1. Start Tailflow from the published image; open it from a phone on the tailnet.
2. The overview lists all three nodes with correct statuses within 20 s.
3. Open the Docker node: charts move live; start a container and see it appear within 2 s.
4. Open its drawer; logs follow; stats match `docker stats`.
5. Open the node without Docker: the Docker tab is disabled with the reason.
6. Stop tailkitd on one node: its status changes to `no-agent` within the stale threshold, others are unaffected.
7. Restart Tailflow: the live state returns within 10 s; charts refill over time.
8. Run `make loadgen` and confirm the section 18.5 numbers.

## 22. Glossary

| Term | Meaning |
| --- | --- |
| tailnet | A private network of devices managed by Tailscale |
| tsnet | Tailscale's Go library for embedding a tailnet node in a program |
| tailkit | Go SDK: tsnet helpers plus a typed client for every tailkitd endpoint |
| tailkitd | Per-node daemon exposing Docker, systemd, metrics, files, vars and exec over the tailnet |
| Host | A tailnet machine that runs tailkitd (`client.Host`) |
| Scope | tailkit's fluent handle for one resource, for example `client.HostScope` |
| SSE | Server-Sent Events: a one-way HTTP stream of named, numbered events |
| Ring buffer | Fixed-size circular buffer that overwrites its oldest entry when full |
| LTTB | Largest-Triangle-Three-Buckets: a downsampling algorithm that preserves visual shape |
| Backpressure | Slowing or shedding work when a consumer cannot keep up |
| Supervisor | A goroutine tree that owns the streams for one host and restarts them on failure |
