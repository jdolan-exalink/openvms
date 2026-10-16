# Alarms Phase 1 — stop the alarm flood

## Objective
Turn alarms into operable incidents: one alarm per ongoing situation instead of one per event, auto-close
when activity stops, no event-source alarms for new Frigate servers, clean the existing backlog, mark late
alarms, and fix the single-alarm authorization bug found in Phase 0.

## Problem and rationale
See `odd/tasks/alarms-phase0-audit.md` §1-§3: `events.Syncer.openAlarm` creates an alarm per Frigate `alert`
event, rules add a second one per event (`UNIQUE(event_id, source)`), nothing auto-closes → 145,338 open.

## Scope and constraints (approved decisions)
- Correlation key: `source|rule_id-or-none|camera_id|label`; an event joins the open alarm with the same key
  if its last event is within the auto-close window; otherwise a new alarm. Every correlated event is linked
  in `alarm_events` (evidence). `event_count`, `first_event_at`, `last_event_at` maintained.
- Auto-close: default 30 min without new events → `closed` with `auto_closed=true`, transition recorded with
  a system actor/comment; configurable per tenant (`alarm_settings.auto_close_after`).
- Event-source alarm mode per Frigate server: `all_alerts` | `rules_only`. Existing servers migrate to
  `all_alerts`; servers registered after the migration default to `rules_only` (and fresh installs too).
- Backlog: data migration closes open alarms whose last activity is older than 24h as `auto_closed`; rows,
  transitions and audit kept; counts logged.
- Late alarms: events older than the auto-close window at ingestion (catch-up after downtime / backfill)
  create alarms flagged `late=true` (shown, not hidden).
- Fix: single-alarm operations must authorize with server and camera-group scopes (same as list).
- Must not break: alarm transitions, UI tabs, notifications, Vision AI, existing tests. Forward-only
  migrations, RLS on new tables, OpenAPI first + `make generate`.

## TDD
- Mode: strict (source: user global CLAUDE.md). Runners: `go test -race ./...`; integration
  `go test -race -tags integration ./internal/...`; web `cd apps/web && pnpm vitest run <file>`.

## Tasks
- [ ] P1-01 — Migration: alarms correlation columns + `alarm_events` (RLS) + indexes; correlation in syncer
      (`openAlarm`) and rules path; integration tests (N events same key → 1 alarm, different label/camera →
      separate, closed alarm → new one). Route: delegated writer.
- [ ] P1-02 — Auto-close sweeper in worker + `alarm_settings.auto_close_after` (default 30m) + transition with
      system actor; tests with fake clock. Route: same writer.
- [ ] P1-03 — `frigate_servers.event_alarm_mode` (existing → all_alerts, new → rules_only) honored by syncer;
      API/OpenAPI field on server; settings UI toggle on the server page. Route: same writer.
- [ ] P1-04 — Backlog cleanup migration (24h cutoff, auto_closed, counts). Route: same writer.
- [ ] P1-05 — `late` flag for alarms created from old events; exposed in API/UI. Route: same writer.
- [ ] P1-06 — Single-alarm authz with server/group scopes + integration test (grant at server/group scope
      can operate on the alarm). Route: same writer.
- [ ] P1-07 — API: `listAlarms` keyset cursor + `event_count`, `last_event_at`, `late`; `listAlarmEvents`;
      Alarms page shows count/last event/late badge. Route: same writer.

## Acceptance criteria and checks
- A person staying 10 minutes in front of a camera with several Frigate reviews → 1 alarm with event_count>1.
- No new alarms for a newly registered Frigate server without rules.
- After the migration, open alarms in this stack drop from ~145k to the last 24h of activity.
- gofmt/golangci-lint/vet/`go test -race ./...`/integration/web tests green; generated code in sync.

## Delivery
- Forecast ~1200 authored lines (+ generated) → chained PRs, stacked-to-main (user's earlier choice):
  PR-A P1-01..P1-02, PR-B P1-03..P1-05, PR-C P1-06..P1-07. RDD on (global).

## Progress and evidence
- Branch `feat/alarms-phase1-flood` from main.

## Next step
P1-01.
