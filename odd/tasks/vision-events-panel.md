# Vision AI events panel: popup, rename and tabs

## Goal

In Vision AI, clicking an event in the side bar opens a popup above the grid with the event
(image, camera, object, zone, time, and the plate when there is one) that can be closed. The
side bar is renamed "Eventos" and gets four tabs: Activos, Históricos, LPR and Faciales.

## Decisions

- One view model (`VisionEventView`) feeds the list rows and the popup, whatever the source
  (active slot, historic event, plate read, face event).
- Historic and Faciales use `/api/v1/events` (`label=face` for faces); LPR uses `/api/v1/lpr/reads`.
  Tab data is fetched only while its tab is open. Tabs are hidden without `events.view` / `lpr.view`.
- Clicking an active event no longer jumps to focus mode by itself; the popup offers
  "Fijar en foco" for that.
- No API or contract change.

## Slices

- [x] 1. `lib/visionEvents.ts`: view model, normalizers for the four sources, tab data hooks, with tests
- [x] 2. `EventPopup` component: image fallback chain, plate via `ArPlate`, close (button and Escape), pin-to-focus action
- [ ] 3. `EventsSidebar` with tabs, wired into `VisionAI.tsx` (rename to "Eventos", popup above the grid); update existing tests

## Verification

- `pnpm --dir apps/web typecheck`
- `pnpm --dir apps/web test -- --run <touched test files>`
