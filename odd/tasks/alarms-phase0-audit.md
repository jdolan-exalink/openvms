# Alarms, Zones and Attention Center — Phase 0 audit

## Objective
Audit the real state of `main` (`d72521c`) against the adjusted development prompt (v2) before any product
code: reuse matrix, current flow, proposed migrations/API, and a revised phase plan. **Checkpoint: no product
code until this document is approved.**

Evidence tags: **[code]** read in source, **[test]** a test exists (not run in this audit), **[db]** observed in
the running stack, **[unverified]** inferred. File references are relative to the repo root.

## 1. Key findings (what changes the plan)

1. **The alarm flood does not come from rules — it comes from the event syncer.** `internal/events/syncer.go`
   (`openAlarm`, ~:370-390) inserts a `source='event'` alarm for **every** Frigate event with severity
   `alert`, gated only by `alarm_settings.enabled_at`. Rules add a second `source='rule'` alarm for the same
   event (`UNIQUE(event_id, source)`), so one event can produce 2 alarms. Nothing ever auto-closes them.
   [code] Result in this stack: 145,338 `open` alarms. [db]
2. **Alarm statuses are consistent.** Migration 00023 allows `open, acknowledged, assigned, investigating,
   resolved, closed`; only its Down migration collapses them. The `investigating` rows are legitimate
   (`alarms/service.go:399`). The prompt's "inconsistency" fact was wrong. [code]
3. **A camera zone editor already exists in the web** (`apps/web/src/components/zones/*`,
   `lib/zoneGeometry.ts`, embedded in `routes/FrigateCameraConfig.tsx`): polygons on the real snapshot,
   normalized 0-1, zones + motion/object masks. It persists nothing in OpenVMS and saves by replacing the
   whole Frigate `zones` section via `PUT /api/config/set` (live-applied). [code]
4. **Renaming a camera in Frigate creates a new OpenVMS camera** (upsert on `(server_id, remote_name)`); the
   old row is marked missing and keeps history, groups, rules. Same for moving to another server. No
   rebind flow exists. [code, by inference]
5. **`map_zone_ids` rule conditions never match in production**: `rules.ZoneResolver` has no implementation
   wired in `apps/worker`. [code]
6. **Single-alarm authorization ignores server and camera-group grants** (`alarms/service.go:231`, `:258`
   build `access.Camera(..., uuid.Nil, ..., nil)`), while the list path honors them → list and detail can
   disagree. Pre-existing bug. [code]
7. **Rules have no permission of their own**: CRUD requires `notifications.manage` at tenant scope; rule
   conditions are not validated (e.g. `server_ids` on event rules is silently ignored, camera IDs the author
   cannot see are accepted). [code]
8. **Lost evaluations**: rules run in an in-memory loop after the event commit; a worker crash in between
   skips evaluation forever (the event is no longer "new"). NATS publishes in the rules path happen inside
   the DB transaction (a rollback can still publish). [code]

## 2. Current flow

```text
Frigate (HTTP, polled every EVENT_SYNC_INTERVAL=5s, 8 servers in parallel)
  └─ events.Syncer.upsertReview  (dedup: events(server_id, remote_id), isNew via xmax=0)
       ├─ openAlarm: severity=alert → alarms(source='event')        ← flood source, no auto-close
       └─ after commit, isNew only → rules.EvaluateEvent (in-memory loop)
            ├─ claim rule_firings(rule_id, camera) 5 min debounce (global, not per rule)
            ├─ create_alarm → alarms(source='rule')                  ← 2nd alarm for same event
            ├─ notify_in_app → notifications + NATS notification.created (published inside tx)
            └─ channel_ids → notify outbox → notify.Worker (SKIP LOCKED, backoff)
vehicle worker → re-calls EvaluateEvent after classification (can re-notify after debounce expiry)
rules.OfflineDetector (HealthInterval) → camera/server offline rules, once per outage
alarms.Service (API only): ack/assign/investigate/resolve/close/comment/bulk + alarm_transitions + audit
```

## 3. Reuse matrix

