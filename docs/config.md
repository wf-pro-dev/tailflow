# Configuration reference

Tailflow is configured entirely by environment variables (twelve-factor). All values are validated at startup and every error is reported at once before exiting with status 2.

## Variables

| Variable | Default | Meaning |
|---|---|---|
| `TAILFLOW_MODE` | `tailnet` | `tailnet` serves over tsnet (production); `local` serves plain HTTP on `127.0.0.1` for development and tests |
| `TS_AUTHKEY` | none | Tailscale auth key; required on first start in `tailnet` mode, ignored once state exists in `TS_STATE_DIR` |
| `TS_STATE_DIR` | `/var/lib/tailflow` | tsnet state directory (a persistent volume in Docker) |
| `TAILFLOW_HOSTNAME` | `tailflow` | tsnet hostname (the node appears as `<hostname>` on the tailnet) |
| `TAILFLOW_LISTEN` | `:8080` | Listen address (tailnet or local) |
| `TAILFLOW_BASE_PATH` | `/` | URL prefix the app is mounted under (e.g. `/ui` for embedding in tailkitd) |
| `TAILFLOW_DISCOVERY_INTERVAL` | `15s` | How often `Discover` runs (minimum 5s) |
| `TAILFLOW_HISTORY_WINDOW` | `1h` | Length of each metric ring buffer (5m to 6h) |
| `TAILFLOW_HISTORY_STEP` | `5s` | Aggregation bucket width; must be a multiple of the stream interval |
| `TAILFLOW_CALL_TIMEOUT` | `10s` | Timeout for each unary tailkit call (streams are exempt) |
| `TAILFLOW_STALE_FACTOR` | `3` | A host is `stale` after this many missed stream intervals |
| `TAILFLOW_DEGRADE_THRESHOLD` | `3` | Consecutive stream failures before `degraded` |
| `TAILFLOW_MAX_HOSTS` | `500` | Safety cap on supervised hosts |
| `TAILFLOW_MAX_SSE_CLIENTS` | `50` | Browser SSE connections accepted; extras get `503` |
| `TAILFLOW_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `TAILFLOW_LOG_FORMAT` | `json` | `json` or `text` |
| `TAILFLOW_HEALTH_ADDR` | `127.0.0.1:8081` | Loopback-only listener serving `/api/v1/healthz` in tailnet mode; used by the container health check |

## Startup contract

The process must:

1. Validate config (exit 2 on any error, reporting all problems at once)
2. Start tsnet (tailnet mode) or the local listener
3. Start the discovery loop
4. Serve `/api/v1/healthz` immediately (liveness does not wait for discovery)
5. On SIGINT or SIGTERM: stop accepting requests, cancel all supervisors, close SSE streams with a `resync` hint, and exit within 10s

## Log fields

Every log line carries `time`, `level`, `msg`. Request logs add `request_id`, `method`, `path`, `status`, `duration_ms`. Collector logs add `host` and `stream` (`metrics`, `ports`, `docker-events`, `container-stats`, `container-logs`). Secrets (`TS_AUTHKEY`) are never logged.

## Examples

### Local development (no tailnet)

```bash
TAILFLOW_MODE=local TAILFLOW_LISTEN=127.0.0.1:8080 ./tailflow serve
```

### Production (tailnet)

```bash
export TS_AUTHKEY=tskey-auth-xxxxx  # only needed on first start
export TAILFLOW_HOSTNAME=tailflow
export TS_STATE_DIR=/var/lib/tailflow
./tailflow serve
# Open https://tailflow.<your-tailnet>.ts.net/
```

### Docker

```bash
# .env file
TS_AUTHKEY=tskey-auth-xxxxx
TAILFLOW_VERSION=latest

docker compose -f deploy/docker-compose.yml up -d
```
