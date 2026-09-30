# Live View Platform — Roadmap & Handoff

Last updated: 2026-09-29. Owner decisions are marked **(PO)**.

This is the entry point for anyone (human or agent) continuing the Live View Platform
work. Read it top to bottom before touching code.

## 1. Reference documents

| Document | Purpose |
|---|---|
| `docs/specs/live-view-platform-brief.md` | Product requirements (condensed master prompt, P0–P4) |
| `docs/ARCHITECTURE_AUDIT.md` | Current-state audit with file:line evidence, proposed architecture, open questions |
| `odd/tasks/live-view-platform.md` | ODD feature document for P0: task checklist, progress, evidence |
| Engram topic `odd/live-view-platform/tasks` | Recovery mirror of the feature document |

## 2. Branches and worktrees

| Branch | Where | Content | State |
|---|---|---|---|
| `fix/playback-purity-lint` | `/root/openvms` (base) | UI refactor + stage two S2-1…S2-11 | ~75 commits ahead of `main`, **not pushed**, deployed locally |
| `feat/live-view-sessions` | `/root/openvms` | Live View P0 (LV-1…LV-8) | in progress, not pushed |
| `feat/live-view-gateway` | `/root/openvms-worktrees/lv-7` | LV-7 gateway hardening (`813f791`) | done, **must be merged into `feat/live-view-sessions`**, then remove the worktree |

Merge LV-7: `git merge --no-ff feat/live-view-gateway` from `feat/live-view-sessions`
once its working tree is clean; resolve conflicts in `internal/platform/config/config.go`
and `apps/api/main.go` if any (both branches add config knobs/wiring). Then
`git worktree remove /root/openvms-worktrees/lv-7`.

## 3. Key facts (from the audit)

- Live video is **MSE only**: browser `<video>` → WS `/media/v1/cameras/{id}/live?quality=sub|main`
  on the central API (`internal/media/gateway.go`) → Frigate `/live/mse/api/ws?src=` → Frigate's
  embedded go2rtc. No WebRTC, no edge node, OpenVMS never talks to go2rtc directly.
- Reconnect causes today: `GridTile key={i}` (`apps/web/src/routes/Live.tsx:273`) +
  `reorderTiles` shifting tiles (`apps/web/src/lib/liveGrid.ts:29`) → moving cell 1→8
  reconnects 8 streams; `MsePlayer` effect deps `[cameraId, quality]`
  (`apps/web/src/components/MsePlayer.tsx:181`) → quality change reconnects; expand is an
  in-page focus that unmounts other tiles and switches sub→main; leaving `/live` drops all.
- No snapshot poster (black while connecting) although `snapshot.jpg` exists on the gateway.

## 4. Product decisions (PO, 2026-09-29)

- Scope now: **P0**, then P1. P2–P4 decided later with the audit.
- P0 defaults: expand keeps the **sub** stream (main arrives via P1 seamless switch);
  sessions stay **WARM ~30 s** after leaving Live (configurable); 25/32 grids **pause
  non-visible tiles**; last frame kept **in memory only**, cleared on logout.
- **TDD is OFF** for this feature (explicit PO decision "sin test"). Still run build,
  vet, typecheck, lint and existing test suites before every commit.
- Work in parallel when tasks don't share files; report progress per task.
- Native review (gentle-ai RDD): PO has declined it on every candidate so far; still ask
  per candidate (it is candidate-scoped).
- Commits: Conventional Commits, **no Co-Authored-By / AI attribution** (PO global rule).

## 5. P0 task status

| # | Task | Status | Evidence |
|---|---|---|---|
| LV-1 | Feature flags (`GET /api/v1/features`, `useFeatures()`) + player metrics baseline | ✅ done | `a0f4a62` |
| LV-2 | Player state machine + `PlayerSession` (owns its `<video>`, attach/detach without teardown) | ✅ done | `b3d0624` |
| LV-3 | `PlayerSessionManager` + provider in `Layout` (WARM TTL, LRU, logout clears) | ✅ done | `4e84007` |
| LV-4 | `VideoSurfaceLayer` + `SurfaceSlot` (persistent `<video>` over cells; ResizeObserver, translate3d), behind flag | ✅ done | `1f632e0` |
| LV-5 | Tiles keyed by camera: DnD swap, layout change, expand with 0 reconnects; 25/32 grids + visibility pausing | ✅ done | `656b5d9` |
| LV-6 | Snapshot poster + last frame; states Connecting/Reconnecting/Offline/Unauthorized/Codec/Error + Retry; jittered backoff grouped per server; **stop retrying on `unauthorized`, `forbidden`, `codec_unsupported`** | ✅ done | `7d91c84` |
| LV-7 | Gateway hardening | ✅ done, merged | `813f791`, merge `3a80d80` |
| LV-8 | Rollout (flags on in local compose), `docs/live-view-architecture.md`, deploy | ✅ done | see feature doc |
| LV-9 | LIVE/REC toggle: synchronized recorded grid, zoomable day timeline, calendar, URL state | ✅ done (not browser-verified) | `a3a3b89`, `faea095` |