| Requirement | Existing code | Gap | Files to change | Migration | Tests | Risk |
|---|---|---|---|---|---|---|
| Incident dedup / correlation | `alarms` table, `UNIQUE(event_id, source)`, transitions [code] | No grouping; 1 alarm per event per source; 2 sources | `events/syncer.go`, `rules/service.go`, `alarms/service.go` | `alarms.correlation_key`, `last_event_at`, `event_count`; `alarm_events` link | integration: N events → 1 alarm | **High** (core path) |
| Auto-close | — | Absent | new `alarms` sweeper in worker | `alarm_settings.auto_close_after` | integration with fake clock | Medium |
| Backlog cleanup | — | 145k open | one-off migration/command | data migration (close as `auto_closed`, keep rows) | count before/after | Medium |
| Rule-level alarm gate | `create_alarm` on any severity [code] | Event-source alarm bypasses rules | `events/syncer.go` | `alarm_settings.event_alarm_mode` | integration | Medium |
| Camera groups in rules | `camera_groups`, `camera_group_members`, events filter [code][test] | Not in `rules.Conditions` | `rules/rule.go`, `rules/service.go` | none (jsonb conditions) | rule_test + integration | Low |
| Dynamic groups | — (manual only) [code] | No filters | `inventory/cameras.go`, queries | `camera_groups.filter jsonb` + materialize into `camera_group_members` | integration | Medium (all authz joins use members) |
| Group coverage display | visible-members only [code] | No total/hidden counts | API + `CameraGroups.tsx` | none | web test | Low |
| Schedules (site TZ, midnight, DST) | `sites.timezone` [unverified] | Absent | `rules/rule.go` (+ schedule eval) | none (jsonb) | table tests incl. DST | Medium |
| Per-rule cooldown / rate limit | global 5 min debounce [code][test] | Not configurable | `rules/service.go` | none (jsonb) | integration | Low |
| Simulation / dry-run | pure `MatchesEvent` [code] | No endpoint/UI | `rules`, API, `Rules.tsx` | none | unit + API | Low |
| Templates | — | Absent | `rules` (static catalog), UI | none (code-defined) | unit | Low |
| Rule versions / audit diff | audit without diff [code] | No history | `rules/service.go` | `rule_versions` | integration | Low |
| Rule permission + validation | `notifications.manage` [code] | Coarse; no condition validation | `authz/catalog.go`, `rules/service.go` | grants seed | authz tests | Medium |
| Map-zone conditions | `ZoneResolver` interface [code] | Not wired | `apps/worker/main.go`, `maps` | none | integration | Low |
| Attention Center | Alarms page with status tabs [code][test]; Vision AI "alarmas" source [code] | No priority queue / video view / realtime alarm frames | new route + realtime routes | none (uses alarms) | web + e2e | Medium |
| Alarm list paging | `limit` only [code] | No cursor | `alarms`, openapi | index on (tenant, status, last_event_at) | API | Low |
| Optimistic locking | maps `If-Match` pattern [code] | None on rules/groups/alarms | rules, groups | `version int` columns | API | Low |
| Zone editor | `components/zones/*` [code][test] | Not persisted; whole-section replace | reuse UI; new zone API | `camera_zones` (normalized polygon, stable id, version, sync state) | web + integration | Medium |
| Zone sync with Frigate | per-section `config/set`, live sections incl. `zones` [code] | No ownership, no verify, no drift, no offline queue | `frigate/config_edit.go`, `inventory/frigate_config.go` | `camera_zones.sync_state`, `remote_hash` | integration with frigate-mock | **High** (production Frigate) |
| Rollback | whole-file raw save, always restart, no conflict check [code] | Unsafe for zones | zone-level rollback instead | — | — | High |
| Camera rebind / replace | soft delete, `missing_since` [code] | Rename → new camera | `inventory/servers.go` + API/UI | `cameras.previous_remote_names` or rebind audit | integration | **High** (identity) |
| Late alarm marking after downtime | cursor-based catch-up [code] | Old events treated as fresh | `events/syncer.go` | `alarms.late bool` | integration | Medium |
| Lost evaluation on crash | — | In-memory post-commit loop | `events/syncer.go` → outbox/NATS | `rule_eval_queue` (or JetStream) | integration | Medium |
| Single-alarm authz bug | list path correct [code] | Detail ignores server/group grants | `alarms/service.go` | none | authz integration | Medium (security) |

## 4. Proposed migrations and API (draft, for approval)

