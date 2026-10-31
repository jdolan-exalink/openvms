# OpenVMS developer entry points. `make help` lists them.
SHELL := /bin/bash
# Versions come from the VERSION files (docs/VERSIONING.md); the commit is a separate build label.
VERSION ?= $(shell cat VERSION 2>/dev/null | tr -d '[:space:]')
AGENT_VERSION ?= $(shell cat deploy/agent/VERSION 2>/dev/null | tr -d '[:space:]')
INFRA := postgres valkey nats seaweedfs mosquitto frigate-helvecia frigate-cayasta
# Development compose: base file + simulated Frigates + the optional mqtt profile (mosquitto).
DEV_COMPOSE := docker compose -f docker-compose.yml -f docker-compose.dev.yml --profile mqtt

export DATABASE_URL ?= postgres://openvms:openvms@localhost:5432/openvms?sslmode=disable
export VALKEY_ADDR ?= localhost:6379
export NATS_URL ?= nats://localhost:4222
export S3_ENDPOINT ?= http://localhost:8333
export S3_ACCESS_KEY ?= openvms
export S3_SECRET_KEY ?= openvms-dev-secret

.PHONY: help
help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

.PHONY: up
up: ## Build and start the full stack in Docker (web on http://localhost:8000)
	VERSION=$(VERSION) AGENT_VERSION=$(AGENT_VERSION) docker compose up -d --build

.PHONY: install
install: ## First install: check Docker + Compose v2, create .env with random secrets, build, up, wait for health (deploy/install.sh)
	./deploy/install.sh

.PHONY: deploy
deploy: ## Deploy the local stack: preflight, DB dump, rollback tags, build, up, health check (deploy/deploy.sh)
	./deploy/deploy.sh

.PHONY: rollback
rollback: ## Retag images to rollback-$(TAG) and restart without build (no DB restore): make rollback TAG=<ts>
	./deploy/rollback.sh "$(TAG)"

.PHONY: release-notes
release-notes: ## Print the CHANGELOG.md section of a version (default: VERSION): make release-notes [V=0.4.0]
	@awk -v v="$(or $(V),$(VERSION))" 'index($$0, "## [" v "]") == 1 {on=1; next} on && /^## \[/ {exit} on' CHANGELOG.md

.PHONY: down
down: ## Stop the stack (keeps volumes)
	docker compose down

.PHONY: dev
dev: ## Start infra + simulated Frigates (docker-compose.dev.yml) in Docker, run API and web locally with live reload of the web
	$(DEV_COMPOSE) up -d --build --wait $(INFRA)
	pnpm install
	$(MAKE) -j2 dev-api dev-web

.PHONY: dev-down
dev-down: ## Stop the development stack started by `make dev` (keeps volumes)
	$(DEV_COMPOSE) down

.PHONY: dev-api
dev-api:
	go run ./apps/api

.PHONY: dev-web
dev-web:
	pnpm dev

.PHONY: proto
proto: ## Compile Protobuf contracts to Go
	@mkdir -p gen/go/openvms/v1
	protoc -I. --go_out=. --go_opt=module=github.com/jdolan-exalink/openvms --go-grpc_out=. --go-grpc_opt=module=github.com/jdolan-exalink/openvms proto/openvms/v1/*.proto

.PHONY: generate
generate: proto ## Regenerate Go server, TS client and proto stubs
	go generate ./...
	pnpm generate

.PHONY: lint
lint: ## Go vet + golangci-lint (if installed) + eslint + typecheck
	go vet ./...
	@if command -v golangci-lint >/dev/null; then golangci-lint run; else echo "golangci-lint not installed, skipped"; fi
	pnpm lint
	pnpm typecheck

.PHONY: test
test: ## Unit tests (Go + web)
	go test -race ./...
	pnpm test

.PHONY: test-integration
test-integration: ## Integration tests against real containers (needs Docker)
	go test -race -tags integration ./...

.PHONY: web-apk-refresh-test
web-apk-refresh-test: ## Tests of the weekly web image apk refresh (stubbed docker)
	bash deploy/maintenance/web_apk_refresh_test.sh

.PHONY: agent-version-check
agent-version-check: ## Fail when the edge agent changed without a version bump (see docs/VERSIONING.md)
	bash tools/agent_version_test.sh
	tools/agent_version.sh check

.PHONY: agent-bump
agent-bump: ## Bump the agent version: make agent-bump LEVEL=patch|minor|major NOTE="what changed"
	tools/agent_version.sh bump $(or $(LEVEL),patch) --note "$(NOTE)"

.PHONY: hooks
hooks: ## Install the git hooks (bumps the agent version when a commit needs it)
	git config core.hooksPath tools/hooks

.PHONY: build
build: ## Build Go binaries into bin/ and the web bundle
	@mkdir -p bin
	for app in api worker frigate-mock; do \
		CGO_ENABLED=0 go build -trimpath -ldflags "-X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Version=$(VERSION)" -o bin/$$app ./apps/$$app; \
	done
	CGO_ENABLED=0 go build -trimpath -ldflags "-X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Version=$(AGENT_VERSION)" -o bin/edge-agent ./apps/edge-agent
	sha256sum bin/edge-agent | awk '{print $$1}' > bin/edge-agent.sha256
	go env GOARCH > bin/edge-agent.goarch
	printf '%s' "$(AGENT_VERSION)" > bin/edge-agent.version
	pnpm build

.PHONY: logs
logs: ## Follow logs of the running stack
	docker compose logs -f --tail=100
