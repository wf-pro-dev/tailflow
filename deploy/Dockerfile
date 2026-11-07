# Tailflow — single-binary image with embedded SPA.
# Design §19.2.
#
# Build stages:
#   1. web:   build the SPA with Vite (node:22-alpine)
#   2. build: compile the Go binary with the SPA embedded (golang:1.26)
#   3. runtime: distroless static (no shell, no libc — CGO_ENABLED=0)

# --- Stage 1: SPA ----------------------------------------------------------
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --silent
COPY web/ ./
# The OpenAPI spec is needed if the SPA build references it; copy for safety.
COPY api/openapi.yaml ../api/openapi.yaml
RUN npm run build

# --- Stage 2: Go binary ----------------------------------------------------
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Copy the SPA build output from stage 1.
COPY --from=web /web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/tailflow ./cmd/tailflow

# --- Stage 3: runtime ------------------------------------------------------
# distroless/static has no shell, no libc — perfect for a CGO_ENABLED=0 Go
# binary. The nonroot variant runs as a non-root user (UID 65532).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tailflow /tailflow
# Default to tailnet mode; operators override via env vars.
ENV TAILFLOW_MODE=tailnet \
    TAILFLOW_HOSTNAME=tailflow \
    TAILFLOW_LISTEN=:8080 \
    TAILFLOW_HEALTH_ADDR=127.0.0.1:8081 \
    TS_STATE_DIR=/var/lib/tailflow
# distroless has no shell, so the entrypoint is the binary directly.
ENTRYPOINT ["/tailflow"]
CMD ["serve"]
