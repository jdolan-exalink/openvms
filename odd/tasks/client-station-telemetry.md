# Client Station Telemetry and Videowall Benchmark

## Objective

Measure the client station (hardware, browser video capabilities, OpenVMS bandwidth, decode and render cost), classify videowall health, estimate the bottleneck, benchmark capacity, and adapt the videowall, as specified in `docs/telemetria-client.md`.

## Problem and rationale

The videowall shows ~26 substreams and switches to main on maximize, but nothing tells an operator or support whether a station can hold 16/26/32/48 cameras, how much bandwidth OpenVMS consumes, or whether the limit is network, decode or render. The structures must also fit a future native Windows client.

## Current baseline (mapping 2026-10-14)

- Live transport is MSE over WebSocket (`apps/web/src/lib/live/PlayerSession.ts`); one `HTMLVideoElement` per session; no WebRTC, no WebCodecs, no WebGL grid compositor (WebGL only in fisheye dewarp and image enhancement).
- `lib/live/playerMetrics.ts` tracks state, connect attempts, reconnects and TTFF per `camera:quality`; no bytes, bitrate, fps, dropped frames or resolution yet.
- `PlayerSessionManager` (React context, `PlayerSessionProvider`) is the session registry; it exposes only `stats()` totals, no session iteration.
- Main/sub switching lives in `routes/Live.tsx` (quality overrides, maximize → main, return → sub).
- Layout presets are 1/4/9/16/25/36/64 cells (`lib/presentations.ts`); no 48 preset.
- `/settings` (`routes/Dashboard.tsx`) shows system info and version (`GET /api/v1/system/info`); there is no Diagnostics page.
- No IndexedDB usage; localStorage keys use the `openvms.*` prefix.

## Scope and constraints

- Framework-free services in `apps/web/src/lib/telemetry/`; React only consumes snapshots (`useSyncExternalStore`) at ~1 Hz.
- Types are platform-neutral (`runtime: "browser" | "native"`) so a native client can fill the same `ClientCapabilities` / `VideoPerformanceMetrics`.
- Never claim hardware decoding from codec support; never invent CPU model; label estimates as estimates.
- Telemetry must not cost performance: no per-frame React renders, ring buffers for history, listeners released on dispose.
- Diagnostics export excludes passwords, tokens, credentials and URLs with secrets.
- No heavy benchmark on startup; reuse a cached result for the same browser/version/GPU/OpenVMS version.
- Product terms: the spec says "OpenBMS"; the product in this repo is OpenVMS. UI copy in Spanish, matching the existing app.
- Strict TDD enabled (project/session instructions); runner `cd apps/web && npx vitest run <file>` / `npm test`; typecheck `npm run typecheck`; lint `npm run lint`.
- Planning heuristic ~400 authored changed lines per task (advisory only).

## Tasks

- [x] CST-01 — Telemetry model + `ClientCapabilitiesService`: platform-neutral types; system/screen/CPU/memory/WebGL+GPU detection; MediaCapabilities codec matrix (H.264, HEVC, VP8, VP9, AV1 × CCTV profiles) with supported/smooth/powerEfficient. Route: inline (new isolated module).
- [x] CST-02 — Per-session stream stats: bytes received from the socket, `getVideoPlaybackQuality` (decoded/dropped), video resolution, reconnects; `PlayerSessionManager.sessions()` accessor. Route: delegated writer.
- [x] CST-03 — `VideoTelemetryService`: 1 Hz aggregator, 60 s ring buffer, totals and per-camera bitrate/fps, main/sub counts, presentation frame-time sampler. Route: delegated writer.
- [x] CST-04 — `PerformanceClassifier` (Óptimo/Aceptable/Al límite/Saturado) and bottleneck estimator (network / transport / decode / render), pure functions with stated confidence.
- [x] CST-05 — `DiagnosticsExporter`: redacted JSON + text for "Copiar diagnóstico".
- [ ] CST-06 — UI: "Estación cliente" settings section (capabilities, codecs, bandwidth meter with 60 s chart, health, advanced panel, copy diagnostics).
- [ ] CST-07 — Developer overlay on the Live grid, toggleable at negligible cost.
- [ ] CST-08 — (incl. per-query timeout in `collectClientCapabilities` for the startup light profile) `ClientBenchmarkService` (3–5 s progressive load with real OpenVMS substreams, stops before severe degradation) + IndexedDB cache + startup profile reuse/invalidation.
- [ ] CST-09 — `AdaptiveVideoPolicy`: gradual degradation order, never silently degrade a maximized camera, operator warning; verify no orphan main streams on maximize/unmaximize.
- [ ] CST-10 — Docs (`docs/live-view-architecture.md` / new telemetry doc) and full checks.
- [ ] CST-11 — (user request 2026-10-14, after CST-10) Forensic detail enhancement ("Mejora de Detalle Forense") on paused recorded playback reached from Live; reproduce and fix the black image reported by the user when using it, with a regression test. Own PR after PR-6.

