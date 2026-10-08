# Advanced Video Export & Evidence Orchestration System

Implement an enterprise-grade, non-blocking video export and evidence orchestration system for OpenVMS, starting with [IN --- OUT] range selection in Live REC mode, multi-camera asynchronous export jobs, bandwidth-managed extraction to the orchestrator, and real-time progress tracking.

## Architecture & Analysis

1. **Backend (`internal/media/`, `apps/worker/`)**:
   - Current export model (`exports` table) delegates 1:1 camera exports to Frigate and streams downloads directly through `/media/v1/exports/{id}/download` without staging on the orchestrator.
   - Upgrade to an orchestrated multi-camera job architecture: `export_jobs` (parent job, multi-camera package) and `export_job_items` (per-camera extraction task).
   - Export Job Manager in worker coordinates jobs across Frigate instances: triggers remote extraction, pulls files into OpenVMS storage with token-bucket bandwidth limiting (low priority traffic: global & per-server caps), computes SHA-256 hashes on the fly, and bundles manifest/metadata.
2. **Frontend Live REC Mode (`apps/web/src/components/LiveRecDock.tsx`, `DayTimeline.tsx`)**:
   - Add range selection markers (`[ Marcar IN ]` / `[ Marcar OUT ]`) tied to the transport playhead.
   - Render the active selection interval visually on the timeline.
   - Add "Exportar" action opening an intuitive modal: shows selected range/duration, lists cameras from the current live grid (with multi-select / select all), and initiates the job asynchronously without blocking UI or playback.
3. **Bandwidth Management & Non-Transcoding**:
   - Traffic priority: LOW (exports) vs CRITICAL (control/alarms) and HIGH (live view).
   - Token-bucket rate limiter per Frigate server and globally to protect remote WAN/VPN links.
   - Direct stream copying (`-c copy` / Frigate native MP4 export) with zero unnecessary transcoding.
4. **Export Center (`apps/web/src/routes/Exports.tsx`)**:
   - Real-time job monitor: percentage, transferred bytes / total bytes, speed (Mbps), ETA, status chips (`QUEUED`, `PREPARING`, `TRANSFERRING`, `PROCESSING`, `READY`, `FAILED`, `CANCELLED`).
   - Actions: Download, Cancel, Retry, Delete.

## Work units

| ID | Title | Scope |
|---|---|---|
| `EXP-01` | Live REC [IN - OUT] range selection & export trigger | Add IN/OUT markers in `LiveRecDock.tsx`, visual range highlight on `DayTimeline.tsx`, and multi-camera export dialog. |
| `EXP-02` | Database schema & API contracts for export jobs | Migration for `export_jobs` and `export_job_items`, OpenAPI schemas, and REST endpoints for creating/listing/canceling multi-camera export jobs. |
| `EXP-03` | Orchestrator Export Job Manager & Bandwidth Limiter | Worker service with token-bucket rate limiting, orchestrated Frigate extraction, SHA-256 checksums, and metadata packaging into OpenVMS storage. |
| `EXP-04` | Export Center real-time dashboard | Enhanced `Exports.tsx` view with live progress, Mbps speed, ETA, camera badges, and action controls. |

## Progress

- [x] `EXP-01` — Live REC [IN - OUT] range selection & export trigger
- [x] `EXP-02` — Database schema & API contracts for export jobs
- [x] `EXP-03` — Orchestrator Export Job Manager & Bandwidth Limiter
- [x] `EXP-04` — Export Center real-time dashboard

## Verification

- Migration `00036_export_jobs.sql` tested and parsed with Goose: passes.
- Go backend unit tests (`internal/media/...`, `internal/api/...`):
  - `TestCreateExportJobInputValidation`: passes (no cameras, end before start, >24h, future range, name length).
  - `TestExportJobsRoutesRequireAuthentication`: passes (401 verification across all 6 endpoints).
  - `TestToExportJobMapping`: passes (DTO field mapping, items, checksums).
  - `TestBandwidthLimiter_Unlimited`, `TestBandwidthLimiter_ContextCancellation`, `TestThrottledReader`: pass.
  - `TestEnsureExportDir`, `TestExportPackageForensicHashing`: pass.
  - Full Go test suite (`go test ./...`): passes 100%.
- Frontend tests (`apps/web`):
  - `LiveRecDock.test.tsx`: passes (renders IN/OUT, updates markers, shows duration, shortcut keys I/O/Esc).
  - `LiveExportModal.test.tsx`: passes (range card, camera picker, multi-camera selection, protected evidence flag, async POST submission).
  - `Exports.test.tsx`: passes (empty state, transferring job with speed/ETA/progress/protection, accordion cameras with SHA-256 integrity, ready job with ZIP download).
  - Full Vitest suite (`pnpm --filter @openvms/web test`): 125 files, 930 tests pass.
  - Production build (`pnpm --filter @openvms/web build`): passes with zero type or bundle errors.
