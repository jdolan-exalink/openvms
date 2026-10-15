# Vision AI icons and detection snapshots

## Objective
Make Vision AI faster to read: icons on object filters, presets and slot object labels, and detection
snapshots in the "Eventos Activos" sidebar.

## Problem and rationale
Object chips, presets and the HUD object badge are text-only; the active events sidebar lists events without
the detection image, so the operator cannot tell at a glance what was detected.

## Scope and constraints
- Web only (`apps/web`): `routes/VisionAI.tsx`, `components/live/LiveVisionTile.tsx`, shared helper.
- Icons from `lucide-react` (already used), one shared object→icon map; `aria-hidden` on decorative icons.
- Snapshot: `/media/v1/events/{event_id}/snapshot.jpg` when the user has `snapshots.view`, else
  `/api/v1/events/{event_id}/thumbnail`, falling back to the thumbnail on error (same pattern as
  `routes/Events.tsx`); lazy-loaded; refreshes when the slot's event changes.
- Interpretation of "títulos en vivo": object labels in slot HUDs (`/vision-ai` and Live tile).

## TDD
- Mode: strict (source: user global CLAUDE.md). Runner: `cd apps/web && pnpm vitest run <file>`.

## Tasks
- [ ] VIS-01 — Object icon map + icons on chips, presets and slot/tile object badges. Route: delegated writer.
- [ ] VIS-02 — Detection snapshots in "Eventos Activos" sidebar. Route: same writer.

## Acceptance criteria and checks
- `pnpm typecheck`, `pnpm lint`, VisionAI/LiveVisionTile tests, full `pnpm test` green.
- Live check on the running stack: icons visible; sidebar shows real snapshots.

## Delivery
- Forecast ~250 authored lines → single PR. RDD on (global).

## Progress and evidence
- Branch `feat/vision-ai-icons-snapshots` from main `7882e54`.

## Next step
VIS-01, VIS-02.
