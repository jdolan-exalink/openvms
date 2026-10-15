# Vision AI live validation against real Frigate

## Objective
Validate Vision AI end to end on the running stack (3 real Frigate servers, 89 cameras) and fix what the
live run exposes.

## Problem and rationale
The headless-browser run of `/vision-ai` against real data showed:
- the 2×2 grid collapsed to a ~150px strip (route not in the fit-viewport workspace list);
- the whole grid churned every ~6s: the already-visible penalty (-15) made every on-screen camera lose to
  an equally ranked newcomer once min display elapsed, so videos never finished loading;
- 139,362 `open` alarms (auto-created per event since 2026-09-29, never closed) flagged 130 of 142 recent
  events as critical (+100), flattening the ranking.

## Scope and constraints
- User decision (alarms): rank by Frigate review severity (`alert` → high +60), ignore alarm rows.
- Note: this document was created after the fixes were written (tracking step missed); recorded honestly.

## TDD
- Mode: strict (source: user global CLAUDE.md). Runners: `go test -race`, `pnpm vitest run`.

## Tasks
- [x] VLV-01 — Fit `/vision-ai` to the viewport (`Layout.tsx`). Route: inline. `8209b14`.
- [x] VLV-02 — Allocator hysteresis: compare against incumbent standing score (without visible penalty) and
      require `replaceMargin` (10) before max display. Route: inline. `9f63e92`.
- [x] VLV-03 — Severity from Frigate `alert`/`detection`; drop alarms lookup. Route: inline. `7a30d70`.

## Progress and evidence
- VLV-01: Layout.test "fits Vision AI to the viewport" RED (missing classes) → GREEN; screenshot shows a
  full-height 2×2 grid with live video.
- VLV-02: `TestAllocatorKeepsIncumbentAgainstEqualNewcomers` RED ("equally ranked newcomer evicted the
  incumbent before max display") → GREEN; existing min-display test updated to use a clearly higher-scoring
  newcomer (the old "+4 points replaces" expectation encoded the churn). Live: slots now hold ~30s
  (max display) instead of ~6s; all four videos reach readyState 4.
- VLV-03: `TestRowToEvents` RED (alert expected Type alarm) → GREEN; integration expects `high`. Live: top
  slots score 135 (alert person) instead of uniform 160 critical.
- Checks: `gofmt -l` empty; `go vet ./...`, `go test -race ./...` clean; integration VisionAI/PGSource/LoadRows
  ok; `golangci-lint` visionai 0 issues; web typecheck+lint clean; full `pnpm test` 175 files / 1480 tests.
- LPR source: empty during the run — no `lpr_reads` in the last 10 min on this stack (expected empty state).

- Native review (lineage review-c61b5aeaa1968b8f): APPROVED, acknowledged. Advisories addressed in a test
  commit: margin boundary (exactly at margin keeps incumbent, one point past evicts; mutation `>`→`>=`
  verified to fail the test) and negative `already_visible_penalty` assertion. Open-alarm advisory: the
  `OpenAlarm` field no longer exists, so alarm rows cannot affect ranking structurally.

## Next step
Push + PR (user decision).
