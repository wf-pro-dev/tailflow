#!/usr/bin/env bash
# Smoke test for Tailflow M1+.
#
# Starts the server in local mode, hits every wired endpoint, and checks
# the responses. Safe to run repeatedly: kills any stale tailflow process
# on the chosen ports first, and cleans up the background server on exit.
#
# Usage:
#   ./scripts/smoke-test.sh [/path/to/tailflow/binary]
#
# Defaults to ./bin/tailflow (run `make build` first) or /tmp/tailflow/tailflow.

set -euo pipefail

BINARY="${1:-./bin/tailflow}"
if [ ! -x "$BINARY" ]; then
	BINARY="/tmp/tailflow/tailflow"
fi
if [ ! -x "$BINARY" ]; then
	echo "ERROR: tailflow binary not found. Run 'make build' or pass the path." >&2
	echo "Usage: $0 [/path/to/tailflow]" >&2
	exit 1
fi

# Use non-default ports so we don't collide with a real dev server.
MAIN_PORT=9090
HEALTH_PORT=9091
MAIN_ADDR="127.0.0.1:${MAIN_PORT}"
HEALTH_ADDR="127.0.0.1:${HEALTH_PORT}"
BASE="http://${MAIN_ADDR}"

# --- Cleanup any stale process on our ports --------------------------------
echo ">> killing any stale tailflow on ports ${MAIN_PORT}/${HEALTH_PORT}..."
pkill -f "tailflow.*serve.*${MAIN_PORT}" 2>/dev/null || true
pkill -f "tailflow.*serve.*${MAIN_ADDR}" 2>/dev/null || true
# Also free the port if something else holds it (best-effort).
fuser -k "${MAIN_PORT}/tcp" 2>/dev/null || true
fuser -k "${HEALTH_PORT}/tcp" 2>/dev/null || true
sleep 0.5

# --- Start the server ------------------------------------------------------
echo ">> starting $BINARY serve on ${MAIN_ADDR} (health on ${HEALTH_ADDR})..."
TAILFLOW_MODE=local \
TAILFLOW_LISTEN="${MAIN_ADDR}" \
TAILFLOW_HEALTH_ADDR="${HEALTH_ADDR}" \
TAILFLOW_LOG_LEVEL=warn \
"$BINARY" serve > /tmp/tailflow-smoke.log 2>&1 &
SRV_PID=$!

# Ensure we kill the server on exit, even on error.
trap 'kill $SRV_PID 2>/dev/null || true; wait $SRV_PID 2>/dev/null || true' EXIT

# --- Wait for readiness ----------------------------------------------------
echo ">> waiting for server to be ready..."
for i in $(seq 1 30); do
	if curl -sf "${BASE}/api/v1/healthz" > /dev/null 2>&1; then
		echo "   ready after ${i} attempts"
		break
	fi
	if [ "$i" -eq 30 ]; then
		echo "ERROR: server did not become ready in 3s" >&2
		echo "--- server log ---" >&2
		cat /tmp/tailflow-smoke.log >&2
		exit 1
	fi
	sleep 0.1
done

# --- Run the checks --------------------------------------------------------
echo ""
echo "=== GET /api/v1/healthz ==="
curl -sf "${BASE}/api/v1/healthz" | python3 -m json.tool

echo ""
echo "=== GET /api/v1/meta ==="
curl -sf "${BASE}/api/v1/meta" | python3 -m json.tool

echo ""
echo "=== GET /api/v1/hosts (empty fleet in local mode) ==="
curl -sf "${BASE}/api/v1/hosts" | python3 -m json.tool

echo ""
echo "=== GET /api/v1/fleet/summary ==="
curl -sf "${BASE}/api/v1/fleet/summary" | python3 -m json.tool

echo ""
echo "=== GET /api/v1/hosts/nonexistent (expect 404 problem+json) ==="
curl -s -w "\nHTTP %{http_code}\n" "${BASE}/api/v1/hosts/nonexistent"

echo ""
echo "=== GET /api/v1/hosts/nonexistent/metrics (expect 404 problem+json) ==="
curl -s -w "\nHTTP %{http_code}\n" "${BASE}/api/v1/hosts/nonexistent/metrics"

echo ""
echo "=== GET /api/v1/stream (expect 501 not-implemented) ==="
curl -s -w "\nHTTP %{http_code}\n" "${BASE}/api/v1/stream"

echo ""
echo "=== healthcheck subcommand (hits ${HEALTH_ADDR}) ==="
"$BINARY" healthcheck -addr "${HEALTH_ADDR}"
echo "healthcheck exit: $?"

echo ""
echo ">> all smoke tests passed"
