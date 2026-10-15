# Vision AI follow-ups and red CI on main

## Objective
Close every pending item left after delivering Vision AI (#54, #55): get `main` CI green and resolve the
non-blocking native review advisories.

## Problem and rationale
- CI on `main` is red: gofmt in `internal/agent/sample.go`, `internal/provision/agent_status_test.go`,
  `internal/store/maps_core_test.go`; gosec G301/G306 in `internal/agent/sample_test.go`, G115 in
  `internal/provision/agent_status.go:199`; `Layout.test.tsx` bottom-nav expects `Alarmas` but the nav entry
  added in 519805c makes `Vision AI` the fourth primary destination.
- Review advisories: Live tile pin reappears after A→B→A source switch; the LPR "requires permission" notice
  cannot tell missing permission from events without plates (no signal in the API); no test for the
  "Sin lecturas de patente recientes" branch.
- Feature doc `odd/tasks/vision-ai-live-engine.md` lacks the final merge record.

## Scope and constraints
- Lint fixes must be real fixes (tighter permissions / safe conversion), not `nolint`, unless a justified
  exception is documented inline.
- API change is additive: `VisionAISlots.plates_visible` (bool, required) = actor has `lpr.view` on at least
  one permitted camera. OpenAPI-first, regenerate Go + TS.
- Advisory ~400 authored changed lines per task. No push/merge without the user.

## TDD
- Mode: strict (source: user global CLAUDE.md). Runners: `go test -race ./...`, integration
  `go test -race -tags integration ./internal/...`, web `cd apps/web && pnpm vitest run <file>`.

## Tasks
- [x] VFU-01 — Go lint: gofmt the three files; fix gosec G301/G306 (test dirs 0750, files 0600) and G115
      (bounded conversion). Route: delegated writer.
- [x] VFU-02 — API `plates_visible` + web LPR notice uses it; test for "Sin lecturas de patente recientes".
      Route: delegated writer (same).
- [x] VFU-03 — LiveVisionTile clears (not hides) the pin when the source changes; A→B→A test. Route: same writer.
- [x] VFU-04 — Mobile bottom nav: user decision 'keep Alarmas' → primary stays En vivo, Eventos, Patentes,
      Alarmas; Vision AI goes to "Más". Add `/alarms` to preferred primaries in `nav.ts`. Route: inline (1 file + test).
- [x] VFU-05 — Record final merge of #55 in `odd/tasks/vision-ai-live-engine.md`. Route: inline.

## Acceptance criteria and checks
- `gofmt -l internal apps` empty; `golangci-lint run` (if installed) / CI Go lint green; `go vet ./...`.
- `go test -race ./...` and integration for touched packages green.
- `pnpm typecheck`, `pnpm lint`, full `pnpm test` green (including Layout.test).

## Delivery
- Strategy ask-on-risk; forecast ~300 authored lines (+ generated) → single PR expected.
- RDD on (global).

## Progress and evidence
- Branch `fix/vision-ai-followups-and-ci` from main `7c8c7c2`.
- VFU-05 in `1430f22` (feature doc records #55 merge).
- VFU-01 `05eae8f`: gofmt (3 files), gosec G301/G306 in agent tests (+ watchdog_test.go found by local
  lint), G115 via `signedSnapshotBytes` clamp; RED `PrunedSegmentsCount = -9223372036854775808` → GREEN.
  `golangci-lint` v2.14.0 (CI pin) → 0 issues.
- VFU-02 `21f4b6b`: `VisionAISlots.plates_visible` (required); `EventSource.RecentEvents` → `EventSet{Rows,
  PlatesVisible}`; LPR notice only when false, "Sin lecturas de patente recientes" when true. Unit +
  integration (operador false / admin true) green; web RED→GREEN.
- VFU-03 `058f239`: tile pin reset on `vision.id` change; A→B→A test RED→GREEN.
- VFU-04 `<this commit>`: `/alarms` added to preferred mobile primaries; Layout.test RED (Vision AI vs
  Alarmas) → GREEN, plus assertion Vision AI is in the More sheet.
- Checks: `gofmt -l` empty; `go vet`, `go build`, `go test -race ./...` clean; integration api/visionai/
  provision/agent ok; web typecheck+lint clean; full `pnpm test` 175 files / 1479 tests passed.

## Next step
RDD assess, then push + PR (user decision).