- **Migrations** (forward only):
  1. `alarms`: `correlation_key text`, `first_event_at`, `last_event_at`, `event_count int`, `late bool`,
     `auto_closed bool`; index `(tenant_id, status, last_event_at desc)`; partial unique index on
     `(tenant_id, correlation_key) WHERE status NOT IN ('resolved','closed')`.
  2. `alarm_events(alarm_id, event_id, tenant_id)` with RLS — links every correlated event (evidence).
  3. `alarm_settings`: `auto_close_after interval`, `event_alarm_mode` (`all_alerts` | `rules_only`).
  4. Backlog: data migration closing open alarms older than a cutoff as `closed` + `auto_closed=true`
     (rows kept; count logged).
  5. Later phases: `rule_versions`, `rules.version`, `camera_groups.filter jsonb` + `version`,
     `camera_zones` (+ RLS), camera rebind audit.
- **API** (OpenAPI first, `make generate`):
  - `listAlarms`: keyset cursor, `sort=priority|last_event_at`, returns `event_count`, `last_event_at`, `late`.
  - `listAlarmEvents` (evidence), realtime frames `alarm.opened` / `alarm.updated` routed to the web.
  - Rules: `camera_group_ids`, `schedule`, `cooldown_seconds` in conditions; `simulateRule` (dry-run);
    `listRuleTemplates`; `If-Match`/`version` on update.
  - Camera groups: `filter`, `member_count_total`, `member_count_hidden`.
  - Zones (Phase 5): `listCameraZones`, `upsertCameraZone`, `syncCameraZones`, `getCameraZoneSync`.

## 5. Revised phase plan

| Phase | Scope | Why this order |
|---|---|---|
| 1 — Alarm flood | correlation key + `alarm_events`, auto-close, event-alarm mode, backlog cleanup, fix single-alarm authz bug, late marking | Nothing else is usable while every alert is an alarm |
| 2 — Rules | groups, schedule (site TZ), per-rule cooldown, validation + dedicated permission, simulation, templates, versions, wire `ZoneResolver` | Cheap, high value, no Frigate writes |
| 3 — Attention Center | priority queue on correlated alarms, video, actions, realtime frames, cursor paging; optional Vision AI pin | Needs Phase 1 to be meaningful |
| 4 — Alarms UX | 4-step wizard, sections, group coverage display | Builds on 2-3 APIs |
| 5 — Zones & identity | persist zones (reuse existing editor), ownership `ovms_`, per-zone patch + verify, drift, offline queue; camera rebind flow | Highest production risk; editor already exists, so smaller than planned |
| 6 — Hardening | durable rule evaluation (no lost evals), publish-after-commit, multi-replica safety, e2e, latency | Cross-cutting |

Each phase keeps its own `odd/tasks/<phase>.md`, TDD, native review and ~400-line PRs.

## 6. Decisions needed at this checkpoint

1. **Correlation key** for incidents: `rule_id|camera_id` (+ open window), or `camera_id|label` for
   event-source alarms? Proposed: `source|rule_or_none|camera_id|label`, merging while open and within
   `auto_close_after` of the last event.
2. **Event-source alarms**: keep creating them for every Frigate `alert` (`all_alerts`) or only via rules
   (`rules_only`)? Proposed default for new installs: `rules_only`; existing installs keep `all_alerts` until
   changed in settings.
3. **Backlog cleanup cutoff**: close open alarms older than N hours (proposed 24h) as `auto_closed`, keeping
   rows and transitions.
4. **Auto-close default**: proposed 30 minutes without new events → `closed` (auto), configurable per tenant.
5. **Phase order** as in §5.

## 7. Risks

- Correlation changes the core ingestion path: needs integration tests with demofix + frigate-mock and a
  feature flag per tenant.
- Data migration on a large `alarms` table: batch it, run outside peak, log counts.
- Zone sync writes production Frigates: ownership prefix, live-apply only, explicit operator confirmation
  for any restart (observed restart: ~4.5 min, all cameras down).
- Camera identity rebind can mis-map cameras across tenants: restrict to same tenant, audit every rebind.
- Generated files (`api.gen.go`, `schema.d.ts`, `internal/store/db`) must stay in sync (CI checks).

## Progress and evidence
- Audit by three read-only explorers (pipeline; Frigate/zones/identity; groups/authz/web/API), synthesized
  here. No tests were run in this phase.

## Next step
**Checkpoint:** user reviews §1-§7 and answers the decisions in §6. No product code before approval.
