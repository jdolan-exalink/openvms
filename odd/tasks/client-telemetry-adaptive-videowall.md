# Client telemetry and adaptive videowall

## Objective

Measure what the client station can sustain and adapt the Live videowall to it: real-time telemetry and a sidebar meter, client capabilities and benchmark, and an adaptive policy that decides how many cameras play live and degrades gradually under load. Source spec: `docs/telemetria-client.md` (user-owned, untracked; read-only reference).

## Problem

Dragging a 63-camera server into Live shows only ~26 cameras playing; the rest show "En pausa". No code cap produces 26 (mapping 2026-10-13): `PlayerSessionManager` caps are soft and never evict in-use sessions, the Go relay has no stream cap, and "En pausa" only comes from SUSPENDED (mainly off-screen IntersectionObserver suspension, `MsePlayer.tsx:108`). The client has no telemetry (no bytes, FPS, dropped frames, hardware data), so the cause cannot be determined.

## Why

Operators need to know whether a station can hold a layout, where the bottleneck is (network, transport, decode, render), and the videowall must adapt instead of failing silently.

## Scope and constraints

- Architecture (spec §16, §19): services independent of React (`lib/telemetry/`); UI only consumes snapshots; ring buffers; UI refresh ~1 Hz; no per-frame React renders; release listeners on unmount; telemetry must not become a performance problem.
- Never invent hardware data the browser does not expose (CPU model, CPU/GPU utilization). Show logical processors, reported memory, WebGL renderer when available; label estimates as estimates.
- Count only OpenBMS video traffic (bytes received on live sessions), not NIC traffic.
- UI copy follows the existing Spanish UI; code, identifiers and comments in English.
- Effective TDD mode: enabled by project instructions; runner `pnpm test` (vitest) in `apps/web`.
- Delivery: one branch and PR per stage, each from `main`. No push/PR/merge without user authorization; merges are done by the user.

## Stages and tasks

### Stage 1 — telemetry and sidebar meter (branch `feat/live-telemetry-meter`)

- [x] VT-01 — Maximize promotes to main immediately (remove the 5 s dwell at `Live.tsx:~758`); keep gap-free handoff and manual-override semantics; update dependent tests.
- [ ] VT-02 — `VideoTelemetryService`: per-session received bytes (WebSocket media messages), 1 Hz sampling of presented/dropped frames (`getVideoPlaybackQuality`), session state counts (playing, connecting, paused/suspended, error) and main/sub counts, 60 s ring buffer of totals; plus static `ClientCapabilities` basics (logical processors, reported memory, WebGL/WebGL2 renderer/vendor when exposed).
- [ ] VT-03 — Compact sidebar footer meter in `LiveExplorer`: live Mbps with 60 s histogram, FPS presented, dropped %, stream counts by state and main/sub, CPU threads and GPU name, health class (Óptimo / Aceptable / Al límite / Saturado) from a first `PerformanceClassifier`. Modern, compact, theme-aware.

### Stage 2 — diagnose the ~26-camera limit

- [ ] VT-04 — Using the Stage 1 meter with the 63-camera server, identify which state the non-playing cameras are in and the mechanism (off-screen suspension, backoff queue, browser/decoder limit); fix the real cause.

### Stage 3 — client station page and benchmark

- [ ] VT-05 — "Estación cliente" diagnostics route: system, CPU, memory, GPU/WebGL, codec capabilities via MediaCapabilities (supported / smooth / powerEfficient per CCTV profile).
- [ ] VT-06 — `ClientBenchmarkService`: 3–5 s progressive benchmark with real pipeline samples, stops before severe degradation; capacity result per layout; IndexedDB cache with invalidation keys; `clientPerformanceScore`.
- [ ] VT-07 — "Copiar diagnóstico" (`DiagnosticsExporter`, no secrets) and optional developer overlay.

### Stage 4 — adaptive videowall

- [ ] VT-08 — `AdaptiveVideoPolicy`: live-camera budget per client from benchmark + telemetry; prefer substream in large layouts; release main immediately on grid return.
- [ ] VT-09 — Gradual degradation under saturation (spec §11) and bottleneck estimation (spec §12), never silently degrading a maximized camera.

## Acceptance criteria

- Each stage: `pnpm lint` 0 problems, `pnpm typecheck`, `pnpm test`, `pnpm build` pass; CI green.
- Stage 1: meter updates at ~1 Hz without re-rendering tiles; numbers match the bytes actually received by sessions in tests.

## Route

- Delegated direct per stage (multi-file, non-trivial) → one bounded writer per stage.

## Progress and evidence

- 2026-10-13: document created; mapping findings recorded in Problem; branch `feat/live-telemetry-meter` from `main` (`75a688f`).
- VT-01 (delegated writer): removed the 5 s dwell and `maximizeTimerRef` entirely (no setTimeout kept); promotion now runs synchronously in the focus effect via the existing override handoff. RED: updated "promotes to main immediately" + 3 dependent tests failed on old code (4 failed / 63 passed). GREEN: `pnpm test src/routes/Live.test.tsx` 67/67; `pnpm lint` 0 problems; `pnpm typecheck` ok. Persistent grid->expand->grid test now expects exactly 1 extra main socket (3 total) with 2 sub sockets and 0 sub reconnects.

Repository-relative file: `odd/tasks/client-telemetry-adaptive-videowall.md`
