# Vision AI: choose which servers' events are shown

The server selector built for Live (#155) belongs in Vision AI: the view is assembled there, and
what it should show depends on which servers are selected. Live goes back to what it was.

Decisions:
- Selecting servers filters everything Vision AI shows: the slot grid, the LPR and face modes, and
  the events side bar (active, historic, LPR, face tabs). The Vision AI tile inside Live is unchanged.
- All servers are selected by default and the choice is remembered per user. What is stored is the
  set of servers left **unchecked**, so a server added later is shown without anyone touching it.
- At least one server stays checked: with none there is nothing to show, and an empty filter
  elsewhere means "all".
- The slot grid is filtered in the backend (`server_id[]` on `GET /vision-ai/slots`), before the
  allocator ranks, so unchecking a busy server does not leave the grid empty while others have
  events. The allocator keeps slot state per session, so each selection gets its own session and no
  camera of an unchecked server lingers in a slot.
- Events already filter by `server_id`. Plate reads have no server filter, so the web sends the
  `camera_id`s of the selected servers (only when some server is unchecked).

## Slices

- [x] 1. Backend: `server_id[]` on the Vision AI slots, filtered before ranking, one allocator per selection.
- [ ] 2. Live: remove the server selector and its grid-based helpers.
- [ ] 3. Vision AI: server selector, remembered selection, and every list filtered by it.

## Delivery chain

- Strategy: auto-chain, stacked to `main`; each PR targets the immediately preceding branch.
- PR 1, `feat/vision-ai-server-filter-backend`, starts at `origin/main` and ends with the
  server-filtered slots API and per-selection allocator. Commit: `a6b0bcc`.
- PR 2, `feat/vision-ai-server-filter-core`, depends on PR 1 and ends with the shared server
  selection, preference, and filtering libraries; retain Live imports/exports for compatibility.
- PR 3, `feat/vision-ai-server-filter-ui`, depends on PR 2 and ends with Live removal and Vision AI
  selector integration, localization, and route coverage. Keep each authored diff at or below 400
  changed lines; generated contract outputs are excluded from that budget.
