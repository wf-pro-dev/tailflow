#!/usr/bin/env bash
# Captures test fixtures from a real tailkitd node by calling each endpoint
# through the tailnet via tailkit. The captured responses are scrubbed of
# hostnames, IPs, and environment variables before being written to
# testdata/tailkitd/.
#
# Usage: make fixtures HOST=vps-1
#
# In M0 this script is a stub — it prints the planned capture list so the
# fixture-capture contract is documented. The actual capture logic lands in
# M1 once the tailkitsrc production Fleet is wired.
set -euo pipefail

HOST="${1:?usage: capture-fixtures.sh <host>}"
OUTDIR="$(dirname "$0")/../testdata/tailkitd"

echo "Capturing fixtures from host '$HOST' into $OUTDIR"
echo ""
echo "Planned captures (one file per endpoint):"
echo "  host.json                          GET /host"
echo "  metrics_all.json                   GET /integrations/metrics/all"
echo "  metrics_config.json                GET /integrations/metrics/config"
echo "  ports.json                         GET /integrations/metrics/ports"
echo "  docker_containers.json             GET /integrations/docker/containers"
echo "  docker_config.json                 GET /integrations/docker/config"
echo "  docker_swarm_services.json         GET /integrations/docker/swarm/services"
echo "  docker_compose_projects.json       GET /integrations/docker/compose/projects"
echo ""
echo "SSE captures (saved as .sse):"
echo "  metrics_all.sse                    GET /integrations/metrics/all/stream  (10s window)"
echo "  ports_stream.sse                   GET /integrations/metrics/ports/stream  (10s window)"
echo "  docker_events.sse                  GET /integrations/docker/stream  (10s window)"
echo ""
echo "Scrubbing: hostnames -> 'vps-1', Tailscale IPs -> '100.64.0.5', env vars removed."
echo ""
echo "M0 stub: actual capture requires the tailkitsrc Fleet (lands in M1)."
exit 0
