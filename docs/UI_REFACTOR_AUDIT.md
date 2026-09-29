# OpenVMS UI/UX Refactor: Current-State Audit

The frontend already has a working React application, protected routes, operational video workflows, reusable query/API clients, and basic visual primitives. The refactor can build on those foundations, but several requested areas—global realtime updates, device/rule workflows, a video wall, and direct/WebRTC media—are not implemented in the inspected frontend. Treat this as a baseline audit, not authorization to implement those capabilities.

## Executive summary

| Area | Current implementation | Refactor implication |
|---|---|---|
| App shell | Persistent desktop sidebar, route content area, nested settings navigation | Recompose around the requested `Primary Nav Rail`, `Context Sidebar`, and `Main Workspace`; there is no shared top bar or mobile nav today. |
| State and API | TanStack Query polling over typed same-origin OpenAPI client | Preserve query/API contracts and authorization; add new transport behavior only with backend support. |
| Live video | DnD camera grid; each mounted tile opens a gateway WebSocket using MSE | Keep grid, camera discovery, and server-mediated access intact; a visual refactor does not supply WebRTC, stream multiplexing, or connection limits. |
| Playback/events | HLS recording playback, event timeline markers, searchable event list and detail actions | Reuse current routes and gateway endpoints; current event updates are polled, not pushed to the UI. |
| Design system | Small shared component set and CSS variables, with Tailwind classes in feature screens | Centralize tokens and shell primitives before broad screen restyling; avoid assuming a larger component library exists. |

## Stack, routes, and state

- **Stack:** `apps/web/package.json` lists React 19, TypeScript, Vite, TanStack Router/Query, Tailwind CSS v4, Lucide icons, dnd-kit, `hls.js`, and `openapi-fetch`. `apps/web/src/api/schema.d.ts` is generated from `packages/api-contract/openapi.yaml`.
- **Bootstrap:** `apps/web/src/main.tsx` installs the QueryClient and router, enables StrictMode, restores the dark/light preference, retries failed queries once, and disables refetch-on-window-focus by default.
- **Route tree:** `apps/web/src/router.tsx` protects the application through `/api/v1/me`; `/` redirects to `/live`. Operational routes are `/live`, `/events`, `/plates`, `/playback`, and `/exports`. A pathless Settings layout wraps `/settings`, `/sites`, `/servers`, `/cameras`, `/users`, `/groups`, `/permissions`, `/audit`, `/branding`, and `/account`.
- **State:** `apps/web/src/api/queries.ts` owns typed query options and filters. Cameras and servers refresh every 15 seconds, events every 15 seconds, health every 10 seconds, and exports every 3 or 30 seconds depending on active jobs. No frontend `EventSource`, SSE client, or application-level `/ws` client was found.
- **Auth:** `apps/web/src/api/client.ts` uses same-origin requests, adds the CSRF marker and optional bearer token, and sends a 401 to login. `apps/web/src/api/auth.ts` keeps a pasted API token in session storage or memory. Browser sessions use the server's HttpOnly cookie (`internal/api/auth.go`).

## Shell and reusable UI

`apps/web/src/components/Layout.tsx` renders the global sidebar, user/logout controls, theme toggle, and route outlet. The sidebar is hidden below the `md` breakpoint; no alternate mobile navigation is present there. `apps/web/src/components/SettingsLayout.tsx` provides a settings sub-navigation. `apps/web/src/components/nav.ts` defines both navigation groups and their Lucide icons. Neither a shared TopBar nor a reusable context-sidebar shell currently exists.

The requested three-level shell maps cleanly as a composition target:

1. **Primary Nav Rail** — global route families; current nearest implementation is `Layout` + `nav.ts`.
2. **Context Sidebar** — context-specific navigation/tools; current examples are `SettingsLayout` navigation and Live's camera tree, but they are separate implementations rather than one shared shell region.
3. **Main Workspace** — active route content, currently rendered through `<Outlet />` in `Layout`.

`apps/web/src/index.css` imports Tailwind and defines dark-default/light CSS tokens plus IBM Plex Sans/Mono. Most layout and component styling is expressed as utility classes in page files. `apps/web/src/components/ui.tsx` provides `PageHeader`, `StatusBadge`, `ErrorNote`, `Button`, `Field`, `TextInput`, `Select`, `Empty`, `Table`, and `Th`; `apps/web/src/lib/cn.ts` composes class names. `apps/web/src/components/Modal.tsx` and `PlateDetailModal.tsx` are additional reusable overlay patterns. `lucide-react` is the established icon source.

## Video, events, and saved-view integration