Dependencies: LV-2 → LV-3 → LV-4 → LV-5. LV-6 can run in parallel with LV-4/LV-5 if it
only touches the session/overlay layer. LV-8 last.

### LV-7 contract (needed by LV-6)

- Error frame before close: `{"type":"error","code":"camera_offline|unauthorized|forbidden|upstream_unreachable|codec_unsupported|server_error","message":"...","value":"<same>"}`.
  Close codes: 1008 unauthorized/forbidden, 1011 others, 1000 normal.
- Env knobs: `LIVE_AUDIT_WINDOW=5m`, `LIVE_REVALIDATE_INTERVAL=30s`, `LIVE_PING_INTERVAL=20s`, `LIVE_PONG_WAIT=60s`.
- Metrics: `openvms_live_sessions_active{quality}`, `openvms_live_sessions_opened_total{quality}`,
  `openvms_live_reconnects_total`, `openvms_live_errors_total{code}`, `openvms_live_proxied_bytes_total{direction}`.
- Known gaps: no end-to-end WS test with fake Frigate; audit dedupe is per API process.
- **PO decision (applied in `d73b5db`)**: unauthenticated requests (no session/token) are rejected
  with the JSON HTTP 401 *before* the WebSocket upgrade. Failures only knowable after resolving
  the camera (forbidden, camera_offline, upstream_unreachable, ...) and later revalidation
  failures keep the error-frame behaviour.

## 6. Acceptance for P0 (from the brief)

- Move camera cell 1 → 8: same session, **0 reconnects**.
- Grid → expand → grid: same session, **0 reconnects**.
- Layout change with the camera still visible: 0 reconnects.
- Camera offline: last snapshot + offline indication + automatic reconnect.
- Never a black tile when a snapshot/last frame exists.
Measure with the LV-1 metrics (`window.__openvmsPlayerMetrics` in dev).

## 7. Later phases (not started; rough estimates)

| Phase | Content | Estimate | Blockers / open questions |
|---|---|---|---|
| P1 | StreamSelectionEngine (+hysteresis), seamless LOW→HIGH handoff (`requestVideoFrameCallback`), prewarm on hover/drag/select, player pool, DecoderBudgetManager, TransportPolicyEngine + WebRTC (via go2rtc) | 1–2 days | WebRTC/edge-direct needs to know whether browsers can reach Frigate/go2rtc hosts directly (audit Q1) |
| P2 | ONVIF discovery wizard, declared vs verified capabilities, CameraHealthService, GOP validation, CameraAdapter interface | 2–3 days | Go ONVIF library choice; WS-Discovery multicast needs host network or site-side probe |
| P3 | Unified event bus (versioned schema, taxonomy), MQTT mapper, `POST /api/v1/events` ingestion, ONVIF events dedupe, correlation engine, unified timeline with LOD buckets | 2–4 days | Reuse existing events/alarms/rules; decide storage for sensor events |
| P4 | Representative frames → VLM → structured JSON → embeddings → hybrid search; AI summaries; explainable results | 3–5 days | VLM provider (cloud vs local) and switching Postgres image to one with pgvector (audit Q6) |

Remaining audit open questions (PO): Q1 browser reachability of Frigate hosts / edge sidecar;
Q6 VLM provider + pgvector; whether `playback.view` maps to existing `recordings.view`.

## 8. Other pending items (outside this feature)

- S2-11 channels: PO to pair WhatsApp (QR), test Telegram, add `WAHA_BASE_URL`/`WAHA_API_KEY`
  to `.env.example` (edits to `.env*` are blocked for agents).
- Debt done (backend, `feat/live-view-sessions`):
  - Live WS auth before upgrade: `d73b5db`.
  - Notification delivery retention (`NOTIFY_DELIVERY_RETENTION`, `NOTIFY_READ_RETENTION`): `84d131b`.
  - SMTP `starttls`/`tls` mode tests: `2b73454`.
  - Server hard delete removes its notifications (new `notifications.server_id`/`camera_id`,
    migration 00019) and strips its ids from rule conditions (rules left with an empty
    camera/server filter are disabled, not widened): `3fbddc5`.
- Debt still open: S2-8 playback drift correction.
- Delivery: push + chained PRs for `fix/playback-purity-lint` and this feature — PO decision.

## 9. How to resume (checklist)

1. `git -C /root/openvms status` and `git log --oneline -15` on `feat/live-view-sessions`.
2. Read this file, then `odd/tasks/live-view-platform.md` (source of truth for checkboxes);
   reconcile with Engram `odd/live-view-platform/tasks` (`mem_search` → `mem_get_observation`).
3. If LV-2/LV-3 files are uncommitted, finish and commit them (check build/tsc/vitest/lint).
4. Merge `feat/live-view-gateway` (section 2).
5. Continue with the next unchecked task; one Conventional Commit per task; update the
   feature document after each task.
6. Deploy: `pg_dumpall` backup to `/root/openvms-backups/`, then `make up`; migrations run on
   API start; verify `/health/ready`.
