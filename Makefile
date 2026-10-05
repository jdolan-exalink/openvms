# OpenVMS developer entry points. `make help` lists them.
SHELL := /bin/bash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
INFRA := postgres valkey nats seaweedfs mosquitto frigate-helvecia frigate-cayasta

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
	VERSION=$(VERSION) docker compose up -d --build

.PHONY: down
down: ## Stop the stack (keeps volumes)
	docker compose down

.PHONY: dev
dev: ## Start infra + mocks in Docker, run API and web locally with live reload of the web
	docker compose up -d --build --wait $(INFRA)
	pnpm install
	$(MAKE) -j2 dev-api dev-web

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

.PHONY: build
build: ## Build Go binaries into bin/ and the web bundle
	@mkdir -p bin
	for app in api worker frigate-mock; do \
		CGO_ENABLED=0 go build -trimpath -ldflags "-X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Version=$(VERSION)" -o bin/$$app ./apps/$$app; \
	done
	pnpm build

.PHONY: logs
logs: ## Follow logs of the running stack
	docker compose logs -f --tail=100
