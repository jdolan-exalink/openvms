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
- A1 Provision events are identified by `events.remote_id` starting with `agent:` (`internal/events/agent_ingest.go:33`). No migration: the event `source` is derived at read time and added to the `Event` schema as an optional field.
- A2 No per-camera "last event received" is stored centrally. It is computed at read time from `events` (index `events_camera_time_idx`), not stored.
- A3 LED states: `receiving` (green, an analytic arrived within the freshness window), `idle` (amber, configured and reachable but nothing recent), `down` (red, the agent or the last ingestion cycle reports an error), `off` (grey, no Provision analytics configured). The window is a constant, default 24 h (assumption; analytics are sparse by nature).

## Tasks
- T1 (S3) Event `source` ("provision" | "frigate") in the list/detail API and a "Provision" chip in the events list. Route: inline. Status: pending.
- T2 (S1) Per-camera analytics reception status: backend field on the camera and LED in the camera detail and list. Route: inline. Status: pending.
- T3 (S1) NVR server analytics reception status: backend field on the server and LED in the Servers list. Route: inline. Status: pending.
- T4 (S2) Integration tests of the Provision analytics path (ingest, status, source). Route: inline. Status: pending.
- T5 Docs and PR. Route: inline. Status: pending.

## Log
- L1 (2026-11-05, user): "dale, corregí esos documentos en las analiticas provision quiero que agreguemos un led de estado de recepcion de analiticas tanto para camaras como para nvr y vamos a integrar y testear camaras con analitica provision, en el listado de eventos si la deteccion es provision podriamos ponerele que usamos en la deteccion"
- L2 (2026-11-05, mapping): see A1-A2 and the evidence: no per-camera last-event store; `lprProvisionIntegration`/`analyticsProvisionIntegration` ignore `ProvisionLink.LastEventAt`; `server_agent_event_cursors.last_pull_at` is set on empty pulls and errors, so it is not an event time; `Events.tsx:~250-275` is the row markup.
- L3 (2026-11-05): S0 done in `0137b1b`; assess says `passive`, no review.
