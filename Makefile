# Tailflow Makefile.
# Targets follow the design §19.1 developer workflow table.

SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

# --- Tooling ----------------------------------------------------------------

GO ?= go
GOTESTSUM ?= $(shell command -v gotestsum || echo "")
NODE ?= node
NPM ?= npm
NPX ?= npx
GOLANGCI_LINT ?= $(shell command -v golangci-lint || echo "")
OAPI_CODEGEN ?= $(shell command -v oapi-codegen || echo "")
SPECTRAL ?= $(shell command -v spectral || echo "")

# Force local toolchain: tailkit's go.mod declares a future Go version, but the
# code itself does not use 1.26-specific features. Setting GOTOOLCHAIN=local
# prevents `go` from trying to auto-download a toolchain that does not exist.
export GOTOOLCHAIN := local

# --- Paths -------------------------------------------------------------------

BIN_DIR := bin
WEB_DIR := web
WEB_DIST := $(WEB_DIR)/dist
OPENAPI_SPEC := api/openapi.yaml
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LD_FLAGS := -s -w -X main.version=$(VERSION)

# --- Help --------------------------------------------------------------------

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Tailflow targets:\n\n"} /^[a-zA-Z_-]+:.*?##/ {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# --- Dev ---------------------------------------------------------------------

.PHONY: dev
dev: ## Run the Go server in local mode against a scripted fake fleet (no tailnet needed)
	@echo ">> starting tailflow in local mode with fake source"
	TAILFLOW_MODE=local TAILFLOW_LISTEN=127.0.0.1:8080 $(GO) run ./cmd/tailflow serve

# --- Codegen ----------------------------------------------------------------

.PHONY: codegen
codegen: codegen-go codegen-ts ## Regenerate Go and TypeScript types from api/openapi.yaml

.PHONY: codegen-go
codegen-go: ## Regenerate Go server types from OpenAPI
	@command -v $(OAPI_CODEGEN) >/dev/null 2>&1 || { \
		echo "oapi-codegen not found. Install with:"; \
		echo "  go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest"; \
		exit 1; }
	$(OAPI_CODEGEN) --config scripts/oapi-codegen.yaml $(OPENAPI_SPEC)

.PHONY: codegen-ts
codegen-ts: ## Regenerate TypeScript types from OpenAPI
	cd $(WEB_DIR) && $(NPM) install --silent
	cd $(WEB_DIR) && $(NPX) openapi-typescript ../$(OPENAPI_SPEC) -o src/lib/api/generated.ts

# --- Web ---------------------------------------------------------------------

.PHONY: web
web: ## Build the SPA into web/dist
	cd $(WEB_DIR) && $(NPM) install --silent
	cd $(WEB_DIR) && $(NPM) run build

.PHONY: web-dev
web-dev: ## Start the Vite dev server
	cd $(WEB_DIR) && $(NPM) run dev

# --- Build -------------------------------------------------------------------

.PHONY: build
build: web ## Build the single binary with the SPA embedded
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LD_FLAGS)" -o $(BIN_DIR)/tailflow ./cmd/tailflow

.PHONY: build-go
build-go: ## Build only the Go binary (no SPA rebuild)
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LD_FLAGS)" -o $(BIN_DIR)/tailflow ./cmd/tailflow

# --- Test --------------------------------------------------------------------

.PHONY: test
test: ## Go unit and component tests
	$(GO) test ./...

.PHONY: test-race
test-race: ## Go tests with the race detector
	$(GO) test -race ./...

.PHONY: test-cover
test-cover: ## Go tests with coverage report
	$(GO) test -race -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: web-test
web-test: ## Run the web unit tests (Vitest)
	cd $(WEB_DIR) && $(NPM) install --silent && $(NPM) run test -- --run

# --- Lint --------------------------------------------------------------------

.PHONY: lint
lint: lint-go lint-web lint-openapi ## Run all linters

.PHONY: lint-go
lint-go: ## golangci-lint + depguard import-graph check
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || { \
		echo "golangci-lint not found. Install with:"; \
		echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; }
	$(GOLANGCI_LINT) run ./...

.PHONY: lint-web
lint-web: ## eslint + tsc --noEmit
	cd $(WEB_DIR) && $(NPM) install --silent && $(NPM) run lint && $(NPM) run typecheck

.PHONY: lint-openapi
lint-openapi: ## Spectral lint of api/openapi.yaml
	@command -v $(SPECTRAL) >/dev/null 2>&1 || { \
		echo "spectral not found. Install with:"; \
		echo "  npm install -g @stoplight/spectral-cli"; \
		exit 1; }
	$(SPECTRAL) lint $(OPENAPI_SPEC)

# --- Contract check ---------------------------------------------------------

.PHONY: contract
contract: codegen ## Verify generated code is up to date (CI gate)
	@git diff --exit-code -- internal/api/ web/src/lib/api/generated.ts || { \
		echo ""; \
		echo "ERROR: generated code is out of date. Run 'make codegen' and commit the result."; \
		exit 1; }

# --- Integration / e2e / load -----------------------------------------------

.PHONY: integration
integration: ## Run the integration suite against a real tailkitd (Docker Compose)
	docker compose -f deploy/compose.test.yml up --abort-on-container-exit --exit-code-from tailflow

.PHONY: e2e
e2e: ## Run the Playwright end-to-end suite against a fakesource-backed server
	cd $(WEB_DIR) && $(NPM) run e2e

.PHONY: loadgen
loadgen: ## Drive fakesource with 200 hosts / 50 containers / 10 browsers for 30 minutes
	$(GO) run ./scripts/loadgen --duration 30m

# --- Fixtures ----------------------------------------------------------------

.PHONY: fixtures
fixtures: ## Capture fixtures from a real tailkitd node (HOST=...)
	@test -n "$(HOST)" || { echo "Usage: make fixtures HOST=vps-1"; exit 1; }
	bash scripts/capture-fixtures.sh $(HOST)

# --- Docker ------------------------------------------------------------------

.PHONY: docker
docker: ## Build the container image
	docker build -f deploy/Dockerfile -t tailflow:dev --build-arg VERSION=$(VERSION) .

# --- Clean -------------------------------------------------------------------

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) $(WEB_DIST) coverage.out
