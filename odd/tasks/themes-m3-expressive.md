# Themes and Material 3 Expressive redesign

## Objective
Three themes (Ristretto, Dracula, Light), one design language inspired by Material 3 Expressive across every screen, a normalized minimalist icon system, PWA installability and a mobile layout with bottom navigation.

## Problem and why
The UI ships two themes toggled by a `.light` class, mixes FontAwesome (navigation, 19 files) with lucide-react (51 files), has one shared primitives file plus 162 raw buttons, and below `md` keeps a fixed 64px rail plus a drawer. There is no manifest or service worker, so it cannot be installed on a phone.

## Design source
Canvas https://claude.ai/artifact/VWVEfmkxMcM6Nd26kDwXAc (tokens, icons, desktop, mobile, PWA install). Theme values in this document are the implementation source of truth.

## Scope and constraints
- Authorized by the user on 2026-10-04 ("cuando termines arranca con el desarrollo").
- Branch `feat/themes-m3`, created from `feat/maps` with the unrelated provisioning work still uncommitted; work-unit commits stage only this feature's paths.
- Tokens: `data-theme="ristretto|dracula|light"` on `<html>`, M3 role variables (`--md-*`), existing Tailwind utilities (`bg-bg`, `text-ink`, `bg-surface`...) remain as aliases so routes migrate incrementally.
- Default theme: stored choice, else `prefers-color-scheme` (light → Light, dark → Ristretto). Legacy `openvms.theme=light` maps to Light, anything else to Ristretto.
- Fonts self-hosted (Caddy CSP `font-src 'self'`): Manrope for UI, IBM Plex Mono kept for data.
- Icons: lucide-react behind one `<Icon>` wrapper (sizes 16/20/24, stroke 1.75, active stroke 2); FontAwesome removed at the end.
- PWA: manifest, maskable icons, service worker that never caches `/api`, `/media`, `/ws`; Caddy must serve `sw.js` and the manifest with `no-cache` instead of the SPA fallback.
- Generated technical artifacts in English; UI copy goes through i18n catalogs es/en/pt.
- TDD: strict, enabled by session configuration. Runner: `cd apps/web && pnpm exec vitest run <files>`; typecheck `pnpm typecheck`.
- Planning heuristic about 400 authored changed lines per task (advisory only).

## Delivery
Forecast: about 3500–5000 authored changed lines (23 routes, 11.4k lines). Over budget: strategy `ask-on-risk`, chain strategy `feature-branch-chain` (user confirmed 2026-10-04). Slices: PR1 T1–T2 (themes), PR2 T3–T4 (icons, primitives), PR3 T5–T6 (shell, PWA), PR4 T7–T8, PR5 T9–T10, PR6 T11. Push, PR creation and merge remain the user's decisions.

## Tasks
- [x] T1 Theme tokens and runtime: three `data-theme` palettes with M3 roles, shape/type/motion tokens, Manrope, `theme.ts` (read/apply/persist, legacy migration, system default), replace the class toggle in `main.tsx` and `AccountMenu.tsx`, `MapStyleController` reads dark/light from the active theme and restyles on change. Route: delegated (multiple non-trivial files).
- [x] T2 Theme selector: segmented control in the account menu and Account page, i18n es/en/pt.
- [x] T3 Icon system: `<Icon>` wrapper (16/20/24/32, stroke 1.75, 1.5 at 32, label → role img), nav and every FontAwesome file outside the Maps boundary migrated to lucide.
- [ ] T3b Finish FontAwesome removal once the uncommitted Maps/LiveExplorer work is committed: LiveExplorer and MapSocSidebar to `<Icon>`, `ContextMenu.MenuItem.icon` to `LucideIcon`, `sprite.ts` to lucide path data, delete `inventoryIcons.ts` and the FA config in `main.tsx`, drop the three FA dependencies, update `Live.test.tsx:544-545` (`data-icon='cloud'`).
- [x] T4 M3 primitives: Button (filled, tonal, outlined, text, icon), Chip, Card, Switch, StatusBadge pill, inputs, Modal/ConfirmDialog shapes, motion utilities.
- [x] T5 App shell: navigation rail with pill indicator, mobile bottom navigation (4 primary + "More" sheet) below `md`, top app bar; drop the fixed mobile rail.
- [x] T6 PWA: manifest with per-theme `theme-color`, maskable icons, service worker, install prompt (beforeinstallprompt + iOS hint), Caddy headers and CSP.
- [ ] T7 Migrate operation screens: Live, LiveExplorer, Playback, Events, Plates, Dashboard.
- [ ] T8 Migrate maps surfaces: MapShell, panels, canvas palette per theme.
- [ ] T9 Migrate inventory screens: Servers, Cameras, Sites, CameraGroups, FrigateCameraConfig, Exports.
- [ ] T10 Migrate settings and auth: Users, Groups, Permissions, Rules, Channels, Alarms, Audit, Branding, Account, Notifications, Login.
- [ ] T11 Hardcoded colors cleanup (DayTimeline, ZoneCanvas, ArPlate, format.ts, sprites) and final visual pass.

## Acceptance criteria
- Switching theme updates every surface without reload, persists, and maps follow it.
- No raw hex colors in components outside token files and documented canvas fallbacks.
- Below 768px: bottom navigation, no horizontal page scroll, touch targets at least 44px.
- Lighthouse installability passes: manifest, icons, service worker; API and media never served from the SW cache.
- Existing vitest suite and typecheck pass after every task.

