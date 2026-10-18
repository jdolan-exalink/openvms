# PRD Implementation Audit

Audit of `docs/PRD.md` (137 sections, Spanish) against the implementation. This is a read-only baseline: it records what the code proves today and proposes an order to close the rest. It does not authorize implementing anything.

## 1. Summary

- **Date:** 2026-10-17
- **Base commit:** `a578c6b` (`main`, PR #73)
- **Method:** the 137 PRD sections were grouped into 40 capability rows. A row is **Done** only with code evidence (file and line, migration, OpenAPI path, route, test). Tests were not run for this audit; "test exists" means the file was found, not that it passes.
- **Statuses:** Done = behavior present and evidenced. Partial = core present, named parts missing. Missing = no implementation found. Superseded = delivered through a different, intentional design. Unverified = evidence incomplete.

| Status | Rows |
|---|---:|
| Done | 19 |
| Partial | 12 |
| Missing | 7 |
| Superseded | 1 |
| Unverified | 1 |
| **Total** | **40** |

**Top gaps (detail in section 5):**

1. Cases and case evidence packages (PRD §55-57, Phase 8) do not exist; only the `cases.*` permission names and a nav placeholder do.
2. No enterprise identity: OIDC, LDAP and AD (§35, Phase 9) are absent.
3. No Edge Agent event pipeline: no SQLite spool, no local MQTT subscription, no offline queue (§14-16).
4. No central backups and no event retention or partitioning (§49-50, §95).
5. `live.ptz`, `live.audio`, `live.talk` and `cases.*` are declared in the catalog but never enforced; the ONVIF PTZ endpoint has no Live UI.
6. Signed media sessions and WebRTC are not on the browser path (§40-42).
7. Face recognition is not built (planned next).
8. Video wall has no rotation or scheduled views (§84).
9. E2E coverage covers none of the PRD §107 scenarios, and the §108 negative matrix is not systematic.
10. No HA or Kubernetes deployment (§93-94).

## 2. Capability table

| # | Capability | PRD § | Status | Evidence | Gap |
|---:|---|---|---|---|---|
| 1 | Multi-tenancy, global resource identity | 5, 6, 69, 134 | Done | Tenants, sites, servers, cameras: `migrations/00002_inventory_and_authz.sql`; RLS `FORCE` at `:201-206`; `/api/v1/tenants` (`openapi.yaml:290`); cross-tenant tests `internal/events/cross_tenant_search_integration_test.go:74`, other-tenant 404 in `internal/api/api_integration_test.go` | None found |
| 2 | Inventory: sites, Frigate servers, cameras, groups, folders; register and discover | 63, 64, 117 | Done | `/servers` (`:477`), `/servers/probe` (`:520`), `/servers/{id}/sync` (`:631`), `/camera-groups` (`:1627`), `/camera-folders` (`:1500`); `internal/inventory/servers.go`, `cameras.go`; dynamic groups `00052`; SSH provisioning `odd/tasks/servers-ssh-provisioning.md` | None found |
| 3 | Frigate adapter, versions, capabilities, config management, remote restart | 19, 65, 100-103, 125 | Done | `internal/frigate/adapter.go:237` (min 0.17), `v017.go`, `v018.go`; `/features` (`:199`); config `raw`/`revisions`/`rollback` (`:1408`, `:1452`, `:1476`), `00021_frigate_config_revisions.sql`; `POST /servers/{id}/restart` (`:648`, `servers.restart`); `odd/tasks/frigate-config-editor.md`, `frigate-drop-016.md` | No compatibility admin screen beyond `/features` and server health (§100) |
| 4 | Event ingestion, reconciliation, model, thumbnails, previews, detail | 17, 18, 20-23, 51, 118 | Done | `internal/events/syncer.go:127,175,241`; idempotent `events_server_remote_key` (`00003_events.sql:40`); cursors `event_sync_state` (`:74`); `stale_reviews.go`; thumbnails `syncer.go:739`, `00007`, `00008`; `/events/{id}`, `/thumbnail`, `/reprocess`, `/events/sync-status` (`:1843-1900`, `:1790`) | Transport differs from the PRD (row 5) |
| 5 | Event transport MQTT -> Agent -> NATS -> worker | 12, 17 | Superseded | Central worker pulls Frigate over HTTP every 5 s (`decisions.md:22`, `syncer.go`). NATS JetStream is used for alarms and the UI feed (`internal/platform/natsx/nats.go:53`, `apps/api/main.go:232`), not for ingestion | MQTT is "future optimization for the Edge Agent" (see row 27) |
| 6 | Event partitioning and central retention | 47-50 | Missing | No `PARTITION` in `migrations/`; no events purge job (only notification retention, `internal/platform/config/config.go:55-58`). Hourly rollups exist (`00040_event_rollups.sql`) | Monthly partitions, retention policy, thumbnail/preview lifecycle |
| 7 | Global search, filters, LPR search | 43-46, 82, 119 | Done | `/search` (`:2878`, `internal/search/service.go:76`), `/events` filters (`:1726`), `/lpr/reads` (`:1917`); indexes `00006`, `00015`, `00030`; Omnibox `apps/web/src/api/queries.ts:392`; permission scoping `internal/api/search_integration_test.go:92`; `odd/tasks/m3-search.md` | No semantic search (row 40); no watchlist/hotlist LPR (row 40) |
| 8 | Users, groups, sessions, MFA, account security | 24, 26, 34, 36, 106 | Done | Argon2id `internal/identity/password.go:17`; TOTP `totp.go:26`; lockout `service.go:32,152`; absolute and idle TTL `service.go:55-58`; `/auth/login` (`:1999`), `/me/mfa/*` (`:2061-2098`), `/users` (`:2116`), `/groups` (`:2201`); `00004_sessions.sql` | No self-service session list or revoke API |
| 9 | RBAC with scopes, DENY wins, delegated admin | 25, 27-31, 120 | Done | `internal/authz/catalog.go:89-137`; `permission_grants` in `00002`; `/grants` (`:234`); no self-grant `api_internal/api_integration_test.go:127`; roles as templates, authorization from grants (`decisions.md:16`) | Enforcement gaps: `live.ptz/audio/talk` and `cases.*` are never checked (`rg` finds only the catalog); no named admin-type templates (§25) |
| 10 | Enterprise SSO: OIDC, LDAP, AD | 35, 125 | Missing | No OIDC/LDAP code or dependency (`go.mod` has only an indirect AWS `ssooidc`); login is local only (`internal/api/auth.go`) | Everything |
| 11 | Audit trail and secrets | 66-68, 73, 103 | Done | Append-only `audit_log` + trigger (`00002:165-189`), `REVOKE UPDATE, DELETE` (`:236`); `/audit` (`:2286`); denied-access audit `internal/api/router.go:96-103`, `access_denied_audit_integration_test.go:26`; AES-256-GCM secrets `internal/secrets/secrets.go`; `credential_ingress_test.go` | No audit export or tamper-evidence chain; PTZ operations not audited (row 22) |
| 12 | Live view, layouts, saved and shared views, camera tree | 37, 38, 81, 121 | Done | Gateway `internal/media/gateway.go:95` (MSE live), `live_session.go`; `/views` (`:2313`), `internal/media/views.go`; `Live.tsx`, `lib/liveGrid.ts`, `components/Presentations.tsx`; `odd/tasks/live-view-platform.md`, `live-phone-pwa.md` | None for the base scope |
| 13 | Live optimization: stream limits, suspension, single socket | 39, 86 | Partial | Persistent `lib/live/PlayerSession.ts`, `surfaceLayer.ts`; adaptive degradation `lib/telemetry` (`odd/tasks/client-station-telemetry.md`, CST-09); gateway read limits `live_session.go:42` | One socket per mounted tile; no server-enforced per-user/per-layout stream cap found; list virtualization not found |
| 14 | Media sessions and data plane (signed session, direct edge, relay) | 40, 41 | Partial | `internal/mediasession/session.go` manager created at `apps/api/main.go:233`; used by gRPC `ConnectionService` (`internal/control/connection.go:30`) and capacity counts (`capacity_handlers.go:228`); `apps/relay` | No `POST /api/media/session`; browser media is cookie-authenticated through the gateway with a 15 s authz cache (`internal/media/access.go:35`); no direct-to-edge path |
| 15 | WebRTC -> MSE -> HLS preference | 42 | Partial | MSE live, HLS VOD (`gateway.go:95-96`, `components/HlsPlayer.tsx`); `internal/videobackend/frigate.go:89,246` exposes a WebRTC URL and capability | No WebRTC client in `apps/web`; no protocol negotiation |
| 16 | Playback and timeline | 52, 122 | Done | `/cameras/{id}/recordings` (`:2393`), VOD proxy `gateway.go:96`; `routes/Playback.tsx`, `lib/playbackSync.ts`; `recordings.view`/`recordings.seek` in catalog | None found |
| 17 | Exports and snapshots, download permissions | 53, 122 | Done | `/exports` (`:2423`), `/export-jobs` (`:2488`); `internal/media/export_job_manager.go`; `00036_export_jobs.sql`, `00037_export_shares.sql`; public shares (`/export-jobs/{id}/shares` in `internal/api/router.go`), `gateway.go:104-107`; `odd/tasks/video-export-job-system.md` | Staging is on orchestrator disk (see section 4) |
| 18 | Evidence integrity and central evidence storage | 54, 57, 70 | Partial | Per-item SHA-256 and manifest `export_job_manager.go:29,119`; `export_jobs.manifest` (`export_jobs.go:60`) | No evidence store independent of exports, no verify endpoint, no object-storage layout for evidence (§70), no immutability |
| 19 | Cases and evidence-in-cases | 55, 56, 124 | Missing | No migration, no OpenAPI path; nav placeholder `components/nav.ts:42` (`milestone: "M8"`); permissions only declared `catalog.go:110-113` | Everything, including case package export |
| 20 | Notification engine | 62, 125 | Done | `internal/notify` (webhook, Telegram, WhatsApp via WAHA, email: `senders.go:115-133`); outbox `00016`, `00018`, `00019`; `/notification-channels` (`:3061`), `/notifications` (`:3267`); `Channels.tsx`, `Notifications.tsx`; `docs/notification-channels.md` | No mobile/web push |
| 21 | Alarms, rules engine, infrastructure alerts | 61 (beyond) | Done | `/alarms` (`:2605`), `/rules` (`:2906`); `00014`, `00023`, `00047`, `00050`; `internal/rules/offline.go`, `00017` camera outages; `odd/tasks/alarms-phase*.md` | Not PRD-driven; extends the PRD |
| 22 | PTZ | 85, 125 | Partial | `POST /cameras/{id}/onvif/ptz` (`:1230`), `internal/api/onvif_handlers.go:624`, `internal/onvif/ptz.go`; UI only in `routes/FrigateCameraConfig.tsx:1307` | No Live PTZ controls or presets; `live.ptz` not checked (`catalog.go:92` only); no audit entry |
| 23 | Video wall | 84 | Partial | Fullscreen Live (`Presentations.tsx` `FullscreenButton`); Vision AI dynamic matrix, the "dynamic wall" future item (`odd/tasks/vision-ai-dynamic-matrix.md`); wall benchmark `client-station-telemetry.md` | No dedicated wall mode, auto-rotation, scheduled views, or 64-stream profile; no `rotation` code found |
| 24 | Dashboard, server and camera health | 58-60 | Done | `routes/Dashboard.tsx`; `internal/inventory/health.go:53,180`; `/system/capacity` (`:79`), `/system/info` (`:66`); `health.view` | None found |
| 25 | Edge Agent enrollment, mTLS, heartbeat, health | 71, 72, 123 | Done | `internal/agentca`, `agentenroll`, `agentauth`; `/agent/enroll` (`:793`), `enroll-token` (`:759`), `revoke-certificates` (`:776`); `00041-00044`; gRPC control `internal/control/agent_server.go`; `odd/tasks/agent-mtls.md` | None found |
| 26 | Edge event pipeline: MQTT local, SQLite spool, offline queue, agent backfill | 14-16, 123 | Missing | `apps/edge-agent` and `internal/agent` only sample host metrics and serve ops endpoints; no SQLite dependency or queue; MQTT used only by the mock (`internal/frigatemock/mqtt.go`) | Everything (central pull covers the online case only) |
| 27 | Edge remote actions and auto update | 15, 123 | Partial | SSH install/update/uninstall (`openapi.yaml:717-886`), ONVIF ops (`:904-1000`), LPR provisioning (`provision-lpr` routes in `internal/api/router.go`), update endpoint `apps/edge-agent/main.go:329`; `odd/tasks/agent-lifecycle-versioning.md` | Updates are operator-triggered; automatic or scheduled update not found (Unverified beyond that) |
| 28 | Backups (central and config) | 95, 125 | Missing | Only Frigate config revisions (`00021`); no backup job, no `pg_dump` flow, no restore doc | Postgres, object storage, secrets/config backup and restore |
| 29 | Version monitoring | 100, 125 | Partial | Frigate version captured per server (`inventory/health.go:180`); agent binary identity and outdated state (`odd/tasks/agent-lifecycle-versioning.md`) | No fleet report, no alert on unsupported/outdated Frigate or agent |
| 30 | Observability: metrics, tracing, logs, correlation, health | 87-91 | Done | `/metrics` `router.go:77`; `otelhttp` `router.go:111`; `X-Request-ID` `internal/platform/httpx/middleware.go:21`; JSON logging `internal/platform/logging`; `/health/live`, `/health/ready` (`:32`, `:46`); `deploy/prometheus` | VMS-level metrics are mostly media/live (`live_session.go:158-170`); no dashboards or alert rules |
| 31 | UI `/ws` push feed | 76, 86 | Partial | `GET /ws` (`router.go:83`), `internal/realtime` hub, authz, resume; client `apps/web/src/lib/realtime.ts:90`; types `routes.go:17-26` | Missing `event.updated`, `export.progress`, `permission.changed`; `docs/UI_REFACTOR_AUDIT.md:51-53` is stale on this point |
| 32 | API conventions: OpenAPI-first, pagination, timestamps, soft delete | 74, 75, 77-79 | Done | `packages/api-contract/openapi.yaml`, generated code checked in CI; keyset cursors (`events/service.go:175`); `deleted_at` on core tables (`00002:11,27`) | None found |
| 33 | Rate limiting and HTTP security | 104, 105 | Partial | Token bucket `internal/platform/httpx/ratelimit.go:33` wired only to agent enroll (`router.go:54,203`); nosniff/frame/referrer `middleware.go:111-113`; CSP in `deploy/docker/Caddyfile:26`; login lockout (row 8) | No per-IP/user rate limit on login or API; CSP only at the Caddy edge |
| 34 | Deployment, HA, Kubernetes | 92-94, 133 | Partial | `docker-compose.yml`, `deploy/docker/*`, `Caddyfile`, SeaweedFS and Valkey services | No HA topology, no Helm/Kubernetes manifests; Valkey is only a readiness probe (`apps/api/main.go:117,207`), the PRD §32 permission cache is in-process |
| 35 | Degraded-state UX (Central, Site down, sync state) | 97-99 | Unverified | APIs exist: `/events/sync-status` (`:1790`), camera outages (`00017`), server status in the `/ws` feed | UI banners and "last synchronized" surfacing not verified in `apps/web` |
| 36 | CI/CD, tests, versioning, migrations, monorepo, dev env, mock, seed | 107 (unit/integration), 109-115 | Done | `.github/workflows/ci.yml` (vet, lint, race tests, govulncheck, integration containers, web build); goose migrations `migrations/embed.go`; `internal/frigatemock`, `apps/frigate-mock`; `vmsctl seed-demo` (`apps/vmsctl/main.go:169`); ~920 Go test functions plus Vitest | None found |
| 37 | E2E and negative security tests | 107, 108, 129 | Partial | Acceptance scenario over HTTP (`internal/api/api_integration_test.go:79-91`), search permission test, cross-tenant search, denied-access audit; Playwright (`packages/test`) | Playwright has only maps specs, none of login/create user/assign group/live/denied/search/LPR/export/case; no negative matrix for live, playback, export and `/ws` |
| 38 | Vehicle and person attributes, Vision AI live engine | 126 | Done | `internal/vehicle` (`00024`, `00025`, `00028`, `00029`), person `00027`; `internal/visionai` slot engine; `/vision-ai/slots` (`:1766`); `routes/VisionAI.tsx`; `odd/tasks/vision-ai-*.md` | See section 3 for what Vision AI does not cover |
| 39 | Face recognition and federation | 126 | Missing | Only Frigate `sub_label` passthrough (`internal/events/service.go:396`); no face data model, UI, or permission | Planned next: Frigate engine + enterprise layer + dedicated page (not designed here) |
| 40 | Other Phase 10 AI: semantic search, cross-camera tracking, speed events, AI summaries, LPR watchlists | 83, 126 | Missing | No embeddings, speed data or summarization code; watchlist explicitly absent (`internal/visionai/source.go:105`); object tracks are per event (`00039`) | Needs product decisions per item (section 3) |

## 3. Phase view (PRD §117-§126)

| Phase | Status | Notes |
|---|---|---|
| 0 Foundation (§116) | Done | Monorepo, Compose, Postgres, Valkey, NATS, SeaweedFS, OTel, CI (row 36). Object store is SeaweedFS, not MinIO. |
| 1 Multi-Site Core (§117) | Done | Rows 1, 2, 3. |
| 2 Federated Events (§118) | Done | Row 4; global Events UI `routes/Events.tsx`. Transport is HTTP pull (row 5). |
| 3 Global Search + LPR (§119) | Done | Row 7; `Plates.tsx`. |
| 4 Identity + RBAC (§120) | Done, with enforcement gaps | Rows 8, 9, 11. Delegated administration is achieved through scoped grants. `live.ptz` and `cases.*` are not enforced. |
| 5 Federated Live (§121) | Partial | Rows 12, 13, 14. Views and shared views are done; media tokens and sessions are not on the browser path. |
| 6 Playback + Export (§122) | Done | Rows 16, 17. |
| 7 Edge Agent (§123) | Partial | Enrollment, mTLS, health, update, remote ops done (rows 25, 27). SQLite queue, local MQTT, edge backfill and edge media missing (rows 14, 26). |
| 8 Cases (§124) | Missing | Row 19. Hashes and export manifests exist (row 18) but no case model. |
| 9 Enterprise Operations (§125) | Partial | Done: notifications, server configuration, remote restart. Partial: advanced audit, PTZ, version monitoring. Missing: OIDC/LDAP/AD, backups. |
| 10 Advanced AI (§126) | Partial | Merged into Vision AI; see below. |

### Phase 10 mapped to Vision AI

Decision applied: Phase 10 is covered by and merged into the existing **Vision AI** feature (`/vision-ai`, `internal/visionai`). Vision AI today is an event-driven slot allocator: it ranks recent events (detections, alarms, LPR) by score, fills a camera grid, and rotates it (`internal/visionai/scoring.go`, `allocator.go`, `source.go`; `routes/VisionAI.tsx`).

| Phase 10 item | Vision AI coverage | Remaining |
|---|---|---|
| Semantic global search | Not covered | Embedding index and query path (decision needed on model and hosting) |
| Face federation | Not covered | Planned next: Frigate engine + enterprise layer + dedicated page |
| Vehicle attributes | Covered: color, type, body class, person attributes feed event `Attributes` that scoring uses (`internal/vehicle`, `00024-00029`) | Attribute search facets in Events if wanted |
| Cross-camera tracking | Adjacent only: ranks cameras by activity; per-event object tracks (`00039`) | Identity continuity across cameras (needs face/plate/vehicle re-identification) |
| Advanced LPR | Partial: plates in slot scoring; reads, crops, clips, watermark exist | Watchlist/hotlist data source (`source.go:105`), alerts on match |
| Speed events | Not covered | Source data (Frigate speed estimation) and a model; not started |
| AI summaries | Not covered | Summarization over events/cases; depends on Cases |

## 4. Notable divergences

| Topic | PRD | Implementation | Source |
|---|---|---|---|
| Permission engine timing | Phase 4 | Built in M1 so no endpoint gets authorization retrofitted | `docs/architecture/decisions.md:34` |
| Event transport | MQTT via Edge Agent, NATS, worker | HTTP pull from the worker with per-server cursors; MQTT left as a future Agent optimization | `decisions.md:22` |
| Central is not an NVR | No video copy to central | Media gateway proxies; but multi-camera export jobs stage files on orchestrator local disk (`export_job_manager.go:30`), which `decisions.md:28` (no copy) does not describe | `export_job_manager.go` |
| Frigate versions | Adapter layer, 0.18 as example | Minimum 0.17, adapters 0.17 and 0.18; 0.16 rejected and its code removed | `adapter.go:237`, PR #71 |
| Object storage | MinIO | SeaweedFS in dev, any S3 in production | `decisions.md:7` |
| Agent transport | ConnectRPC (decision record) | Plain gRPC with TLS and mTLS (`internal/control/server.go:96`) | `decisions.md:12` |
| Live protocol order | WebRTC first | MSE over WebSocket first, WebRTC deferred | `decisions.md:13` |
| Delegated admin roles | Named admin types as templates | Scoped grants only; no template catalog | `catalog.go`, `decisions.md:16` |
| Stale internal doc | n/a | `docs/UI_REFACTOR_AUDIT.md:51-53` states no `/ws` feed, no rules, no alarms inbox; all exist now (`router.go:83`, `/rules`, `/alarms`) | row 21, row 31 |

**Built beyond the PRD** (not counted above): Maps (`00022`, `/maps/*`), alarm lifecycle and attention center, rules engine with versions and simulation, ONVIF camera management, SSH agent provisioning, Frigate config editor, forensic enhancement and image enhancement, client station telemetry, phone PWA Live, tenant branding and clip watermarking.

## 5. Proposed closing plan

Order is by value and dependency. Size: S under 1 week, M 1-3 weeks, L over 3 weeks, for one engineer.

| Order | Epic | Scope | Depends on | Size | Risks |
|---:|---|---|---|---|---|
| 1 | **Face recognition** (already prioritized by the user) | Frigate face engine + enterprise layer, dedicated page, permission, `/ws` and event surfacing. Design pending; not designed here. | Epic 2 (permission enforcement pattern), Frigate 0.17+ face feature | L | Biometric privacy and retention policy; per-tenant legal basis; Frigate face API shape changes between 0.17 and 0.18 |
| 2 | **Authorization enforcement hardening** | Enforce `live.ptz`, `live.audio`, `live.talk`; audit PTZ operations; add the negative test matrix (§108) for live, playback, export, `/ws`, ONVIF | None | S | Behavior change for operators who relied on `cameras.view`; needs a grant migration |
| 3 | **M8 Cases and evidence** | Case model (migration, OpenAPI, RLS), notes, attach events/exports/clips, case package with hashes, `cases.*` enforcement, UI route replacing the `nav.ts:42` placeholder; promote export evidence into a first-class store | Epic 2 | L | Evidence immutability and retention rules; where evidence bytes live (orchestrator disk vs object storage); legal chain-of-custody expectations |
| 4 | **Test coverage** | Playwright flows for PRD §107 (login, create user, assign group, live, denied camera, search, LPR, export, case) and CI job; extend after Epic 3 for the case flow | Epic 2 (matrix), Epic 3 (case) | M | Flaky video and WebSocket flows; needs the Frigate mock in CI |
| 5 | **Enterprise operations** | Backups (Postgres, object storage, secrets/config) with restore runbook and a test; version monitoring (fleet report, alerts on unsupported Frigate or outdated agent); event retention and monthly partitioning (§49-50) | None | M | Partitioning an existing large `events` table needs a migration plan; restore must be proven, not assumed |
| 6 | **M9 Enterprise identity** | OIDC first, then LDAP and AD; group/claim to role mapping; keep local login and MFA as break-glass | Epic 2 | L | IdP test environments; session and MFA interplay; mapping onto scoped grants rather than role templates |
| 7 | **Platform gaps** | `/ws` completion (`event.updated`, `export.progress`, `permission.changed`); `POST /api/media/session` with short-lived signed tokens; WebRTC client with MSE/HLS fallback; stream caps and suspension; Live PTZ controls; video wall mode (rotation, scheduled views) | Epic 2 (PTZ); media sessions precede WebRTC | L | Biggest regression surface in Live; needs performance validation with the client-station benchmark; direct edge media needs the Agent to proxy video |
| 8 | **Edge Agent event pipeline** | SQLite spool, local MQTT subscription, offline queue, agent-side backfill, automatic update | Epic 7 (media sessions share the Agent channel) | L | Duplicates versus central pull (idempotency already exists at `00003:40`); ack and ordering protocol; only worth it for sites with unreliable links |
| 9 | **Rate limiting, security headers, HA** | Login and API rate limits, app-level CSP, Valkey-backed permission cache, Kubernetes/Helm and HA documentation, degraded-state UX verification (row 35) | Epic 5 | M | HA changes ownership of workers and the NATS stream; keep Compose as the supported default |
| 10 | **Remaining Phase 10 AI** | LPR watchlists, cross-camera tracking, semantic search, speed events, AI summaries, as Vision AI extensions | Epic 1 (identity signals), Epic 3 (summaries) | L each | Model hosting cost and licensing; each item needs a product decision first, so do not schedule without one |

Notes:

- Epic 1 is first because the user already prioritized it. Epic 2 is listed second because it is small and unblocks Epic 1's permission model; they can run in parallel.
- Epics 5 and 6 can swap based on customer demand: enterprise identity is a sales gate, backups are an operational risk.
- Rows to confirm before planning: row 27 (automatic update), row 35 (degraded-state UI).
