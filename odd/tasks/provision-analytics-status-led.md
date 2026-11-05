# Provision analytics reception LED and detection-source label

Objective: show whether Provision analytics are actually being received (LED on cameras and on NVR servers), label events that Provision detected, and cover the Provision analytics path with integration tests.
Branch: `feat/provision-analytics-status-led` (from `main` at `5b7d17f`). Delivery strategy: `ask-on-risk`.
Runner: `go test ./internal/events/... ./internal/camconfig/... ./internal/api/...`, `go test -tags integration` for the integration tests, `pnpm --dir apps/web test -- --run <path>`, `pnpm --dir apps/web typecheck`.

## Specs
- S0 "corregí esos documentos": the three NVR feature docs and the plan state what is merged. Done in `0137b1b`.
- S1 "quiero que agreguemos un led de estado de recepcion de analiticas tanto para camaras como para nvr": a status LED for analytics reception on a camera and on an NVR server.
- S2 "vamos a integrar y testear camaras con analitica provision": the Provision analytics path (agent receiver, central ingestion, status) is covered by tests. A run against real cameras needs the owner's explicit authorization for the destination and credentials (assumption: not part of this change).
- S3 "en el listado de eventos si la deteccion es provision podriamos ponerele que usamos en la deteccion": an event row says when Provision made the detection.

## Design (assumptions, from the mapping of 2026-11-05)
- A1 Provision events are identified by `events.remote_id` starting with `agent:` (`internal/events/agent_ingest.go:33`). The event `source` is derived at read time and added to the `Event` schema as an optional field.
- A2 No per-camera "last event received" was stored centrally, and a scan of `events` by origin has no index. It is stored per camera at ingestion in `cameras.last_provision_event_at` (migration 00091, nullable, no backfill); see L4.
- A3 LED states: `receiving` (green, an analytic arrived within 24 h), `idle` (amber, nothing in that window), `down` (red, recorders only: the last pull from the agent failed). No state means no LED. The window is a constant (assumption; analytics are sparse by nature).

## Tasks
- T1 (S3) Event `source` ("provision" | "frigate") in the list/detail API and a "Provision" chip in the events list. Route: inline. Status: done (`d534380`: `events.SourceOf`, optional `Event.source`, chip in `Events.tsx`; Go and vitest green; RED observed on the web test, the Go test was written first but the implementation landed in the same step).
- T2 (S1) Per-camera analytics reception status: backend field on the camera and LED in the camera list. Route: inline. Status: done (`670ee3b` backend: migration 00091 `cameras.last_provision_event_at`, set by the agent ingestion with `greatest`, `provision.Reception`, `Camera.analytics_reception`; web `ReceptionLed` in `CameraRow`). The camera detail already shows the keepalive in the integrations tab and was left alone.
- T3 (S1) NVR server analytics reception status: backend field on the server and LED in the Servers list for `provision_nvr` servers only; `down` when the last agent pull failed (`server_agent_event_cursors.last_error`). Route: inline. Status: done (`GetServer`/`ListServers` carry `last_provision_event_at` and `provision_ingest_error`; Servers row shows the LED).
- T4 (S2) Integration tests of the Provision analytics path (ingest, status, source). Route: inline. Status: partial: `TestAgentIngestRecordsWhenTheCameraLastReportedAndNeverMovesItBack` passes against real Postgres (migration applied, reception time recorded, never moved back, server row). A run against real Provision cameras is not done: it needs the owner's authorization for the destination and credentials.
- T5 Docs and PR. Route: inline. Status: pending (PR not opened; push and PR need the owner's word).

## Log
- L1 (2026-11-05, user): "dale, corregí esos documentos en las analiticas provision quiero que agreguemos un led de estado de recepcion de analiticas tanto para camaras como para nvr y vamos a integrar y testear camaras con analitica provision, en el listado de eventos si la deteccion es provision podriamos ponerele que usamos en la deteccion"
- L2 (2026-11-05, mapping): see A1-A2 and the evidence: no per-camera last-event store; `lprProvisionIntegration`/`analyticsProvisionIntegration` ignore `ProvisionLink.LastEventAt`; `server_agent_event_cursors.last_pull_at` is set on empty pulls and errors, so it is not an event time; `Events.tsx:~250-275` is the row markup.
- L3 (2026-11-05): S0 done in `0137b1b`; assess says `passive`, no review.
- L4 (2026-11-05): decision. The last analytic is stored per camera at ingestion (nullable column, no backfill) instead of computed from `events`: that table has no index by origin and a scan per camera would be slow. Until the first analytic after the deploy a Provision camera shows `idle`. A camera with no Provision evidence and no analytic shows no LED. The camera LED has no `down` state: that needs a live agent call per row.
- L5 (2026-11-05): native review could not start for the high-risk slice (`lens_context_budget_exceeded`: `api.gen.go` regeneration adds 2114 realigned lines to any candidate that holds the backend commit); no authority was created. Independent verifier (read-only) on `670ee3b`: no blockers; one advisory, a camera whose clock is ahead would pin `last_provision_event_at` in the future and keep the LED on `receiving`. Fixed: the stored time is capped at `now()` (`least($2, now())`); the integration test failed before the fix (reception time 2026-12-05) and passes after.
