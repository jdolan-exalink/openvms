# OpenVMS — Repository Architecture & Agent Navigation Guide

OpenVMS is an enterprise federated video management platform (Go 1.26 + React/TypeScript) integrating Frigate NVR sites, ONVIF edge agents, Maps, Vision AI, Alarms, and Evidence exports.

## 1. Applications (`apps/`)

| Path | Role |
|---|---|
| `apps/api` | Main HTTP/REST (`:8080`) and Agent gRPC mTLS (`:9443`) control-plane server |
| `apps/edge-agent` | Site ONVIF discovery, camera provisioning, and mTLS gRPC edge agent (`:7419` / `:7443`) |
| `apps/worker` | Background media/clip-watermarking (`ffmpeg`) and async job processor (`:8081`) |
| `apps/relay` | RTSP/WebRTC stream relay service (`:8554`) |
| `apps/vmsctl` | Administrative CLI (including safe Compose admin password reset) |
| `apps/frigate-mock` | Simulated Frigate NVR server for local dev/testing (`docker-compose.dev.yml`) |
| `apps/web` | React + TypeScript + Vite SPA served behind Caddy (`apps/web/src/{routes,components,lib,api,i18n}`) |

## 2. Backend Domain Packages (`internal/`)

- **API & Auth**: `api` (HTTP handlers, middleware, RBAC, agent gRPC server), `identity` (users, passwords, sessions), `authz` (permissions/roles), `bootstrap` (initial tenant/site/admin seed & reset), `access`.
- **Edge Agent & ONVIF**: `agent` (agent runtime, mTLS client, enrollment, update/reinstall staging), `agentca` (mTLS CA & leaf certs), `agentauth` (enrollment tokens), `agentstatus` (heartbeats), `onvif` (WS-Discovery, SOAP profiles, PTZ, network config).
- **Video, Frigate & Live**: `frigate` (Frigate HTTP client & config sync), `frigatemock`, `media` (live/playback gateway, MSE/WebRTC, signal checks), `videobackend`, `control` (PTZ & camera controls), `realtime` (WebSocket hub).
- **Intelligence, Alarms & Exports**: `events` (Frigate event ingestion, NATS JetStream, S3/SeaweedFS sync), `visionai` (Vision AI rules & matrix), `faces` (face recognition & training), `vehicle` (LPR/plates & attributes), `rules`, `alarms` (alarm state machine & escalation), `notify` (WAHA WhatsApp/webhook notifications), `watermark` & `clipwatermark` (forensic image/video watermarks).
- **Platform, Storage & Maps**: `store` (Postgres repositories & `queries/*.sql`), `maps` (hierarchy, floorplans, GeoJSON/raster layers, FoV cones), `inventory` (tenants, sites, servers, cameras), `provision` (SSH/Compose server & agent provisioning), `platform` (config, logging, Valkey, NATS, S3, telemetry).

## 3. Contract Sources of Truth vs. Generated Files

Never hand-edit generated files. Edit the source of truth and regenerate:

| Domain | Source of Truth (Edit here) | Generated Output (Excluded by `.rgignore`) |
|---|---|---|
| REST API | `packages/api-contract/openapi.yaml` | `internal/api/gen/api.gen.go`, `apps/web/src/api/schema.d.ts` |
| SQL Queries | `internal/store/queries/*.sql`, `migrations/*.sql` | `internal/store/db/*.sql.go` (`sqlc generate`) |
| gRPC / Proto | `proto/openvms/agent/v1/*.proto` | `gen/openvms/agent/v1/*.pb.go` |

## 4. Navigation & Search Rules (Fast Path)

1. **Symbol / Call-graph lookup**: Use `codegraph_explore` MCP or `codegraph query|callers|callees|impact <Symbol>` first.
2. **Module-scoped text search**: Scope `rg` to the target package (e.g., `rg -n <pattern> internal/agent` or `rg -n <pattern> apps/web/src/routes`).
3. **Explicit search in generated/contract files**: `.rgignore` hides `*.gen.go`, `*.sql.go`, `schema.d.ts`, `openapi.yaml`, and `odd/tasks/` from broad searches. Pass the file path explicitly when needed (e.g., `rg -n <OperationId> packages/api-contract/openapi.yaml`).
4. **Subagent Context Pack**: Parent agents own Engram memory (`mem_*`) and pass a complete `Context Pack` (`TASK`, `SCOPE`, `CONTEXT`, `SYMBOLS`, `CONSTRAINTS`, `ACCEPTANCE`, `OUTPUT`) so subagents do not re-explore or call Engram.

## 5. Build, Test & Verification Commands

- **Go package test**: `go test ./internal/<pkg>/...` (or with race detector: `go test -race ./internal/<pkg>/...`)
- **Frontend typecheck & test**: `pnpm --dir apps/web typecheck` / `pnpm --dir apps/web test -- --run <path>`
- **Local Compose deploy**: `make deploy` (`./deploy/deploy.sh`)

## 6. Key Architecture Documentation (`docs/`)

- `docs/ARCHITECTURE_AUDIT.md` — Platform-wide architecture and module boundaries
- `docs/live-view-architecture.md` — Live view streaming, feature flags, and player lifecycle
- `docs/maps/ARCHITECTURE.md` — Maps hierarchy, floorplan editor, and spatial overlays
- `docs/grpc-tls.md` — Agent mTLS enrollment, CA rotation, and gRPC transport
- `docs/notification-channels.md` — Alarm notification channels and WAHA integration
- `docs/telemetria-client.md` — Client station telemetry
