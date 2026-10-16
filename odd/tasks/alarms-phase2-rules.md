# Alarms Phase 2 — rules engine, groups, schedules, validation, simulation & ZoneResolver

## Objective
Upgrade the OpenVMS rules engine and camera groups so operators can author expressive, validated, time-aware
and zone-aware rules with per-rule cooldowns, version history, templates, dry-run simulation, and dynamic
camera groups.

## Problem and rationale
See `odd/tasks/alarms-phase0-audit.md` §1 (items 5, 7) and §3-§5:
- `rules.Conditions` lacks `camera_group_ids`, `schedule` (with site timezone, midnight-crossing, and DST),
  and configurable `cooldown_seconds` (currently hardcoded to 5m global debounce).
- `rules.ZoneResolver` is not wired in `apps/worker/main.go`, so `map_zone_ids` never match in production.
- Rules have no dedicated permission (reusing `notifications.manage`), no condition validation, no version
  history (`rule_versions`), and no optimistic locking (`version` / `If-Match`).
- Operators have no rule templates or dry-run (`simulateRule`) to preview matches before enabling a rule.
- Camera groups are manual-only (`camera_groups.filter` absent) and do not expose `member_count_total` vs
  `member_count_hidden` when a user has partial camera visibility.

## Scope and constraints (approved decisions from Phase 0)
- Camera groups in rules (`camera_group_ids` in `rules.Conditions`) + dynamic camera groups (`camera_groups.filter jsonb` materialized into `camera_group_members`) + group coverage counts (`member_count_total`, `member_count_hidden`).
- Schedules (`schedule` in `rules.Conditions`: days of week, start/end `HH:MM`, evaluated in camera's site timezone with fallback to UTC, supporting overnight windows e.g. `22:00`–`06:00` and DST transitions) + per-rule `cooldown_seconds` (default 300s when unset).
- Wire `rules.ZoneResolver` in `apps/worker/main.go` (and store/maps helper) so `map_zone_ids` matches cameras located inside or linked to the specified map zones.
- Dedicated rule permissions (`rules.read`, `rules.manage` seeded in `authz/catalog.go` + migration grant seed for existing roles that had `notifications.manage` / `alarms.manage`), condition validation (reject invalid condition combinations such as `server_ids` on `event` rules, validate schedule/cooldown/scope access), `rules.version` optimistic locking, and `rule_versions` history table (RLS).
- Rule templates (`GET /api/v1/rules/templates`) and dry-run simulation (`POST /api/v1/rules/simulate` against recent events in a time window) exposed in OpenAPI, backend, and `Rules.tsx`.
- Forward-only migrations, RLS on new tables, OpenAPI first + `make generate`.

## TDD
- Mode: strict (`strict_tdd: true`, source: user global config).
- Runners: `go test -race ./...`; integration `go test -race -tags integration ./internal/...`;
  web `cd apps/web && pnpm vitest run <file>`.

## Tasks
- [x] P2-01 — Conditions: `camera_group_ids`, `schedule` (site TZ, overnight windows, DST), and per-rule
      `cooldown_seconds` in `internal/rules`; unit + integration tests. Route: delegated writer.
- [x] P2-02 — Wire `rules.ZoneResolver` in `apps/worker` + store/maps lookup so `map_zone_ids` conditions
      match in production; integration test. Route: same writer.
- [x] P2-03 — Migration + backend: `rules.version` (optimistic locking), `rule_versions` (RLS, version snapshot
      history), dedicated `rules.read` / `rules.manage` permissions (seeded onto existing roles), and rule
      condition validation; unit + integration tests. Route: same writer.
- [ ] P2-04 — Dynamic camera groups (`camera_groups.filter jsonb` materialized into `camera_group_members` +
      `version`) and group coverage counts (`member_count_total`, `member_count_hidden`) in API and
      `CameraGroups.tsx`; integration + web tests. Route: same writer.
- [ ] P2-05 — Rule templates (`listRuleTemplates`), dry-run simulation (`simulateRule` against recent tenant
      events), version history endpoint (`listRuleVersions`), and `Rules.tsx` UI (schedule, groups, cooldown,
      templates, dry-run preview); integration + web tests. Route: same writer.

## Acceptance criteria and checks
- Event rule with `camera_group_ids`, `schedule` (site TZ, overnight), and custom `cooldown_seconds` matches
  and debounces accurately.
- Event rule with `map_zone_ids` matches events from cameras in those map zones in `apps/worker`.
- Invalid rule conditions are rejected with 400; concurrent stale `version` updates fail with 409; each
  create/update records a `rule_versions` row.
- Dynamic camera groups materialize matching cameras and report `member_count_total` / `member_count_hidden`.
- `simulateRule` and `listRuleTemplates` work end-to-end in API and `Rules.tsx`.
- `make generate`, `go vet ./...`, `go test -race ./...`, integration tests, and web vitest/typecheck pass.

## Delivery
- Forecast ~1100 authored lines (+ generated) → chained PRs, `stacked-to-main`:
  PR-2A (`P2-01..P2-02`), PR-2B (`P2-03..P2-04`), PR-2C (`P2-05`).

## Progress and evidence
- Branch `feat/alarms-phase2-rules` created from `main` (`30b3e2d`).
- P2-01 done in `bccf614`; P2-02 done in `7bc28de`. Checks (2026-10-16): `go vet ./internal/rules/ ./apps/worker/`
  ok; `go test -race ./internal/rules/` ok; `go test -race -tags integration ./internal/rules/` ok.
- RDD: assessed `high` (process_boundary in `apps/worker/main.go`), consent granted, 4-lens review
  `review-7e3ddcaf755fa39d` approved and acknowledged. Reviewed boundary advances to `7bc28de`.
- Advisory follow-ups (non-blocking, not yet in scope): site-timezone lookup errors swallowed silently
  (`service.go:444-459`); schedule TZ precedence / silent never-match edge semantics (`rule.go:158-202`);
  duplicate ZoneResolver wiring (`apps/worker/main.go:120`); ZoneResolver per-event DB load; frigate zone
  cross-camera match (`zones.go:123-125`); potentially flaky 1s cooldown integration test; day-of-week `7`
  alias undocumented; unbounded `cooldown_seconds` (to be covered by P2-03 validation).

- P2-03 implemented in `2161587` (delegated writer): `rules.version` + required `If-Match` (stale → 409),
  `rule_versions` (RLS, snapshot per create/update), `rules.read`/`rules.manage` (handlers switched off
  `notifications.manage`), `ValidateConditions` (cooldown 0..86400, schedule days 0..6, trigger/field combos).
  Checks: `make generate` ok, `go vet ./...` ok, `go test -race ./...` ok, integration rules/api/authz/store ok,
  web typecheck + vitest (1524) ok; parent spot check `go test -race -count=1 -tags integration ./internal/rules/
  ./internal/authz/` ok. RDD: high (`rules.sql.go` signal), consent granted, 4-lens review
  `review-2fe9adbb8eb32697` approved + acknowledged; boundary → `2161587`.
- Accumulated-branch review attempt (main..2161587) stopped with `lens_context_budget_exceeded`; no authority
  created; slices already reviewed separately.
- Review correction (scoped, in P2-03 scope) `a9e4c3b`: non-condition PATCH (e.g. `enabled=false`) no longer
  re-validates legacy-invalid stored conditions; stored-conditions decode error surfaced. RED observed
  (`TestRuleUpdateNonConditionPatchOnLegacyInvalidRule`, `TestRuleUpdateCorruptStoredConditionsSurfacesError`);
  vet, unit, integration rules + api `-run Rule` ok.
- Decision (user, 2026-10-16): keep broadened seeding (`alarms.manage` → `rules.manage`+`rules.read`,
  `alarms.view` → `rules.read`) AND mirror explicit `alarms.*` denies into rules permissions (new forward-only
  migration). Done in `cd96890` (`00051_rules_alarm_deny_mirror.sql` + semantic grant-seeding integration
  test). RED observed (role allow + direct deny still yielded rules.manage); vet, unit, integration
  authz/rules/store ok; parent spot check grant-seeding test ok. Gap: 00051 Down not exercised.
- Open P2-03 advisories: `If-Match` hard requirement breaks stale SPA bundles / API clients during rollout
  (no grace period); UI does not refetch on 409; per-user camera visibility not enforced on rule references;
  web edit form can drop API-only condition fields (to fix in P2-05).

## Next step
Review slice `2161587..HEAD` (correction + deny mirror + doc), then P2-04 (route: delegated writer).