| Workflow | Current behavior and reusable path | Backend dependency / constraint |
|---|---|---|
| Live view | `apps/web/src/routes/Live.tsx` groups enabled visible cameras by site/server, supports 1×1 through 4×4 grids, drag/drop placement and reorder, focus, and quality selection. `apps/web/src/lib/liveGrid.ts` contains pure grid operations and per-user/tenant localStorage persistence. | Cameras/sites/servers come from API queries. `internal/frigate/v017.go` discovers go2rtc stream names; `internal/media/gateway.go` authorizes and proxies live streams. |
| Live tile | `apps/web/src/components/MsePlayer.tsx` opens one WebSocket per mounted tile at `/media/v1/cameras/{id}/live?quality=...`, performs go2rtc MSE setup, and retries after drops. | Stream is served through the central authenticated media gateway, not directly from an RTSP URL. No client WebRTC path or single session socket was found. |
| Playback | `apps/web/src/routes/Playback.tsx` queries one camera's recordings and event index for a selected day, shows a 24-hour timeline, and exports clips when permitted. `apps/web/src/components/HlsPlayer.tsx` plays an hour-sized range through hls.js or native HLS. | `/media/v1/cameras/{id}/vod/...` proxies Frigate VOD; `internal/media/gateway.go` enforces `recordings.view`. No live MSE-to-HLS fallback is implemented in `MsePlayer`. |
| Events | `apps/web/src/routes/Events.tsx` filters the indexed event list by site, camera/group, label, zone, sub-label, severity, plate, time, review, snapshot, and preview; it paginates and provides detail/review/playback/export actions. | `apps/web/src/api/queries.ts` polls `/api/v1/events`; `internal/events/syncer.go` syncs Frigate review items into the VMS index. UI does not subscribe to event pushes. |
| Saved views | Live selection is stored locally in `openvms.live.selection.v1` per tenant/user; inaccessible cameras are dropped during restore. Named views are loaded and mutated through `/api/v1/views`, including shared/editable behavior. | The API contract documents private/shared view operations. Preserve the distinction between local last-used layout and server-persisted named views. |

`internal/api/router.go` mounts the media gateway under `/media/v1`; `internal/media/gateway.go` documents MSE live, HLS VOD, snapshots, LPR media, and export download routes. `internal/api/auth.go` authenticates both API and media requests. `internal/authz/catalog.go` defines granular permissions including live audio/talk/PTZ, but their existence in the catalog does not mean corresponding UI controls or backend interactions are implemented.

## Present versus absent capabilities

**Present in the inspected frontend:** live multi-camera grid; camera search/tree; event search/filtering and detail; recorded playback; clip exports; local grid persistence; named private/shared views; inventory and administration routes; session/token login; permission-aware visibility; polling-based status/event lists.

**Not found as implemented UI behavior:** dedicated Devices workflow beyond Cameras/Servers/Sites inventory; Rules screen; global search route; alarm/notification inbox; camera settings drawer; shared TopBar; route-level context-sidebar framework; video-wall mode or automatic rotation; PTZ/audio/talk controls; frontend application event socket/SSE; WebRTC playback; browser-to-edge signed media session; stream-count limiting or automatic suspension of invisible tile streams.

The PRD (`docs/PRD.md`) describes planned capabilities that are not evidence of implementation: a UI `/ws` event feed, signed media sessions and optional direct edge media (sections 40–41, 76), WebRTC → MSE → HLS preference (section 42), Video Wall (section 84), and stream limits/suspension (section 86). Likewise, `nav.ts` currently labels Cases and Notifications as future `M8` placeholders. Keep unavailable capabilities hidden or clearly non-interactive until the route, contract, and behavior exist; do not turn a placeholder into an apparent working feature.

## Existing test foothold

Frontend tests use Vitest and Testing Library (`apps/web/src/test-setup.ts`, `test-utils.tsx`). Existing coverage includes `router.test.tsx`; route tests for Live, Events, Playback, Cameras, Dashboard, Login, Branding, and Plates; SettingsLayout, Modal, PlateDetailModal, and MsePlayer component tests; and `lib/format.test.ts` plus `lib/liveGrid.test.ts`. This gives the refactor focused seams for shell/routes, stream lifecycle, playback, and grid behavior. Other routes/components do not all have corresponding tests. No tests were run for this audit.

## Refactor boundaries and invariants

- **Refactor candidates:** `Layout.tsx`, `SettingsLayout.tsx`, `nav.ts`, and `index.css` for shell/tokens; feature screens can then adopt shared primitives. Keep route URLs stable unless separately authorized—some screens intentionally retain flat paths under a pathless Settings layout.
- **Preserve operational screens:** `Live.tsx`, `Playback.tsx`, and `Events.tsx` are implemented workflows with direct media, query, mutation, and permission dependencies. Recompose them; do not delete or replace their capabilities based solely on a new visual model.
- **Preserve auth boundaries:** `lib/perm.ts` controls presentation only; the API and media gateway remain authoritative. Retain login/session behavior, CSRF on cookie-authenticated writes, bearer-token support, and backend permission checks.
- **Preserve data semantics:** keep local last-used Live grid separate from named server views; maintain per-user/tenant scoping, stale-camera filtering, route search state for playback, and event query filters/pagination.
- **Preserve stream semantics:** substream is used for grid tiles, main quality for focused/1×1 view. Each currently mounted tile owns a socket. Changes to stream transport, multiplexing, concurrency, or topologies require corresponding backend support and focused performance/behavior validation.
- **Do not overstate settings:** `Cameras.tsx` is a filtered inventory table, not a camera editing/settings drawer. `Dashboard.tsx` shows inventory and control-plane health/version; it is not a system configuration console. No Rules or standalone Devices route was found in `router.tsx`.