## Acceptance criteria and checks

- Each service has unit tests with injected browser APIs (fake `navigator`, `MediaCapabilities`, WebGL, clock).
- Dashboard updates at ≤1 Hz; disposing the UI releases timers and listeners.
- Exported diagnostics contain no token/password/credential/secret URL (tested).
- Focused tests, `npm test`, `npm run typecheck`, changed-file lint and `git diff --check` pass per task.

## Delivery

- Branch `feat/client-station-telemetry` from `origin/main` (2026-10-14).
- Forecast: well above 400 authored changed lines (~2500–3500); strategy `ask-on-risk`; chain strategy `stacked-to-main` (user choice 2026-10-14).
- Slices (each a PR to main, merged in order, each branch stacked on the previous until merged):
  1. PR-1 `feat/client-station-telemetry` — CST-01 (+ spec and this document).
  2. PR-2 — CST-02 + CST-03 (stream stats + aggregator).
  3. PR-3 — CST-04 + CST-05 (classifier + exporter).
  4. PR-4 — CST-06 + CST-07 (UI + dev overlay).
  5. PR-5 — CST-08 (benchmark + cache).
  6. PR-6 — CST-09 + CST-10 (adaptive policy + docs).

## Progress and evidence

- 2026-10-14: mapping done (delegated explorer); feature document created.
- 2026-10-14: user-requested 48-camera preset added (`8×6`, id `p8x6`, between 6×6 and 8×8); `presentationForCount` now picks it for 37–48 cameras. Route inline; RED (3 failing) → GREEN; presentations + Live + components suites 488/488. Commit `3137894` (rides in PR-1).
- 2026-10-14: CST-01 done (route: delegated writer — types, service and tests are 3 non-trivial files). RED: tests written first, run failed on missing module (import resolution); GREEN: `npx vitest run src/lib/telemetry` 17/17, `npm run typecheck` clean, eslint clean, `git diff --check` clean; parent spot check re-ran tests/typecheck/lint green. ~650 authored lines (tests 276, impl 280, types 90) — above the heuristic because of the codec/GPU/UA test matrix; not compressed.
  - Writer decisions: H.264 `avc1.42E01E` for 360p, `avc1.640028` above; mp4 for h264/hevc/av1, webm for vp8/vp9; profile bitrates 0.6–12 Mbps; `memory.bucketed`; probes webgl2+webgl and loses both contexts; `screens` only from `screen.isExtended` (no permission prompt).
- 2026-10-14: native review of PR-1 slice (`origin/main..26216d2`): assess medium, `slice_budget_reached`; user granted; lens `review-reliability` approved; acknowledged, authority burned (lineage `review-1422ebf89da5c47e`). Reviewed boundary advances to `26216d2`.
  - Advisory R3-legacy-catalog-migration (WARNING): verified not a defect — `loadCatalog` always prepends `DEFAULT_PRESENTATIONS` from code and only reads customs from storage, so old catalogs show 8×6.
  - Advisory R3-unbounded-startup-collect (SUGGESTION): accepted as follow-up — add a per-query timeout to `collectClientCapabilities` before it runs at startup (folded into CST-08).
- 2026-10-14: second review pass (candidate incl. `6a5fe50`, lineage `review-4334db557835c9b7`): user granted; reliability approved; acknowledged, authority burned. Two advisory WARNINGs verified as real and fixed in CST-01 (TDD RED 2 failing → GREEN 18/18, typecheck + eslint clean):
  - Codec strings now declare a level that covers each profile (H.264 High 5.1 for 4K; HEVC L150 for 4K; VP9 2.1/3.1/4.0/5.0; AV1 2.1/3.1/4.0/5.0) instead of a fixed low level that strict platforms could reject.
  - Browser no longer invents `screens: 2` from `screen.isExtended`; it sets `multipleScreens` (boolean). `screens` stays as the exact count a native client can fill.
