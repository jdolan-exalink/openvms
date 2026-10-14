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
- [ ] CST-02 — Per-session stream stats: bytes received from the socket, `getVideoPlaybackQuality` (decoded/dropped), video resolution, reconnects; `PlayerSessionManager.sessions()` accessor. Route: delegated writer.
- [ ] CST-03 — `VideoTelemetryService`: 1 Hz aggregator, 60 s ring buffer, totals and per-camera bitrate/fps, main/sub counts, presentation frame-time sampler. Route: delegated writer.
- [ ] CST-04 — `PerformanceClassifier` (Óptimo/Aceptable/Al límite/Saturado) and bottleneck estimator (network / transport / decode / render), pure functions with stated confidence.
- [ ] CST-05 — `DiagnosticsExporter`: redacted JSON + text for "Copiar diagnóstico".
- [ ] CST-06 — UI: "Estación cliente" settings section (capabilities, codecs, bandwidth meter with 60 s chart, health, advanced panel, copy diagnostics).
- [ ] CST-07 — Developer overlay on the Live grid, toggleable at negligible cost.
- [ ] CST-08 — `ClientBenchmarkService` (3–5 s progressive load with real OpenVMS substreams, stops before severe degradation) + IndexedDB cache + startup profile reuse/invalidation.
- [ ] CST-09 — `AdaptiveVideoPolicy`: gradual degradation order, never silently degrade a maximized camera, operator warning; verify no orphan main streams on maximize/unmaximize.
- [ ] CST-10 — Docs (`docs/live-view-architecture.md` / new telemetry doc) and full checks.

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