## Backend dependencies and scope boundary

The current frontend depends on the typed API contract, same-origin API proxy, authorization service, event index/syncer, camera/server inventory, Frigate adapters, and central media gateway. Current contracts cover the route behaviors used by the existing pages; adding global search, rules, notification delivery, device configuration, signed media sessions, direct edge access, WebRTC, or a WebSocket event feed requires confirming or extending backend contracts and authorization first. A CSS/shell phase can remain frontend-only; phases that introduce those missing capabilities cannot be represented as frontend-only refactors.

## Phased migration map

This sequence follows the requested 14 phases. It identifies current assets and gaps; it does not start any phase.

| # | Requested phase | Grounded starting point and boundary |
|---:|---|---|
| 1 | Design tokens + AppShell | Consolidate `index.css` tokens and `Layout.tsx`; establish the three shell regions without changing route/data behavior. |
| 2 | Primary Navigation + TopBar | Reuse `nav.ts`; add no working destinations until routes exist. There is no current shared TopBar or mobile navigation. |
| 3 | Video View sidebar | Reuse/refactor Live's `CameraTree`; preserve site/server grouping, visible-camera filtering, search, and status. |
| 4 | Video Grid + VideoTile | Refactor Live's grid and `MsePlayer`; retain DnD, local layout behavior, quality selection, and authenticated per-camera media access. Stream limits are not currently implemented. |
| 5 | Timeline + playback | Reuse Playback's timeline and `HlsPlayer`; preserve camera/day/instant URL search state, recording markers, event markers, and clip export authorization. |
| 6 | Saved views + layouts | Keep local selection (`lib/liveGrid.ts`) distinct from API-backed named/shared views; preserve ownership and permission rules. |
| 7 | Search | Current evidence is filter-based event/plate search; no global-search route was found. Keep global search hidden until its intended scope and API/index contract are confirmed. |
| 8 | Events + Alarms | Refactor the existing Events list/detail. No push-driven alarm inbox or notification UI was found; do not imply realtime alarms from the current polling list. |
| 9 | Devices | Existing nearest surfaces are Sites, Servers, and Cameras inventory. Confirm what “device” means before adding or renaming a domain surface. |
| 10 | Camera settings drawer | `Cameras.tsx` is read-only inventory in the inspected UI; no drawer implementation was found. Requires confirmed editable fields and API permissions/contracts. |
| 11 | Rules | No Rules route or implementation was found. Keep absent from working navigation until the domain/API and permissions are defined. |
| 12 | Users/RBAC | Reuse Users, Groups, Permissions, `lib/perm.ts`, and server enforcement; preserve scope/effect semantics and keep frontend gating non-authoritative. |
| 13 | System | Reuse Dashboard's health, inventory counts, and version information for an operational system view; do not present it as configuration management. |
| 14 | Polish, performance, and responsive behavior | Validate keyboard/accessibility behavior, responsive navigation, image loading, query cancellation, and stream lifecycle. Current query polling and per-tile sockets are the baseline, not proof of virtualization or connection throttling. |

## Acceptance checks for later implementation

- [ ] The three shell regions have clear ownership and work at narrow and wide viewport sizes; all implemented routes remain reachable.
- [ ] Existing route paths, API payloads, query semantics, saved-view scope, and permission-gated actions remain behaviorally equivalent unless a separate requirement authorizes a change.
- [ ] Missing features (global search, rules, alarm inbox, device editing, camera drawer, PTZ, video wall, direct/WebRTC media) are not exposed as working controls without implemented API and authorization support.
- [ ] Live grid and playback keep their current stream-quality and media-access behavior; any transport/concurrency change is verified against the media gateway contract.
- [ ] Focused frontend tests cover changed route and interaction behavior; future source changes use configured Strict TDD (RED → GREEN → REFACTOR).

## Open product decisions

1. Does **Devices** denote the existing camera/server/site inventory, or a separate device domain?
2. Does **Search** mean the existing event/plate filters or a new global indexed search?
3. Are missing capabilities (realtime alarms, Rules/device editing, video wall, PTZ, direct/WebRTC media) in this refactor's implementation scope, or should they remain explicitly unavailable while the UI is redesigned?

These decisions matter before the affected phases; they do not block the shell and visual audit itself.
