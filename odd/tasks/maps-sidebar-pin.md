# Maps Sidebar Pin

## Objective
Replace the Maps sidebar compact/collapse control with the same pin and edge-reveal behavior used by the Live explorer.

## Problem and rationale
The Maps panel had a circular chevron that collapsed the sidebar. The requested interaction is the Live sidebar's persisted pin: unpinned sidebars reveal at the edge on pointer/focus and hide after leaving; pinned sidebars remain visible.

## Scope and constraints
- Change the shared Maps sidebar, its MapShell/FloorMap bindings, and focused tests only.
- Reuse `live.pin`, `live.unpin`, and existing shared pin persistence; do not add locale keys.
- Do not commit. Parent may publish the web container if no competing web build is running.
- Preserve unrelated concurrent edits.

## TDD and verification
- Strict TDD enabled; runner: `pnpm exec vitest run` from `apps/web`.
- Test hidden/reveal/hide and pin/unpin persistence before implementation.
- Run MapShell, MapSocSidebar, and Live route tests.

## Tasks
- [x] MAP-PIN-1 Add a focused integration test for the Maps sidebar's pin control and shared persistence (initial RED observed).
- [x] MAP-PIN-2 Mirror Live edge reveal/hide (280 ms leave delay), keyboard access, FontAwesome pin visual and shared pin behavior; remove collapse control.
- [x] MAP-PIN-3 Run focused Maps/Live tests and record results.

## Progress and evidence
- Initial test-first run failed as expected because the map panel had no pin button.
- First implementation pass added shared pin state but left the sidebar always visible; parent review correctly identified that as incomplete parity. Full edge behavior now matches Live's persisted pin and pointer/focus reveal, including the edge affordance; both MapShell and FloorMap use the shared pin key.
- Existing MapShell test interactions that require sidebar content now focus the edge affordance first. Focused suites: 65/67 pass; remaining failures are `opts into live hover at 700ms without acquiring a second session` (surface slot assertion) and `synchronizes mode with URL navigation and does not preview in investigation` (expected `site` query missing). The MapSocSidebar and Live route suites pass. `pnpm typecheck` still fails only in existing `LiveExplorer.tsx` union typing and four `pinnedWindows.test.ts` undefined checks; no errors remain for MapSocSidebar/FloorMap props.

## Next step
Parent independent verification passed: new MapShell pin tests 2/2, MapSocSidebar/Live 33/33, FloorMap 5/5; parent spot-check pin tests 2/2. Changed-file whitespace checks passed. Web image published: `sha256:e03e8148aec03831c51171307622a217ed91a3f0aafe94cfdc2b32276feb480f`. No commits. Native review assessment was unavailable without an explicit untracked inventory; preflight requested selection and no review transaction or approval was created. Parent owns any follow-up review and existing test/typecheck failures.

## Routing and recovery
Delegated writer: preparation and multiple non-trivial source files. Independent read-only verifier used for unassessable/high review tier. Tracker creation occurred after initial edits; this ordering miss is recorded rather than presented as compliant. Rollback boundary: the shared sidebar pin interaction, MapShell/FloorMap bindings and corresponding focused tests; unrelated working-tree edits must be preserved. No authenticated browser interaction was performed.