- 2026-10-14: PR-1 opened as #38. PR-2 branch `feat/client-station-telemetry-2-stream-stats` stacked on PR-1.
- 2026-10-14: CST-02 + CST-03 done (route: delegated writer — 4 new modules + PlayerSession). RED: new test files failed on missing modules; GREEN: `npx vitest run src/lib/telemetry src/lib/live` 142/142, `npm test` 1161/1161, typecheck and eslint clean, `git diff --check` clean; parent spot check re-ran telemetry/live tests, typecheck, lint green. ~860 authored lines (4 small modules + their tests); not compressed.
  - `PlayerSession.streamStats()` (bytes, decoded/dropped frames, resolution, reconnects, stalls, last error) + framework-free `sessionRegistry` (covers private players too); `createVideoTelemetry` 1 Hz deltas, 60-entry `RingBuffer`, rAF `createFrameSampler` (fps, avg/p95 frame time). Not started anywhere yet (PR-4 wires it).
  - Decisions: `activeStreams` = ACTIVE or WARM; totals' frame/stall counts are per-interval deltas; streams without `getVideoPlaybackQuality` report undefined fps; first tick of a stream sets its baseline (bitrate 0).
  - Open note: the global registry relies on every session calling `close()` (true today).
- 2026-10-14: review of `6a5fe50..1899e78` (lineage `review-62d84d1d2078f9b4`): user granted; reliability approved; acknowledged, authority burned; boundary → `1899e78`. Advisory follow-ups fixed with TDD (RED 3 failing → GREEN 144/144, typecheck + eslint clean):
  - Frame counters that become defined later now only set a baseline (no fake fps spike).
  - A throwing telemetry subscriber no longer starves the others or breaks the interval (logged).
  - Stall-unlisten test is now behavioral (dispatch `waiting` after close).
  - Own finding: `droppedFrameRatio` was dropped/(decoded+dropped) but `totalVideoFrames` already includes dropped frames → now dropped/decoded; type docs state the semantics.
  - Accepted risk (documented): the global session registry holds strong refs until `close()`; every session type closes today.
- 2026-10-14: PR-2 opened as #39 (base: PR-1 branch until #38 merges). Review of `origin/main..856f958` (lineage `review-a4a25e1db5e9fd81`): user granted; approved; acknowledged; boundary → `856f958`. Advisory R3-001 (a throwing `streamStats()` aborts the whole tick) accepted as hardening, applied in PR-3.
- 2026-10-14: CST-04 + CST-05 done on `feat/client-station-telemetry-3-classifier` (route: delegated writer — 4 modules + tests). RED: new test files failed on missing modules; 3 classifier expectations then corrected (writer arithmetic, thresholds unchanged). GREEN: `npx vitest run src/lib/telemetry` 104/104, typecheck, eslint, `git diff --check` clean; parent spot check green. ~1130 authored lines (source ~665, tests ~470).
  - `classifyHealth` (10-sample window, sustained ≥30% rule, layers network/transport/decode/render, presentation fps alone caps at `acceptable`), `estimateBottleneck` (always `estimated` unless direct metrics; network only vs an expected bitrate), `createHealthTracker` hysteresis (downgrade after 2, upgrade after 5), `buildDiagnostics`/`formatDiagnosticsText` with recursive redaction and caps, `copyDiagnostics`, Spanish `labels.ts`.
  - Thresholds are untuned defaults (`HEALTH_THRESHOLDS`); the benchmark PR may adjust them.
- 2026-10-14: R3-001 hardening applied: a source whose `streamStats()` throws is skipped (logged) at start and on each tick instead of aborting aggregation for every stream. TDD RED 1 failing → GREEN 105/105, typecheck + eslint clean.
- 2026-10-14: review of `856f958..cc584fe` (PR-3, lineage `review-1b354e409ccbbfaa`): user granted; approved; acknowledged; boundary → `cc584fe`. Advisory findings verified real, fix delegated (writer, TDD): formatter must not throw on sanitized NaN/Infinity (null) values; invalid timestamps must not throw; <3 samples → new `unknown` health state ("Sin datos") and ≥2 sustained samples required; empty window no longer "optimal"; failing telemetry source logs once (recovery already re-baselines, to be proven by test).
  - Fixed (writer, TDD RED 11 failing → GREEN 117/117, typecheck + eslint + `git diff --check` clean; parent spot check green): formatter prints `n/a` for null/non-finite; `isoOrNull` timestamps ("invalid time"); `MIN_HEALTH_SAMPLES = 3`, `MIN_SUSTAINED_SAMPLES = 2`; `HealthState` adds `unknown` ("Sin datos") for thin/empty windows (layers unknown too); tracker passes `unknown` through and adopts the next known state immediately; failing source logs once per failure episode, recovery tick only re-baselines (tested).
