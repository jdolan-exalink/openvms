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
- [ ] WCP-2: Confirm/lock in "always Live" as the default after login and for "/" with a
      dedicated Login-flow test; audit for any other place assuming "/" shows the dashboard.
- [ ] WCP-3: Persist the Live grid selection (columns + tile order/camera ids) per user
      (`tenant_id` + user id from `meQuery`) in `localStorage`, wrapped in try/catch, restored on
      load, gracefully dropping cameras the user can no longer see.
- [ ] WCP-4: Drag and drop in Live — drag a camera from the list into a grid slot, and
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
  - Commit: pending (see below).

## Next step
Commit WCP-1, then start WCP-2 (default-route login-flow test + audit).
