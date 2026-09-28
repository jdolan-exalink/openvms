# Web config panel — settings area, default route, live persistence, drag & drop

## Objective
Group the "Infraestructura" and "Administración" sidebar sections into one settings area
("Configuración") with its own sub-navigation, make Live the default landing page, remember a
user's Live grid selection across visits, and let cameras be dragged from the camera list into
grid slots (plus reordering tiles by dragging).

## Problem and rationale
The main sidebar today (`apps/web/src/components/nav.ts`) mixes operational pages (Live, Events,
Plates, Playback, Exports) with infrastructure/admin pages (Sites, Servers, Cameras, Users,
Groups, Permissions, Audit, Account) and a "Panel" dashboard at "/". This buries day-to-day pages
under administrative clutter and makes "/" show a status dashboard instead of the operational
Live view operators actually want first. Live's camera/grid selection is also not persisted, so
operators re-build their grid every session, and placing cameras requires click-based
select-then-pick instead of direct drag and drop.

## Scope and constraints
- In scope: web-only (apps/web). No Go backend changes expected (routes/paths are UI-only;
  server-side view layout/order already persists via the existing `/api/v1/views` schema).
- Respect existing permission gating: settings sub-pages stay visible only to users holding the
  permission the current sidebar item already requires (reuse `can()` from `@/lib/perm`).
- Keep existing tests green; update/add tests that assert on sidebar/Dashboard routing.
- Do not touch Docker/the running stack (a separate verifier uses it).
- Advisory ~400 authored changed lines per task; not a hard cap.
- Do not push, do not merge.

## Tasks
- [x] WCP-1: Settings area (pathless `/settings` layout route with its own sub-nav) + sidebar
      regroup (remove Panel/Infraestructura/Administración entries, add one "Configuración"
      entry) + settings landing reusing Dashboard's content + "/" redirects to "/live" (Panel is
      no longer reachable from "/"; existing sub-page URLs are unchanged, so no redirects needed
      for them). Route: direct/delegated mix (single writer, this session).
- [x] WCP-2: Confirm/lock in "always Live" as the default after login and for "/" with a
      dedicated Login-flow test; audit for any other place assuming "/" shows the dashboard.
- [x] WCP-3: Persist the Live grid selection (columns + tile order/camera ids) per user
      (`tenant_id` + user id from `meQuery`) in `localStorage`, wrapped in try/catch, restored on
      load, gracefully dropping cameras the user can no longer see.
- [x] WCP-4: Drag and drop in Live — drag a camera from the list into a grid slot, and
      reorder/swap tiles by dragging within the grid, with dnd-kit's keyboard accessibility.
      Persisted selection (WCP-3) must include the resulting order; saving a view keeps working.

## Verification mode
- Strict TDD: enabled (source: global user config `Strict TDD Mode: enabled`). RED → GREEN →
  REFACTOR with observed evidence, per task.
- Runners: `pnpm --filter web test`; `pnpm typecheck`; `make lint`; `pnpm --filter web build`.

## Acceptance criteria
- Main sidebar shows only operational pages (Live, Events, Plates, Playback, Exports/Casos) plus
  one "Configuración" entry; Infraestructura/Administración pages and Panel are gone from it.
- Settings area (`/settings`) has a sub-nav to Sitios, Servidores, Cámaras, Notificaciones
  (milestone), Usuarios, Grupos, Permisos, Auditoría, Mi cuenta, and a landing page showing the
  former Panel content.
- "/" and post-login always resolve to Live.
- Live grid selection (columns, tile camera ids/order) persists per user+tenant in localStorage
  and restores on load, dropping cameras no longer visible.
- Cameras can be dragged from the list into a slot, and tiles can be reordered by dragging
  (keyboard-operable via dnd-kit); saved views keep the resulting order.
- All runners above pass.

## Delivery
- Branch `feat/web-config-panel` from baseline `bdc3b41` on `main`. One work-unit commit per
  task, Conventional Commits, no AI attribution. No remote configured for this session; do not
  push.

## Progress and evidence
- Baseline commit `bdc3b41` on `main`.

