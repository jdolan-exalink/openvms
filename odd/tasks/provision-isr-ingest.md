# Provision-ISR stage 2: durable queue and agent-to-central ingestion (ODD)
Objective: plate readings the camera reports to the edge agent survive restarts and outages and reach central `lpr_reads` exactly once, by central pulling them from the agent. Branch: `feat/provision-isr-ingest` (from main `5d99095`). Delivery: ask-on-risk, slices of about 400 lines. Runner: `go test ./internal/agent/... ./internal/events/... ./internal/provision/...`, `go test -tags integration ./internal/events/...`, `go build` with and without cgo. Previous stages: `odd/tasks/provision-isr-plugin.md`.

## Specs
- S1: "Implementar una cola persistente con reintentos e idempotencia." (prompt, stage 2)
- S2: "No crear una integración paralela si podemos extender la existente." Extend `internal/agent/provisionlpr` and the agent bearer channel; the central side reuses the agent client of `internal/provision` and the `lpr_reads` table.
- S3: "vamos con pull entonces" (user, L2): central asks the agent for new events after a sequence number and confirms what it stored; no new inbound surface on central, no proto change.
- S4: "No inventes protocolos, endpoints ni capacidades del fabricante." The queue stores the agent's own normalized event (`LPREvent`); it adds no assumption about the camera's payload. The LPR payload parser is still unverified until E0.4.
- S5: "Una cámara no autorizada no puede inyectar eventos." Only events from registered cameras are queued for central; an event whose camera ID is not a central camera UUID is not ingested.
- Assumptions (not asked): the queue is bounded and drops the oldest unacknowledged events with a counted, visible loss; central ingests only plate, time, confidence and camera for now; alarms, rules and realtime announcements for agent-sourced reads are stage 5.

## Tasks
- [x] E2.1 — S1,S4: agent durable queue (`provision-events.jsonl` in the state dir, 0600): append with a monotonic sequence and a stable event ID before the in-memory fan-out, survive restart, bounded by count and bytes with a loss counter, compaction after acknowledgement. inline. Only readings from registered cameras are queued; images are not queued. RED observed (undefined symbols).
- [x] E2.2 — S1,S3: agent routes behind the bearer: `GET /v1/provision-lpr/events?after=N&limit=M` and `POST /v1/provision-lpr/events/ack`; queue depth, oldest sequence and lost count in `/status`. inline. RED observed: the ack POST was swallowed as an event and the routes were open without a token until wired behind the bearer.
- [ ] E2.3 — S1,S2,S3,S5: central: migration for the per-server cursor, actor-free agent client, ingestor that pulls, inserts into `lpr_reads` idempotently on `(server_id, remote_event_id)`, then acks; worker job; skips and counts events for cameras it does not know. inline
- [ ] E2.4 — S1: docs (`docs/provision-isr/ingestion.md`, matrix and setup updates), central diagnostics (last pull, backlog, losses) in the camera configuration screen. inline

## Log
- L1 (2026-10-24): "dale, arrancá la etapa 2"
- L2 (2026-10-24): design question answered. Pull vs push: "vamos con pull entonces".
- L3 (2026-10-24): central side mapped (read-only explorer). `lpr_reads` (migration 00003) has unique `(server_id, remote_event_id)`, no source or snapshot column (only `crop_key`, migration 00046); latest migration is 00072, so the next is 00073; insert path `upsertPlate` in `internal/events/syncer.go:791`, raw SQL under `store.AllTenants`; plates insert publishes nothing, alarms and realtime follow `events` review upserts through unexported helpers; periodic jobs follow `events.Syncer.Run/SyncAll` started from `apps/worker/main.go`, no leader election, replicas rely on idempotent upserts and `SKIP LOCKED`; agents live in `server_agents` (host, port, `token_sealed`) and the existing helpers need an `authz.Actor`, so a background variant is needed; integration tests use `internal/testutil/pgtest`.