## Progress and evidence
- T1 (delegated writer; trigger: 2+ non-trivial files). RED: `theme.test.ts` failed to load (module missing); `MapStyleController.test.ts` data-theme isDark row and "restyles between two dark themes" (expected 1 call, got 0). GREEN: theme + MapStyleController 22/22; typecheck clean; `pnpm build` OK (existing >500 kB chunk warning). Full suite 661/663: both failures in `MapShell.test.tsx` (live hover 700ms, investigation navigation) reproduce with T1 files stashed, so they come from uncommitted Maps work, not T1. Shape tokens named `--radius-m3-*` because Tailwind `--radius-xs..2xl` would resize existing `rounded-*`. Map restyles through the existing MutationObserver, now on `data-theme`, keyed by theme id. Commit `ada0302`. Review: assessed medium (507 lines, slice budget reached); user declined review for this candidate. Note: committed-only preflight needed `--untracked-scope=exclude --expected-untracked-inventory=<digest>` because the provisioning feature's untracked files are present.

- T2 (delegated writer; trigger: 2+ non-trivial files). RED: `ThemePicker.test.tsx` failed to resolve `./ThemePicker`; Account test could not find heading "Apariencia". GREEN: ThemePicker/Account/Layout/i18n 19/19 (parent re-ran); full suite 666/668 with only the two known pre-existing MapShell failures; typecheck clean. ThemePicker is a radiogroup with roving tabindex, arrows/Home/End, 44px targets; swatch hexes live in one `SWATCHES` constant. Legacy keys `common.dark`, `common.lightMode`, `common.darkMode` are now unused; prune in T11. Commit `119c512`. Review assess: medium, 212 lines, `under_budget` (pending in slice with base `ada0302`).
- T3 constraint: FontAwesome files with uncommitted Maps work (LiveExplorer, MapShell, FloorMap, MapSocSidebar) stay untouched; FontAwesome removal is deferred until that work is committed.

- T3 (delegated writer; trigger: 2+ non-trivial files). RED: `Icon.test.tsx` and `nav.test.ts` failed to resolve `./Icon` (no separate RED recorded for the "nav has no FontAwesome" assertion: a documented strict-TDD gap). GREEN: Icon/nav/Layout/AppShell/SettingsLayout 47/47; parent re-ran Icon/nav/Layout 40/40; full suite 699/701 with only the two known MapShell failures; typecheck clean. Nav mapping avoids duplicates (events Zap, rules GitBranch). Active rail item uses stroke 2; SettingsLayout stays 1.75 (no active flag in scope). Inline icons that were FA 1em now use 16px: check alignment in T11. Commit `1779985`. Slice T2+T3 assessed medium, 507 lines, `slice_budget_reached`; user declined review for this candidate.

- T4 (delegated writer; trigger: 2+ non-trivial files). RED: `ui.test.tsx` 12 failed / 11 passed (missing IconButton, Chip, Card, Switch, new Button variants, StatusBadge tone). GREEN: ui + Modal 26/26 (parent re-ran); full suite 723/725 with only the two known MapShell failures; typecheck and build OK. Legacy Button variants map primary→filled, secondary→tonal. Visible impact: buttons grow to 44px pills and inputs to 48px across all 44 importers; use `size="sm"` in dense tables during T7–T10. Follow-ups: i18n for hard-coded "Cerrar"/"Cancelar" in Modal and ConfirmDialog; visual contrast check of the danger button in each theme. Commit `ca9cedf`; assessed medium, 404 lines, `slice_budget_reached`; user declined review for this candidate.

- T5 (delegated writer; trigger: 2+ non-trivial files). RED: `nav.test.ts` 4 failed (helpers missing); Layout/AppShell 8 failed (bottomNav slot, raised sheet, `hidden md:flex` rail, bottom bar and More sheet). GREEN: Layout/AppShell/nav 46/46 (parent re-ran); writer ran those plus Live 77/77; full suite 731/733 with only the two known MapShell failures; typecheck and build OK. Bottom bar picks /live, /maps, /events, /servers when permitted, then other operational items; settings always under More. More sheet: dialog, focus trap, Escape returns focus. fitViewport routes keep zero margins. Not verified in a real browser yet (360px scroll, safe areas): do it during T11 or the first local deploy. `common.openMenu` now unused. Commit `1df3aad`; assessed medium, 569 lines, `slice_budget_reached`; user declined review for this candidate.

- T6 (delegated writer; trigger: 2+ non-trivial files). RED: pwa.config, InstallSheet, PwaUpdatePrompt tests failed to resolve their modules; AccountMenu 2 failed (install entry). `useInstallPrompt` had no natural RED (hook written first; RED reproduced by moving the file): documented strict-TDD gap. GREEN: parent re-ran PWA set 26/26; full suite 757/759 with only the two known MapShell failures; typecheck and build OK; dist has sw.js, manifest.webmanifest and icons; Caddy validate OK and a real Caddy run returned no-cache for sw/manifest/SPA, immutable for /assets, manifest content type and the new CSP. SW: registerType prompt, precache build assets only (97 entries, 4.3 MiB first load), no runtime caching, navigate fallback denylist for /api /media /ws /health /openapi.json /docs. Real-device checks pending: Android install and maskable crop, iOS sheet and status bar, update snackbar after two deploys, live video with SW active, Lighthouse. Open decision: brand ring #fff3ee on #f38d70 has low contrast (on-primary would read better). Commit `caf2f03`; assessed medium, 4329 lines (mostly lockfile), `slice_budget_reached`; user declined review for this candidate.

## Next step
T7 operation screens.