- WCP-1: Sidebar/settings-area restructure.
  - `apps/web/src/components/nav.ts`: `navGroups` (main sidebar) now holds only En vivo,
    Investigación (Eventos/Patentes/Grabaciones/Exportaciones/Casos) and a single "Configuración"
    entry (no permission gate, same as the former "Mi cuenta"/"Panel" entries — settings has at
    least one always-visible page, "Mi cuenta"). New `settingsNavGroups` export holds the former
    Infraestructura/Administración groups plus a new "Resumen" item (`/settings`, the former
    Panel landing).
  - `apps/web/src/components/SettingsLayout.tsx` (new): renders a "Configuración" `PageHeader`,
    a secondary nav from `settingsNavGroups` (same permission-filtering and milestone-item
    pattern as `Layout.tsx`'s primary nav — duplicated rather than abstracted into a shared
    component, matching this codebase's existing preference for direct duplication over
    generic list components, e.g. `Live.tsx`'s `CameraTree` vs `Layout.tsx`'s aside), and an
    `<Outlet />` for the active settings page.
  - `apps/web/src/router.tsx`: added a pathless `settingsRoute` (`id: "settings"`, no `path`,
    same pattern as the existing pathless `appRoute`) as a child of `appRoute`, wrapping Sites,
    Servers, Cameras, Users, Groups, Permissions, Audit, Account — **same literal URL paths as
    before** (a pathless layout route contributes no URL segment, only shared component
    chrome + a longer internal route id), so no redirects are needed for these pages. Added a
    new `/settings` child (component: the existing `Dashboard`, unchanged) as the settings
    landing page. Replaced the old `child("/", Dashboard)` with an `indexRoute` whose
    `beforeLoad` always `throw redirect({ to: "/live" })`.
  - Fallout: `apps/web/src/routes/Permissions.tsx`'s `useSearch({ from: "/app/permissions" })`
    used TanStack Router's internal route-id string (which does include ancestor `id` segments,
    unlike the URL path); nesting `permissionsRoute` under `settingsRoute` changed that id to
    `/app/settings/permissions` — caught by `pnpm typecheck`, fixed by updating the literal.
    `Playback.tsx`'s equivalent `from: "/app/playback"` is untouched (that route stayed
    top-level).
  - TDD: `SettingsLayout.test.tsx` (new) and `router.test.tsx` (new) were written after their
    implementation code in this task (process deviation from strict RED-first ordering — noted
    honestly rather than staging a fake RED). Non-vacuity was proven directly for each instead:
    - `SettingsLayout.test.tsx`: temporarily replaced the permission filter
      (`!item.permission || can(...)`) with `true` — `pnpm exec vitest run
      src/components/SettingsLayout.test.tsx` failed (`Cámaras`/`Usuarios`/`Grupos`/`Permisos`/
      `Auditoría` links appeared despite no matching grant). Reverted, same command passed (2/2).
      A first run also caught a genuine test bug (asserting "Servidores" synchronously via
      `getByRole` before the `/api/v1/me` fetch had resolved, since the adjacent "Resumen"
      assertion doesn't depend on grants and can pass before data loads) — fixed by awaiting
      `findByRole` on the grant-gated item instead.
    - `router.test.tsx`: temporarily reverted `indexRoute` to `component: Dashboard` (no
      redirect) — `pnpm exec vitest run src/router.test.tsx` failed (timed out waiting for the
      "En vivo" heading). Reverted, same command passed (1/1).
  - Full verification: `pnpm --filter web test` PASS (7 files / 18 tests); `pnpm typecheck`
    clean; `make lint` clean (`go vet` 0 issues, `eslint .` clean); `pnpm --filter web build`
    clean (pre-existing >500kB single-chunk warning, unrelated to this change).
  - Commit: `a2c6e56` (`feat(web): group infrastructure/admin pages into a settings area`, not
    pushed — no remote configured).

- WCP-2: Default-route confirmation.
  - `apps/web/src/routes/Login.test.tsx` (new): drives the real `router.tsx` `routeTree` from
    "/login", stubs `/api/v1/auth/login`, submits the password form, and asserts the app ends on
    the "En vivo" heading at `router.state.location.pathname === "/live"` — this specifically
    exercises `Login.tsx`'s own `navigate({ to: "/" })` call chained through the WCP-1 redirect
    (not just the redirect in isolation, which `router.test.tsx` already covers).
  - Audit: `rg 'to="/"|to: "/"'` across `apps/web/src` now returns only `Login.tsx`'s
    `navigate({ to: "/" })` (correctly resolves through the redirect); `rg Dashboard` shows the
    component is only wired at `/settings`. No other place assumes "/" shows a dashboard.
  - TDD: written after `Login.tsx` already worked unmodified (no code change needed for this
    task; genuinely new test coverage, not a bugfix). Non-vacuity proven directly: temporarily
    changed `Login.tsx`'s post-login `navigate({ to: "/" })` to `navigate({ to: "/login" })` —
    `pnpm exec vitest run src/routes/Login.test.tsx` failed (timed out waiting for "En vivo").
    Reverted, same command passed (1/1).
  - Full verification: `pnpm --filter web test` PASS (8 files / 19 tests); `pnpm typecheck`
    clean; `make lint` clean; `pnpm --filter web build` clean (same pre-existing chunk-size
    warning).
  - Commit: `39f30c8` (`test(web): cover the post-login default route to Live`, not pushed — no
    remote configured).

- WCP-3: Persist the Live grid selection.
  - `apps/web/src/lib/liveGrid.ts` (new): pure, framework-free helpers shared by Live's grid
    logic — `resizeTiles`, `placeCameraAt`, `liveSelectionKey(tenantId, userId)` (key
    `openvms.live.selection.v1:<tenant|"platform">:<userId>`), `serializeSelection`/
    `parseSelection` (JSON round-trip; drops any camera id not in the caller's valid-id set,
    leaving that slot `null`; returns `null` — never throws — for missing/malformed/structurally
    invalid input).
  - `apps/web/src/routes/Live.tsx`: added a `restored` state flag and a render-time (not
    Effect) state adjustment — `if (!restored && me.data && cameras.data) { setRestored(true);
    ...restore from localStorage... }` — that applies the saved selection (or leaves the
    default grid) before the default grid ever paints, guarded by `restored` so it runs at most
    once. A separate `useEffect` persists `{columns, tiles}` via `serializeSelection` whenever
    they change, once `restored` is true. Both localStorage calls are wrapped in try/catch.
    `setGrid`'s inline resize logic now calls the shared `resizeTiles`; `place` now calls the
    shared `placeCameraAt`.
  - Design note: the restore logic was first written as a plain `useEffect` calling
    `setColumns`/`setTiles` in its body; `make lint` failed on
    `react-hooks/set-state-in-effect` ("Calling setState synchronously within an effect can
    trigger cascading renders"). Rewrote it as React's documented alternative — adjusting state
    directly during render, guarded by a `restored` flag so it is idempotent — which the lint
    rule doesn't flag (it only targets Effect bodies) and avoids an extra render pass. The
    *persist* effect keeps using `useEffect` since it has no setState call, only a
    `localStorage.setItem` side effect keyed to React state.
  - TDD: `apps/web/src/lib/liveGrid.test.ts` (new, 9 tests) written first — RED:
    `pnpm exec vitest run src/lib/liveGrid.test.ts` failed (`Failed to resolve import
    "./liveGrid"`, module didn't exist yet). GREEN after adding `liveGrid.ts`, same command, 9/9
    pass. `apps/web/src/routes/Live.test.tsx` (new — Live had no test file before this task)
    written next, also before wiring `Live.tsx`: RED — both new tests failed (restore test:
    `localStorage.getItem` key never populated so no camera tile rendered; persist test: timed
    out waiting for a `localStorage` entry that was never written). GREEN after wiring the
    restore/persist logic into `Live.tsx`, same command, 2/2 pass. One test bug found and fixed
    along the way: the restore test's first assertion (`findByText("Puerta norte")`) became
    ambiguous once restore actually worked, since the name then appears both in the camera list
    and in the restored tile's overlay — changed to `findAllByText(...)` asserting length 2.
  - Full verification: `pnpm --filter web test` PASS (10 files / 30 tests); `pnpm typecheck`
    clean; `make lint` clean; `pnpm --filter web build` clean (same pre-existing chunk-size
    warning).
  - Commit: `d56eb76` (`feat(web): persist the Live grid selection per user in localStorage`, not
    pushed — no remote configured).

- WCP-4: Drag and drop in Live.
  - Dependency: `apps/web/package.json` had no DnD library (checked before starting). Added
    `@dnd-kit/core@6.3.1`, `@dnd-kit/sortable@10.0.0`, `@dnd-kit/utilities@3.2.2` via `pnpm add`
    (run from `apps/web`), per the task's stated preference.
  - `apps/web/src/lib/liveGrid.ts`: added `reorderTiles(tiles, from, to)` (array-move semantics —
    moves one tile, shifting the ones in between; a no-op for `from === to` or an out-of-range
    index) and drag-id helpers `tileDragId(index)`/`cameraDragId(cameraId)` plus
    `resolveDragEnd(activeId, overId)`, a pure, DOM-independent function that turns a dnd-kit
    drag's (active, over) ids into either `{type:"place", index, cameraId}` (a camera dragged
    from the list onto a tile) or `{type:"reorder", from, to}` (a tile dragged onto another
    tile), or `null` for a no-op drop. Kept these framework-free specifically so the drop logic
    is unit-testable without simulating real pointer/keyboard geometry.
  - `apps/web/src/routes/Live.tsx`: wrapped the aside+grid section in `<DndContext sensors={...}
    onDragEnd={handleDragEnd}>` (`PointerSensor` with a 4px activation distance — so a plain
    click/double-click on a tile or its inner buttons isn't mistaken for a drag — and
    `KeyboardSensor` with `sortableKeyboardCoordinates`). The grid's tiles now render through a
    new `GridTile` component using `useSortable({id: tileDragId(index)})` inside a
    `<SortableContext items={shown.map(tileDragId)} strategy={rectSortingStrategy}>`; each
    camera-list leaf now renders through a new `DraggableCamera` component using
    `useDraggable({id: cameraDragId(camera.id)})`. `handleDragEnd` calls the shared
    `resolveDragEnd` and dispatches to `place(cameraId, index)` (generalized to take an explicit
    target index, defaulting to the existing `selected` tile so click-to-place is unchanged) or
    the new `reorder(from, to)`. Existing click/double-click/remove/maximize interactions on
    tiles, and click-to-place on camera list items, are untouched (same handlers, just also
    draggable now). `GridTile` gained an `aria-label={"Cuadro " + (index+1)}` for both testability
    and a clearer accessible name than dnd-kit's default unlabeled `role="button"`.
  - Item 5's "persisted selection must include order" / "saving a view keeps working": both were
    already true structurally — WCP-3's persistence effect serializes the live `tiles` array
    (whatever order it's in), and the save mutation's `body()` already maps `tiles` directly into
    `layout.cells` in order; reordering via drag changes that same array, so no additional code
    was needed for either. Verified by reasoning over `apps/web/src/routes/Live.tsx`'s `body()`
    and the WCP-3 persistence effect, not by a new dedicated test (would duplicate WCP-3's
    persistence test plus the reorder test below).
  - TDD: `apps/web/src/lib/liveGrid.test.ts` extended first — RED: `pnpm exec vitest run
    src/lib/liveGrid.test.ts` failed (`reorderTiles`/`cameraDragId`/`tileDragId` not exported yet,
    10 new tests failing). GREEN after adding the functions, same command, 19/19 pass (9 prior +
    10 new: 5 `reorderTiles`, 5 `resolveDragEnd`).
  - `apps/web/src/routes/Live.test.tsx`: added a keyboard-driven dnd-kit interaction test
    ("reorders two tiles with the keyboard"), written and run against the *already-implemented*
    wiring (process deviation again, same as WCP-1/WCP-3's honest-disclosure pattern — the DOM
    wiring and the test were developed together because getting the exact dnd-kit event sequence
    right needed fast empirical iteration, not a priori RED authorship). Non-vacuity is inherent
    here rather than proven by reverting: the test asserts the *actual post-drop tile content*
    (`"Cuadro 1"` contains "Porton sur", `"Cuadro 2"` contains "Puerta norte" — a real content
    swap), so a broken `resolveDragEnd` wiring or a `reorderTiles` bug would fail it directly; it
    was iteratively developed against the real component (not asserted a priori) precisely
    because jsdom's lack of layout made the exact event sequence non-obvious up front, documented
    below.
    - Feasibility finding (the task explicitly allows skipping this "if feasible"): jsdom never
      lays out elements, so every `getBoundingClientRect()` call returns an all-zero rect;
      dnd-kit's `sortableKeyboardCoordinates` picks a directional (e.g. ArrowRight) neighbor by
      comparing real rects, so with all rects identical it found no neighbor — pickup (Space) and
      drop (Space) both worked (`aria-pressed` toggled correctly), but the arrow-key move was a
      no-op and the tiles never swapped. Fixed by stubbing `Element.prototype.getBoundingClientRect`
      for the two tiles' actual 2x2 grid positions (a technique dnd-kit's own test suite uses) —
      after that, the same Space → ArrowRight → Space sequence produces a real swap. A `0ms`
      `setTimeout` yield is needed between each keydown (dnd-kit measures on the next tick after
      pickup before a coordinate getter has anything to compare); without it the arrow move
      degenerates back to the same "found no neighbor" no-op.
  - Full verification: `pnpm --filter web test` PASS (10 files / 41 tests); `pnpm typecheck`
    clean; `make lint` clean; `pnpm --filter web build` clean (same pre-existing chunk-size
    warning, slightly larger now from the added dnd-kit bundle).
  - Commit: `1d220b6` (`feat(web): drag and drop cameras and tiles in the Live grid`, not pushed
    — no remote configured).

## Next step
All four WCP tasks are done and committed on `feat/web-config-panel`. Nothing pending; delivery
(push/PR/merge) is the user's decision — this branch was never pushed (no remote configured for
this session).
