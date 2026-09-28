# Plate detail watermark

## Objective
In the Plates page, clicking a plate read opens a closable detail modal showing the
detection photo at maximum quality and a playable clip, with download buttons for both.
Every download is watermarked (burned in) with the detection's date/time (with timezone
offset) and the configured owner name/logo; the on-screen view shows the same watermark as
a CSS overlay. Owner branding (name/logo) is configurable per tenant from Configuración.

## Problem and rationale
`apps/web/src/routes/Plates.tsx` only shows a hover preview of the plate snapshot; there is
no detail view, no clip playback, no downloads, and no way to prove who captured a
detection and when once a file leaves the system. `internal/media/gateway.go` already has
the authz/relay pattern for tenant-scoped Frigate media (see `lprReadSnapshot`), which this
feature extends.

## Scope and constraints
- Contract first: every API change starts in `packages/api-contract/openapi.yaml`, then
  `make generate`.
- Never leak media or branding across tenants or to actors lacking the relevant permission.
- Do not contact any real Frigate/remote host; `internal/frigatemock` only.
- Do not push/merge. One work-unit commit per task, Conventional Commits, no AI attribution.
- `make up` only at the very end, never concurrently with integration tests (4.5 GB RAM host).

## Tasks
- [x] PDW-1: Owner branding settings (migration, contract, service, audit, Configuración page). Route: delegated direct (writer: this session).
- [x] PDW-2: Plate detail modal — full-quality photo + clip playback via gateway (Range) + CSS watermark overlay. Route: delegated direct (writer: this session).
- [x] PDW-3: Watermarked photo download (Go burn-in, embedded font, logo composited). Route: delegated direct (writer: this session).
- [x] PDW-4: Clip watermark job in the worker (ffmpeg image, job table/state, object store, status + download endpoints). Route: delegated direct (writer: this session).
- [x] PDW-5: Modal download UI for photo and clip job (progress, errors), audit labels. Route: delegated direct (writer: this session).
- [x] PDW-6: Fix independent-verification findings (ffmpeg drawtext injection/escaping,
  silent audio loss, stuck jobs, logo decompression bomb, JPEG quality nit), verified against
  real ffmpeg inside the running worker container. Route: delegated direct (writer: this
  session).
- [x] PDW-7: Configurable per-tenant watermark timezone (migration, contract, service,
  worker, web UI, Go+web tests incl. DST), tzdata embedded in api/worker. Route: delegated
  direct (writer: this session).
- [x] PDW-8: PlateDetailModal tabs (Foto/Clip, accessible tablist, lazy-mounted clip video)
  and confirmation that the watermark timestamp is correct (seen_at-sourced, local time,
  overlay matches burned-in output exactly). Route: delegated direct (writer: this session).

## Verification mode
- Strict TDD: enabled (source: global user config `Strict TDD Mode: enabled`). RED → GREEN → REFACTOR with observed evidence.
- Runners: `go test ./...`; `go test -tags integration ./internal/...` (needs Docker); `pnpm --filter web test`; `pnpm typecheck`; `make lint`; `make generate` + clean `git diff` after commit.

## Acceptance criteria
- Modal: Esc/close button/focus trap/aria-modal; photo at Frigate's highest quality; clip
  playable and seekable (Range support); on-screen CSS watermark overlay (no re-encode).
- Downloads (photo, clip) always carry a burned-in watermark: date/time with timezone
  offset + owner name/logo when configured.
- Authorization: view photo needs `lpr.view` + `snapshots.view`; view clip needs `lpr.view`
  + `recordings.view`; download photo additionally needs `snapshots.download`; clip job/
  download additionally needs `exports.create`/`exports.download`. Cross-tenant or missing →
  404; denied → 403 (audited via the existing central `auditDenied`).
  Branding: read needs only tenant membership; write needs `tenant.manage` (closest existing
  tenant-administration permission — no dedicated branding permission exists).
- Clip watermark job: queued → running → done/failed, polled, then downloaded, mirroring
  the existing Exports UX; worker image gains ffmpeg without making API/other images non-distroless.
- All runners above pass; `make up` succeeds and `docker compose exec worker ffmpeg -version` works.

## Progress and evidence

### PDW-1: Owner branding settings
- Route: delegated direct (writer: this session, single bounded-writer invocation).
- Migration `migrations/00010_tenant_branding.sql`: `tenant_branding` table (tenant_id PK/FK,
  owner_name, logo_key, logo_content_type, updated_at, updated_by), RLS via
  `app_tenant_visible` mirroring `views`/`exports`/`object_snapshots`. Logo bytes live in the
  object store (`internal/platform/objectstore`), not Postgres, keyed
  `tenant/{tenant_id}/branding/logo` — same split as event thumbnails.
- New package `internal/branding` (`Service{Store, Blobs, Log}`, mirroring `internal/media`'s
  `Service`/`tx` pattern): `Get`, `Logo`, `Update` (partial: owner_name and/or logo, or
  `RemoveLogo`), `Delete` (clears both). Read needs only `authorizeTenant` (own tenant, or
  any tenant for a platform user) — no permission beyond tenant membership, since every
  viewer of a plate detail needs it to render the on-screen overlay. Write requires
  `tenant.manage` (decision: closest existing tenant-administration permission; there is no
  dedicated branding permission in `internal/authz/catalog.go`, and adding one was judged
  out of scope for a single settings screen).
- Logo validation (`decodeAndValidateLogo`): PNG/JPEG only, ≤512 KB, and the declared
  `Content-Type` must match the sniffed image format (`image.DecodeConfig`).
- Contract: `packages/api-contract/openapi.yaml` — `GET/PUT/DELETE
  /api/v1/tenants/{tenantId}/branding` and `GET .../branding/logo` (binary image response,
  mirrors `getEventThumbnail`). `logo` is `type: string, format: byte` (base64), which
  oapi-codegen maps to Go `*[]byte` for free. `make generate` (`go generate ./...` +
  `pnpm generate`) re-run twice after the final commit-equivalent state, identical file list
  both times (no drift).
- Handlers: `internal/api/branding_handlers.go`; wired `Handlers.Branding` and
  `apps/api/main.go` (`branding.Service{Store: st, Blobs: store, Log: log}`).
  `internal/api/errors.go` gained a `*branding.ValidationError` case in `statusFor` (was
  missing — see RED below).
- Audit: `BRANDING_UPDATED` / `BRANDING_REMOVED` (PRD §66 PAST_TENSE convention), written in
  the same transaction as the metadata row. Labels added to `apps/web/src/routes/Audit.tsx`.
- Web: `apps/web/src/routes/Branding.tsx` (new Configuración page, gated by `tenant.manage`
  for edit controls; read-only display otherwise), nav entry "Marca de agua" in
  `apps/web/src/components/nav.ts` (gated by `tenant.manage`), route in `router.tsx`,
  `brandingQuery` in `apps/web/src/api/queries.ts`.
- RED/GREEN #1 (unit, `internal/branding/service_test.go`
  `TestDecodeAndValidateLogo/content_type_mismatches_actual_bytes`): first implementation of
  `decodeAndValidateLogo` only checked the declared Content-Type was PNG/JPEG-shaped, not
  that it matched the *actual* sniffed image format — a JPEG could be uploaded declared as
  `image/png` and pass. RED: `go test ./internal/branding/... -v` — that subtest failed
  (`want error, got nil`). Fix: cross-check `format` (from `image.DecodeConfig`) against the
  declared content type. GREEN: same command, all 7 subtests pass.
- RED/GREEN #2 (integration, `internal/api/branding_integration_test.go`
  `TestTenantBrandingAPI`): oversized logo upload returned 500 instead of 400 — `statusFor`
  in `internal/api/errors.go` only recognized `*inventory.ValidationError`, not
  `*branding.ValidationError`, so the branding service's validation error fell through to
  the generic 500 branch. RED: `go test -tags integration ./internal/api/... -run
  TestTenantBrandingAPI -v` — failed on the oversized-logo assertion (`500 ... want 400`).
  Fix: added the `*branding.ValidationError` case. GREEN: same command, full test passes
  (empty branding read, 403 without `tenant.manage`, update + logo roundtrip via
  `GET .../logo`, cross-tenant 404, delete + audit row counts, oversized-logo 400).
- Web RED/GREEN (`apps/web/src/routes/Branding.test.tsx`): initial `findByLabelText("Nombre
  del propietario")` failed (`Unable to find a label...`) because the `Field` component's
  hint text is inside the same `<label>`, so the accessible name includes the hint —
  test-authoring issue, not an app bug (existing `Field`+hint pattern, e.g.
  `Account.tsx`'s "Nueva contraseña", was never previously exercised by a web test). Fixed
  by matching with a regex (`/Nombre del propietario/`) instead of an exact string. GREEN:
  `pnpm --filter web exec vitest run src/routes/Branding.test.tsx` — 3/3 pass (read-only
  without `tenant.manage`, save with `tenant.manage`, remove-logo button only when a logo is
  configured).
- Full verification: `go build ./...` clean; `go vet ./...` clean; `golangci-lint run` — 0
  issues (after fixing one `errorlint` finding, `store.Classify(err) == store.ErrNotFound` →
  `errors.Is(store.Classify(err), store.ErrNotFound)`, to match the rest of the codebase's
  convention in `internal/identity/service.go`/`internal/inventory/cameras.go`); `go test
  ./...` PASS; `go test -tags integration ./internal/...` PASS (Docker/testcontainers, all
  packages); `pnpm --filter web test` PASS (11 files / 49 tests); `pnpm typecheck` clean
  (fixed a `remove_logo: boolean` required-field mismatch from openapi-typescript's
  `default: false` handling); `pnpm lint` clean; `make generate` re-run twice after a
  verification mishap (an exploratory `git stash`/`pop` accidentally regenerated
  `internal/store/db/models.go` against a temporarily-reverted contract — caught before
  committing, `git checkout --` on that one file, re-ran `git stash pop`, confirmed clean),
  stable file list both times, byte-identical.
- Deviation from the suggested design: the request said "read/update/delete", which this
  implements as three branding endpoints (`GET`/`PUT`/`DELETE`) plus one binary logo-fetch
  endpoint (`GET .../logo`, needed because the logo is a separate asset from the JSON
  metadata) — four endpoints total, not three; documented here since it is a minor scope
  interpretation, not a product decision requiring a stop.
- Commit: pending (this task's own commit, created immediately after this document update).

### PDW-2: Plate detail modal (photo + clip + CSS watermark)
- Route: delegated direct (writer: this session).
- No contract change: the media gateway (`internal/media/gateway.go`) is handwritten, not
  part of `packages/api-contract/openapi.yaml` (per the package's own doc comment), so this
  task touched no OpenAPI/generated files.
- Backend: refactored `lprReadSnapshot`'s "load read → authorize snapshots/recordings
  permission → authorize lpr.view" logic into a shared `lprReadCamera(w, r, p)` helper, used
  by both the existing snapshot endpoint and the new
  `GET /media/v1/lpr/reads/{id}/clip.mp4` (`lprReadClip`), which needs `recordings.view` +
  `lpr.view` (decision: mirrors `/media/v1/cameras/{id}/vod`'s permission, the closest
  existing precedent for "recorded video access"; documented here as the "clips-appropriate
  permission" the task description left open). `lprReadSnapshot` now always requests
  `quality=100` (max quality) from Frigate and accepts `?bbox=0` to turn the bounding box
  off (default stays on, `bbox=1`, unchanged from before). Range support for the clip needs
  no new code: `ad.Media().Open` already forwards the full incoming `r.Header` (including
  `Range`) to Frigate, and the existing `relay()` helper already copies
  `Content-Range`/`Accept-Ranges`/`Content-Length` and the upstream status code (206) back —
  the same mechanism `vod` already relies on for HLS segment ranges.
- `internal/frigatemock`: added `GET /api/events/{id}/clip.mp4` (`eventClip`), a deterministic
  16 KiB fake payload served via `http.ServeContent` — chosen specifically because it gives
  genuine HTTP Range semantics (206, Content-Range) for free, unlike the existing
  `exportFile` mock (a static `w.Write`, no Range support), so the clip endpoint's Range
  passthrough could be tested against real Range behavior without a real Frigate.
- Web: new `apps/web/src/components/Modal.tsx` (hand-rolled `role="dialog" aria-modal="true"`
  with Esc, a close button, a Tab/Shift+Tab focus trap, and focus restored to the trigger on
  close) — decision: not the native `<dialog>`/`showModal()`, because jsdom (this project's
  vitest environment) does not implement `showModal` at all (verified directly: `d.showModal
  is not a function`), which would make every modal test fail outright; a hand-rolled
  dialog is also easier to keep consistent with the rest of the app's Tailwind styling.
  `apps/web/src/components/PlateDetailModal.tsx` renders the photo
  (`/media/v1/lpr/reads/{id}/snapshot.jpg`) and clip (`<video controls>` sourced from
  `/media/v1/lpr/reads/{id}/clip.mp4`), each gated independently on `lpr.view` +
  `snapshots.view` / `lpr.view` + `recordings.view` (a caller with only one permission still
  sees that one section, with a message instead of a broken element for the other), with a
  `WatermarkOverlay` (date/time + owner name/logo from `brandingQuery`) positioned over each.
  `apps/web/src/lib/format.ts` gained `fmtWatermarkTimestamp` (decision: always UTC with an
  explicit `+00:00` offset, not the viewer's browser time zone, so the same text appears for
  every viewer and — in PDW-3/PDW-4 — matches what gets burned into the downloaded file
  exactly; a per-camera/site time zone was considered but `PlateRead` carries no time zone
  field today and adding one was judged out of scope for this task). `Plates.tsx` gained a
  "Ver detalle" button (shown when the actor holds `lpr.view` and at least one of
  `snapshots.view`/`recordings.view`) and modal state.
- Download buttons are intentionally not present yet: photo burn-in (PDW-3) and the clip
  watermark job (PDW-4) do not exist yet, and PDW-5 is the task that wires the download UI.
- RED/GREEN #1 (integration, `internal/api/lpr_read_clip_integration_test.go`
  `TestLPRReadClipEndpoint`): RED via the M3-doc's "temporarily disable" method (commented
  out the new `r.Get("/lpr/reads/{id}/clip.mp4", ...)` route registration) —
  `go test -tags integration ./internal/api/... -run TestLPRReadClipEndpoint -v`: 3 of 4
  subtests failed with 404 (route did not exist), including, expectedly, the
  "without recordings.view is forbidden" subtest (also 404 pre-route, since chi has nothing
  to match — noted in the test). GREEN: restored the route, same command, all 4 subtests
  pass (200 with Content-Type/Accept-Ranges, 206 + Content-Length=100 + Content-Range for a
  `Range: bytes=0-99` request, 403 without `recordings.view`, 404 for another tenant's read).
  The pre-existing `TestLPRReadSnapshotEndpoint` was re-run alongside and still passes
  unchanged (the `quality=100`/`bbox` change did not alter its Content-Type assertion).
- RED/GREEN #2 (web, `apps/web/src/components/Modal.test.tsx`): written alongside the
  component (test-only in the M3-doc sense — no bug found by simply running it), so
  non-vacuousness was proven directly per that same convention: temporarily renamed the Esc
  key check to `"EscapeBROKEN_FOR_RED_PROOF"`, reran
  `pnpm --filter web exec vitest run src/components/Modal.test.tsx` — the Esc/close-button/
  restore-focus test failed as expected (`onClose` called 0 times, want 1); reverted, same
  command, both tests (Esc/close/restore-focus, Tab focus trap) pass.
- Web RED/GREEN (`apps/web/src/routes/Plates.test.tsx`, two new tests): initial
  `screen.findByText(/Municipalidad de Helvecia/)` (no `selector` option) failed with a
  find-by-text timeout even though the text was visibly present in the rendered output —
  root cause: the overlay's date/time and owner-name are two separate JSX text-expression
  children of the same `<span>`, and with two overlays (photo + clip) both containing the
  same concatenated text, the unscoped query could not resolve to a single element; fixed by
  querying `findAllByText(..., { selector: "span" })` and asserting `length === 2` (one per
  overlay) plus the first overlay's full text content — a test-authoring fix, not an app bug
  (the text was correct and visible in both places). Second new test
  ("does not offer detail without lpr.view") passed immediately, confirming the "Ver
  detalle" button's permission gate.
- Full verification: `go build ./...` clean; `go vet ./...` clean; `golangci-lint run` 0
  issues; `go test ./...` PASS; `go test -tags integration ./internal/...` PASS (all
  packages, Docker/testcontainers — re-run after this task's changes since
  `internal/media`/`internal/frigatemock` are shared by many other integration tests, no
  regressions); `pnpm --filter web test` PASS (12 files / 53 tests); `pnpm typecheck` clean
  (fixed two `possibly 'undefined'` findings in `Modal.tsx`'s focus-trap array indexing);
  `pnpm lint` clean. No contract change, so no `make generate` step was needed for this task.
- Commit: pending (this task's own commit, created immediately after this document update).

### PDW-3: Watermarked photo download
- Route: delegated direct (writer: this session).
- No contract change (media gateway is handwritten, see PDW-2's note).
- New package `internal/watermark`: `Text(seenAt time.Time, ownerName string) string`
  (shared, single source of truth for the watermark string — must match
  `apps/web/src/lib/format.ts`'s `fmtWatermarkTimestamp` exactly, since PDW-2's CSS overlay
  uses that function and the point of a watermark is "what you see is what gets burned in";
  will also be reused by the ffmpeg `drawtext` argument in PDW-4) and
  `BurnPhoto(data []byte, text string, logo image.Image) ([]byte, error)` (Go `image/draw` +
  `golang.org/x/image/draw` for logo scaling, `golang.org/x/image/font/opentype` +
  `golang.org/x/image/font/gofont/goregular` for anti-aliased text — decision: opentype
  rendering with the embedded Go font over `font/basicfont`'s bitmap font, per the task's
  own suggestion and because a bitmap font would look noticeably worse on a watermark meant
  to be legible evidence). Added `golang.org/x/image` as a new direct dependency (`go get`
  + `go mod tidy`; also reclassified `golang.org/x/crypto` and `gorilla/websocket` from
  indirect to direct in `go.mod`, an unrelated pre-existing bookkeeping fix `go mod tidy`
  made while it was run).
- `internal/media/gateway.go`: `lprReadCamera` now also returns the read's `id` and
  `seen_at` (needed for the watermark and the download filename/audit target; the SQL
  query gained `seen_at`). `lprReadSnapshot` handles `?download=1`: requires
  `snapshots.download` in addition to what `lprReadCamera` already checked
  (`snapshots.view` + `lpr.view`), reads the full Frigate response body, calls
  `g.burnPhotoWatermark` (loads the camera's tenant branding via the new `Branding
  *branding.Service` field on `Gateway`, decodes the logo if present, calls
  `watermark.BurnPhoto`), sets `Content-Disposition: attachment`, and audits
  `SNAPSHOT_DOWNLOADED` (the existing `ActionSnapshotViewed` constant, already used by
  `eventSnapshot`'s own `download=1` path) with `target_type = "lpr_read"`. A branding
  read/logo-decode failure degrades to an un-branded (date/time only) watermark rather than
  failing the download outright — decision: the date/time is the load-bearing evidence; a
  missing owner name/logo is a lesser degradation than refusing the whole download.
  `apps/api/main.go` now shares one `branding.Service` instance between `api.Handlers` and
  `media.Gateway` (previously constructed inline only for `Handlers`).
- RED/GREEN #1 (unit, `internal/watermark/photo_test.go` `TestBurnPhoto`): forced the bar's
  fill alpha to 0 (drawing a fully transparent rectangle instead of the semi-opaque black
  bar) — RED: `go test ./internal/watermark/... -run TestBurnPhoto -v` failed
  ("bottom-bar pixel barely changed from background (diff=3)"). GREEN: restored alpha 170,
  same command passes, including the companion assertion that the *top* of the image (well
  outside the bar) is unchanged, and that output dimensions exactly match the input.
- RED/GREEN #2 (integration, `internal/api/lpr_read_snapshot_download_integration_test.go`
  `TestLPRReadSnapshotDownload`): RED via the "temporarily disable" method — replaced the
  `download=1` check with `if true { relay(...); return }`, forcing every request down the
  plain-view path. `go test -tags integration ./internal/api/... -run
  TestLPRReadSnapshotDownload -v`: all 4 assertions in the first subtest failed (no
  `Content-Disposition` header, output byte-identical to the plain view — i.e. e.g. no
  watermark burned in, 0 `SNAPSHOT_DOWNLOADED` audit rows) and the second subtest failed too
  (200 instead of 403, since the `snapshots.download` check never ran). GREEN: restored the
  real branch, same command, both subtests pass (watermarked attachment with matching
  dimensions but different bytes than the plain view; exactly 1 audit row; 403 for an actor
  with `snapshots.view` but not `snapshots.download`). Re-ran the adjacent
  `TestLPRReadClipEndpoint`/`TestLPRReadSnapshotEndpoint` alongside — unaffected.
- Full verification: `go build ./...` clean; `go vet ./...` clean; `golangci-lint run` — 0
  issues (fixed one `gosec` G705 finding on the raw `w.Write(out)` of the watermarked JPEG
  body, a false positive given the explicit `image/jpeg` Content-Type and no HTML rendering
  anywhere in the path — `//nolint:gosec` with a reason comment, matching the codebase's
  existing convention in `internal/secrets/secrets.go`/`internal/frigate/client.go`); `go
  test ./...` PASS (including the new `internal/watermark` package); `go test -tags
  integration ./internal/...` PASS (all packages, Docker/testcontainers, re-run for
  regression safety since `internal/media/gateway.go` changed substantially); `pnpm
  --filter web test` PASS (12 files / 53 tests, unchanged — no web files touched this task);
  `pnpm typecheck` clean; `pnpm lint` clean. No contract change, so no `make generate` step.
- Commit: pending (this task's own commit, created immediately after this document update).

### PDW-4: Clip watermark job in the worker
- Route: delegated direct (writer: this session; this task's write session was resumed once
  after an API rate-limit interruption mid-task — noted per the coordinator's resume
  instruction. Work in progress at the interruption (migration, sqlc queries/generated code,
  `internal/clipwatermark/{service,ffmpeg}.go` and `service_test.go`'s
  `TestDecodeAndValidateLogo`-style scaffolding had NOT yet been RED-proven) was re-verified
  from scratch after resuming, and the `escapeDrawtext` RED/GREEN below was captured fresh
  after resume — no RED evidence from before the interruption is claimed as still valid
  without re-running it.
- Contract: `packages/api-contract/openapi.yaml` — `POST
  /api/v1/lpr/reads/{readId}/clip-watermark-jobs` (create, 202 queued),
  `GET .../clip-watermark-jobs/{jobId}` (poll status), `GET .../clip-watermark-jobs/{jobId}/download`
  (binary `video/mp4`, 409 `Conflict` while not done) — `ClipWatermarkJob`/
  `ClipWatermarkJobStatus` schemas. `make generate` re-run twice after the final state,
  stable file list both times.
- Migration `migrations/00011_clip_watermark_jobs.sql`: `clip_watermark_jobs` (tenant/site/
  server/camera/lpr_read ids, `remote_event_id`, `requested_by`, `watermark_text`,
  `logo_key`, `status` CHECK IN queued/running/done/failed, `error`, `output_key`), RLS via
  `app_tenant_visible` mirroring `exports`/`views`. `watermark_text` and (a copy of) the
  logo are frozen at request time — decision: matches the photo download's own synchronous
  burn-in (current branding at the moment of the click), so the result is consistent
  regardless of whether the browser downloads a photo instantly or a clip job that sits
  queued for a while; also means the worker never needs a `branding.Service`/actor of its
  own, only `Store`/`Adapters`/`Blobs`.
- New package `internal/clipwatermark`:
  - `Service` (`CreateJob`/`GetJob`/`Download`): authorization mirrors
    `internal/media/gateway.go`'s `lprReadCamera` (recordings.view + lpr.view for any clip
    access), reusing `media.Service.Authorize` rather than reimplementing camera-ancestry
    permission checks — `CreateJob` additionally requires `exports.create`, `Download`
    additionally requires `exports.download` (decision, per the task's own instruction:
    reuse the existing export permissions rather than add dedicated ones — a clip watermark
    job is conceptually "another way to export a clip"). Denied access is audited centrally
    by the existing `h.auditDenied` (router.go's `ResponseErrorHandlerFunc`), so the service
    does not call it itself, only returns `access.ErrForbidden`-wrapped errors — confirmed
    by reading `internal/api/router.go` before writing this, per the task's own note that
    this is "already audited centrally on this branch."
  - `buildFFmpegArgs`/`escapeDrawtext` (`ffmpeg.go`): pure functions, H.264/AAC output,
    `-preset veryfast`, a fixed `-threads 2`, `-movflags +faststart`, a 120s output cap as a
    defensive limit. With a logo, switches from `-vf` to `-filter_complex` (scale + overlay
    + drawtext chained). Escaping order (backslash, then quote, then colon, then percent)
    follows ffmpeg's own documented drawtext escaping rules.
  - `Worker` (`worker.go`): `Once` claims and processes queued jobs one at a time
    (`ClaimNextClipWatermarkJob`'s `FOR UPDATE SKIP LOCKED`, matching the task's
    "concurrency 1"), fetches the clip from the camera's Frigate via
    `ad.Media().Open(ctx, "/api/events/"+remote_event_id+"/clip.mp4", nil, nil)` (the same
    path `lprReadClip` proxies), stages it to a temp file, runs ffmpeg via an exported,
    overridable `RunFFmpeg` field (nil uses the real `exec.CommandContext`; tests substitute
    a fake — this dev host confirmed to have no `ffmpeg` on PATH before writing this
    feature, per the task's own instruction not to install packages here), uploads the
    result to the object store, and marks the job done/failed. A per-job
    `context.WithTimeout` (default 5 minutes) bounds a stuck run.
  - `internal/api/errors.go` gained a `clipwatermark.ErrNotReady` → 409 case in `statusFor`.
- Docker: `deploy/docker/go.Dockerfile` gained two named runtime stages —
  `runtime-ffmpeg` (`debian:bookworm-slim` + `ffmpeg` + a non-root `appuser`) and `runtime`
  (unchanged distroless, kept as the LAST stage so a build with no `--target` still defaults
  to it — verified this matters: Docker defaults to the last stage when `--target` is
  omitted, so `runtime-ffmpeg` had to be reordered before `runtime`, not just added).
  `docker-compose.yml`'s `worker` service gained `target: runtime-ffmpeg`; every other Go
  service (`api`, `frigate-helvecia`, `frigate-cayasta`) is untouched and stays distroless.
- Worker wiring: `apps/worker/main.go` gained a `clipwatermark.Worker{Store, Adapters,
  Blobs: objects, Log}` alongside the existing `events.Syncer`/`media.ExportTracker`/
  `inventory.HealthPoller` goroutines. `apps/api/main.go` gained
  `clipwatermark.Service{Store, Media: mediaSvc, Branding: brandingSvc, Blobs: store, Log}`
  wired into `Handlers.ClipWatermark`.
- Added `golang.org/x/image` was already a direct dependency from PDW-3; no new Go
  dependency was needed for PDW-4 (ffmpeg is an external binary, not a Go import).
- RED/GREEN #1 (unit, `internal/clipwatermark/ffmpeg_test.go`, captured fresh after the
  resume): temporarily replaced `escapeDrawtext`'s single `strings.Replacer` (which performs
  one non-overlapping pass, so ordering does not matter) with four sequential
  `strings.ReplaceAll` calls in the wrong order (colon/quote/percent before backslash) — a
  real, easy-to-make bug class for exactly this kind of escaping. RED:
  `go test ./internal/clipwatermark/... -run 'TestEscapeDrawtext|TestBuildFFmpegArgsNoLogo' -v`
  — 4 of 5 `TestEscapeDrawtext` subtests failed with double-escaped backslashes (e.g.
  `13\\:05\\:30` instead of `13\:05\:30`), and `TestBuildFFmpegArgsNoLogo` failed too (its
  filter string assertion). GREEN: restored the single-pass `strings.NewReplacer`, same
  command, all pass, including `TestDrawtextFilterIsWellFormed` (counts unescaped `'` in the
  built filter, must be exactly 2 — the open/close of `text='...'`).
- RED/GREEN #2 (integration, `internal/clipwatermark/worker_integration_test.go`
  `TestClipWatermarkJobLifecycle`): RED via the "temporarily disable" method — added an
  early `return nil` before the `MarkClipWatermarkJobDone` call. `go test -tags integration
  ./internal/clipwatermark/... -run TestClipWatermarkJobLifecycle -v` — failed
  (`job status after processing = "running" ... want done`). GREEN: restored the real call,
  same command, passes (queued → running → done via a faked `RunFFmpeg` that copies the
  staged input straight to the output path, since this host has no real ffmpeg; a sibling
  subtest covers a faked ffmpeg *failure* ending in `status = "failed"` with a non-empty
  `error`, and `Download` on it returning `ErrNotReady`).
  - Design note: `worker_integration_test.go` is `package clipwatermark_test` (external),
    not a white-box `package clipwatermark` test — a white-box version created a real import
    cycle (`internal/testutil/demofix` → `internal/bootstrap` → `internal/api` →
    `internal/clipwatermark`, now that `api.Handlers` carries a `ClipWatermark` field),
    caught by `go vet -tags integration ./internal/...` failing with "import cycle not
    allowed in test." Fixed by making `Worker.RunFFmpeg` an exported field (it needed to be
    settable from `internal/api`'s own test anyway, see below) and switching the test file
    to the external test package, a standard Go pattern for this exact situation.
  - A second RED/GREEN caught a genuine test-authoring bug, not a product bug: the sibling
    `TestClipWatermarkJobAuthorization` initially only granted `exports.create` before
    calling `CreateJob`, which still failed with "forbidden" — turned out `operatorPerms`
    (`internal/bootstrap/demo.go`) already grants "operador" `recordings.view` but not
    `lpr.view`, which `authorizeClip` also requires. Fixed by granting both `lpr.view` and
    `exports.create`; documented in the test's own comment.
- RED/GREEN #3 (integration, `internal/api/clip_watermark_job_integration_test.go`
  `TestClipWatermarkJobAPI`, the HTTP-layer test): passed immediately on first run
  (test-only, no bug found — service/worker logic was already proven above). Proved
  non-vacuous by temporarily renaming the `ActionClipDownloaded` constant's value; RED:
  `go test -tags integration ./internal/api/... -run TestClipWatermarkJobAPI -v` failed
  (`CLIP_DOWNLOADED audit rows = 0, want 1`). GREEN: reverted, same command passes (create
  returns 202 queued; poll returns 200; download before the worker runs returns 409;
  download after processing returns 200 `video/mp4` and is audited).
- An ffmpeg end-to-end test exists (`internal/clipwatermark/ffmpeg_e2e_test.go`,
  `TestFFmpegEndToEnd`), skip-guarded via `exec.LookPath("ffmpeg")` per the task's own
  instruction. Ran it here: it skipped, as expected, since this host has no ffmpeg (and the
  task instructions forbid installing packages on the host) — **this means the real ffmpeg
  invocation (the actual `drawtext`/`overlay` filter graph accepted by a real ffmpeg binary)
  has NOT been verified in this environment.** The filter syntax was written to match
  ffmpeg's documented `drawtext`/`overlay`/`filter_complex` grammar and its own escaping
  rules, and the argument-list construction is unit-tested, but only a real run (this test,
  wherever ffmpeg is available — e.g. inside the built `runtime-ffmpeg` worker image) closes
  that gap. Flagged explicitly per the task's own honesty requirement, not glossed over.
- Full verification: `go build ./...` clean; `go vet ./...` clean; `golangci-lint run` — 0
  issues (fixed two `gosec` findings, G204 subprocess-with-variable and G304
  file-inclusion-with-variable, both `//nolint:gosec` with reasons — `ffmpegPath`/`args` are
  fixed config and our own `buildFFmpegArgs` output, not user input; `outputPath` is our own
  `os.CreateTemp` result); `go test -race ./...` PASS (including `internal/clipwatermark`);
  `go test -tags integration ./internal/...` PASS (all packages, Docker/testcontainers,
  full re-run for regression safety); `pnpm --filter web test` PASS (12 files / 53 tests,
  unchanged — no web files touched this task, PDW-5 wires the UI); `pnpm typecheck` clean;
  `pnpm lint` clean; `make generate` (`go generate ./...` + `pnpm generate`) re-run twice,
  stable file list both times. `go mod tidy` run, no changes beyond what PDW-3 already
  introduced.
- Commit: pending (this task's own commit, created immediately after this document update).

### PDW-5: Modal download UI + audit labels
- Route: delegated direct (writer: this session).
- No contract change.
- `apps/web/src/components/PlateDetailModal.tsx`: photo download is a plain `<a
  href="/media/v1/lpr/reads/{id}/snapshot.jpg?download=1">` gated by `snapshots.download`
  (in addition to the view permissions already gating the photo section) — mirrors the
  existing `snapshots.download` link pattern in `apps/web/src/routes/Events.tsx` exactly
  (read before writing, per the task's instruction to check existing patterns). New
  `ClipWatermarkDownload` component: a button ("Preparar clip con marca de agua") that
  `POST`s to create the job, then polls `GET .../clip-watermark-jobs/{jobId}` via
  `useQuery`'s `refetchInterval` (1.5s, stopping once `status` is `done` or `failed` —
  `react-query`'s function form of `refetchInterval` reads the latest `query.state.data`),
  showing "Preparando clip… (En cola/Generando)" while queued/running, the failed job's
  error text on failure, or a download link
  (`/api/v1/lpr/reads/{id}/clip-watermark-jobs/{jobId}/download`) once done — the same
  queued/running/done/failed UX `Exports.tsx` already has for its own jobs, scoped to one
  plate read instead of a list. Gated by `exports.create` (creating the job) in addition to
  the clip-view permissions; the download link itself relies on the server enforcing
  `exports.download` (a 403 there surfaces through the browser's normal download-failure
  UI, matching how `Events.tsx`'s snapshot download link already behaves without a
  client-side `exports.download`/`snapshots.download` check on the link itself beyond what
  is already used to decide whether to render it).
- Audit labels added to `apps/web/src/routes/Audit.tsx`: `CLIP_WATERMARK_REQUESTED` →
  "Clip con marca de agua solicitado", `CLIP_DOWNLOADED` → "Clip descargado" (both actions
  were already implemented server-side in PDW-4; this task only adds their Spanish labels).
- RED/GREEN (web, `apps/web/src/components/PlateDetailModal.test.tsx`, new file, 4 tests):
  written alongside the component and passed immediately (test-only in the M3-doc sense).
  Proved non-vacuous for the most failure-prone piece — the polling stop condition — by
  temporarily changing the download link's render condition from `status === "done"` to a
  typo'd string; RED: `pnpm --filter web exec vitest run src/components/PlateDetailModal.test.tsx -t "requests, polls"`
  — the test timed out waiting for the download link that would now never appear (5000ms
  timeout, confirming the assertion is load-bearing, not vacuous). GREEN: reverted, same
  command (and the full file) passes: photo download link absent/present by
  `snapshots.download`, clip job create → poll (queued → running → done, via a route mock
  that flips status on each poll) → download link with the exact expected `href`, and a
  failed-job path showing both the generic and the job's own error message.
- Full verification: `pnpm --filter web test` PASS (13 files / 57 tests); `pnpm typecheck`
  clean; `pnpm lint` clean. Go side unchanged this task — `go build ./...`, `go vet ./...`,
  `golangci-lint run` (0 issues) and `go test ./...` re-run anyway for safety, all clean. No
  contract change, so no `make generate` step.
- Commit: pending (this task's own commit, created immediately after this document update).

### PDW-6: Fix independent-verification findings
- Route: delegated direct (writer: this session, resumed continuation of the same bounded
  writer that did PDW-1..5; not a fresh session).
- No contract change (no OpenAPI surface touched by any of these fixes).
- **Finding 1+2 (ffmpeg drawtext injection + literal `\:` rendering), `internal/clipwatermark/ffmpeg.go`:**
  root cause confirmed by re-reading ffmpeg's own filtergraph quoting rules: content inside
  `drawtext=text='...'` single quotes is copied by ffmpeg *literally* — backslash has **no**
  escaping meaning there at all, and there is no way to represent a literal `'` inside a
  single-quoted value (ffmpeg's own docs: close the quote, insert an escaped quote *outside*
  it, reopen the quote). The old `escapeDrawtext` backslash-then-colon-then-quote-then-percent
  `strings.NewReplacer` therefore did two wrong things at once: escaping colons as `\:`
  rendered as a **literal** backslash-colon in every clip (finding 2, since backslash isn't an
  escape inside quotes), and "escaping" a quote as `\'` did not close/reopen — it emitted a
  literal backslash then ended the quoted value early, so an owner name containing `'`
  corrupted the filtergraph (finding 1). Confirmed empirically against the *real* ffmpeg binary
  (5.1.9, worker container — see below): the old code's exact args for owner name
  `O'Brien's Towing` do not error, but ffmpeg's parser absorbs everything after the stray quote
  — including `:fontcolor=white:fontsize=24:box=1:boxcolor=...:x=10:y=...` — into the drawn
  text itself, so `fontcolor`/`box`/positioning are silently never applied (the black
  background bar disappears) and the burned-in text ends with literal garbage
  (`...Towing':fontcolor=white:fontsize=24:...`). A real, observable defect, not merely
  theoretical.
  - **Fix**: the watermark text is no longer embedded in the filtergraph string at all. The
    worker (`worker.go`'s new `writeTempText`) writes it verbatim (unescaped — it is just file
    content) to a fixed-name file (`watermark.txt`) inside a fresh private temp directory
    (`os.MkdirTemp`), and `buildFFmpegArgs` now takes a `textFilePath`, building
    `drawtext=textfile='<path>':expansion=none:...`. `expansion=none` stops drawtext from
    interpreting `%{...}` sequences in the file's content (the old percent-escaping is no
    longer needed for the same reason). Only the (server-generated, non-attacker-controlled)
    file path is still embedded inline, quoted via a new `quoteFilterValue`, which — having
    read the real quoting rules this time — *refuses* (returns an error) rather than
    mis-escapes if the path ever contained a `'`; `buildFFmpegArgs` now returns `([]string,
    error)`, propagated through `runJob`.
- **Finding 3 (silent audio loss with a logo), `ffmpeg.go`:** the `-filter_complex` branch had
  no `-map`, so ffmpeg's default stream selection kept only the filtered video pad and dropped
  the source audio (a plain `-vf`, used in the no-logo branch, keeps ffmpeg's normal implicit
  "map everything from the single input" behavior, so it was unaffected). Fix: label the video
  output (`...,drawtext=...[vout]`) and add `-map "[vout]" -map "0:a?"` (audio optional, so a
  silent source clip still succeeds) to the logo branch only.
- **Finding 4 (stuck jobs), `internal/clipwatermark/worker.go`:**
  - `runJobRecovered` wraps `runJob` with `recover()`, turning a panic anywhere in job
    processing into a normal "failed" job instead of crashing the whole worker process (which
    also runs `events.Syncer`/`media.ExportTracker`/`inventory.HealthPoller` on the same
    process — a single clip-job panic would have taken all of them down).
  - `Worker.SweepStuck` (exported, called periodically from `Run` on a new `SweepInterval`
    ticker, default 1 minute) fails every job still `status='running'` after `stuckTimeout` (15
    minutes), mirroring `internal/media/exports.go`'s `exportTimeout` sweep — for a worker
    process that is *killed* (not merely panicking) mid-job, `recover()` never runs at all,
    since the process itself is gone, so only a periodic external sweep can ever unstick that
    row. New sqlc query `FailStuckClipWatermarkJobs` (`internal/store/queries/clipwatermark.sql`
    → generated `internal/store/db/clipwatermark.sql.go`).
- **Finding 5 (logo decompression bomb), `internal/branding/service.go`:** `image.DecodeConfig`
  only reads the image header, so a highly-compressible solid-color PNG can declare an
  enormous width/height while staying tiny on disk (well under `MaxLogoBytes`); that file would
  previously pass validation and later be fully decoded (`BurnPhoto` for photo downloads, and
  again when staged for ffmpeg's overlay filter for clip jobs), allocating gigabytes for a
  small upload. Added `MaxLogoPixels = 1024*1024` and reject `cfg.Width*cfg.Height >
  MaxLogoPixels` (400 `ValidationError`, already wired through `statusFor`).
- **Finding 6 (JPEG quality nit), `internal/watermark/photo.go`:** `jpeg.Options{Quality: 95}`
  → named constant `photoJPEGQuality = 100` (user asked for maximum quality; the download is
  evidence).
- **RED/GREEN #1** (`internal/clipwatermark/ffmpeg_test.go`, rewritten): new tests for the
  desired textfile-based behavior (`TestBuildFFmpegArgsNoLogo/WithLogo`,
  `TestBuildFFmpegArgsNeverInlinesRawText`, `TestQuoteFilterValueRejectsSingleQuote`,
  `TestBuildFFmpegArgsRejectsUnquotableTextPath`) were written against the new
  `buildFFmpegArgs`/`quoteFilterValue` signatures before either existed. RED: `git stash push
  -- internal/clipwatermark/ffmpeg.go && go test ./internal/clipwatermark/... -run
  'TestBuildFFmpegArgs|TestQuoteFilterValue' -v` — build failure (`assignment mismatch: 2
  variables but buildFFmpegArgs returns 1 value`, `undefined: quoteFilterValue`) against the
  pre-PDW-6 `ffmpeg.go`. GREEN: `git stash pop`, same command, all 5 pass.
- **RED/GREEN #2** (`internal/clipwatermark/worker_text_test.go`, new,
  `TestWriteTempTextContentMatchesWatermarkTextExactly`): proves finding 2's explicit ask (the
  textfile content equals `watermark.Text`'s output exactly). Non-vacuousness proven directly:
  temporarily made `writeTempText` append a stray `!` to the written bytes; RED: `go test
  ./internal/clipwatermark/... -run TestWriteTempTextContentMatchesWatermarkTextExactly -v` —
  failed (`got ...Towing!, want ...Towing`). GREEN: reverted, same command passes.
- **RED/GREEN #3** (integration, `internal/clipwatermark/worker_integration_test.go`
  `TestClipWatermarkJobRecoversFromPanic`): temporarily called `w.runJob` directly instead of
  `w.runJobRecovered` in `process`. RED: `go test -tags integration ./internal/clipwatermark/...
  -run TestClipWatermarkJobRecoversFromPanic -v` — the panic was **not** caught: the test
  *binary itself* panicked and crashed (`panic: simulated panic inside job processing
  [recovered, repanicked]`), the strongest possible non-vacuous RED signal (without `recover()`
  there is no test failure to report — the whole process dies). GREEN: reverted to
  `runJobRecovered`, same command passes (job ends `status=failed`, error mentions "panic").
- **RED/GREEN #4** (integration, `TestSweepStuckClipWatermarkJobs`): two jobs forced to
  `status='running'` at different `updated_at` ages (20 min and 10 s ago) via a raw SQL update
  through `env.Store.TxRaw`. RED: temporarily made `SweepStuck` an early `return`. `go test
  -tags integration ./internal/clipwatermark/... -run TestSweepStuckClipWatermarkJobs -v` —
  failed (`swept job status = "running", want failed`). GREEN: reverted, same command passes
  (the 20-minute-old job is failed with a non-empty error; the 10-second-old job is left
  running, proving the sweep does not touch recently-claimed jobs).
- **RED/GREEN #5** (`internal/branding/service_test.go`, new subtest "huge declared dimensions
  despite small byte size (decompression bomb)"): a 2000×2000 solid-color PNG (`png.Encode`
  compresses it to well under `MaxLogoBytes`, asserted by the fixture generator itself). RED:
  `go test ./internal/branding/... -run TestDecodeAndValidateLogo -v` — failed (`want error,
  got nil`) before `MaxLogoPixels`/the bound existed. GREEN: same command, all 8 subtests pass.
- **RED/GREEN #6** (`internal/watermark/photo_test.go`, new `TestPhotoJPEGQualityIsMaximum`):
  written to reference an as-yet-undefined `photoJPEGQuality` constant. RED: `go test
  ./internal/watermark/... -run TestPhotoJPEGQualityIsMaximum -v` — compile failure (`undefined:
  photoJPEGQuality`). GREEN: added the constant (100) and used it in `BurnPhoto`'s
  `jpeg.Encode` call, same command passes.
- **Real ffmpeg check (the whole point of this task's own explicit instruction):** the host
  still has no ffmpeg (confirmed again), but the already-running `openvms-worker-1` container
  does (ffmpeg 5.1.9-0+deb12u1, built with `--enable-libx265`/`--enable-libx264`, `ffprobe`
  present). Compiled a static integration-tagged test binary
  (`CGO_ENABLED=0 go test -tags integration -c -o cw.test ./internal/clipwatermark`), `docker
  cp`'d it into the container, and ran it there
  (`docker exec -w /tmp openvms-worker-1 /tmp/cw.test -test.v -test.run FFmpeg`) — the *only*
  docker commands used, no restart/rebuild. All 12 `TestFFmpegEndToEnd` subtests passed against
  the real binary:
  - plain watermark burn-in (valid output produced).
  - **six** owner-name subtests covering `'`, `:`, `%`, `,`, `;`, `[` — every one renders and
    exits 0 with the textfile fix (this is the same real binary that, per the "old bug" repro
    below, mishandles the `'` case under the pre-fix code).
  - source with a sine-wave audio track **and** a generated PNG logo → both an audio and a
    video stream present in the output (checked via `ffprobe -show_entries stream=codec_type`,
    present in this image), proving finding 3's `-map` fix.
  - an HEVC (`libx265`) source, re-encoded to H.264 output (this ffmpeg build has `libx265`, so
    the subtest ran rather than skipped) — output has a video stream.
  - Additionally, to *prove* findings 1/2 were real and not just a misreading of ffmpeg's docs:
    wrote a small standalone Go program reproducing the **pre-fix** `buildFFmpegArgs`/
    `escapeDrawtext` byte-for-byte, compiled it statically, ran it in the same container
    against the same real ffmpeg with owner name `O'Brien's Towing`. Result: ffmpeg did **not**
    error (exit 0, 3211-byte output) — but per ffmpeg's quoting rules the stray `'` ends the
    quoted text value early, so everything from `Brien...` through the final `y=h-th-10` gets
    re-parsed and, because the final quote is never closed, the *entire* remainder
    (`:fontcolor=white:fontsize=24:box=1:boxcolor=black@0.6:boxborderw=8:x=10:y=h-th-10`) is
    absorbed as literal drawn text instead of being applied as filter options — the background
    box, font color and position are silently never applied, and the burned-in text ends with
    visible filter-syntax garbage. A real, silent corruption bug, confirmed against the actual
    binary, not merely inferred from documentation.
  - Container cleanup: the container's non-root user could not `rm` the root-owned files
    `docker cp` placed under `/tmp` (`Operation not permitted`) — left in place; they are
    ephemeral container state, not part of the image or repo, and the container is recreated
    by the final `make up` anyway. No image rebuild or container restart was performed before
    that final step.
- **Full verification**: `go build ./...` clean; `go vet ./...` clean; `golangci-lint run
  ./...` — 0 issues; `go test ./...` PASS (all packages); `go test -tags integration
  ./internal/...` PASS (all packages, Docker/testcontainers, full re-run for regression
  safety); `go test -race ./...` PASS; `make generate` (`go generate ./...` + `pnpm generate`)
  re-run twice, stable file list both times (only the intentional
  `internal/store/db/clipwatermark.sql.go` diff, no drift) — no contract change, so no web
  types regenerated differently. `pnpm --filter web test`/`pnpm typecheck`/`pnpm lint` not
  re-run for this task (no web files touched by PDW-6; will be covered again at the end-of-
  session combined verification pass).
- Commit: pending (this task's own commit, created immediately after this document update).

### PDW-7: configurable per-tenant watermark timezone
- Route: delegated direct (writer: this session, same continued session as PDW-1..6).
- **Contract**: `packages/api-contract/openapi.yaml` — `TenantBranding` gains required
  `timezone` (string), `TenantBrandingInput` gains optional `timezone`. `make generate`
  (`go generate ./...` + `pnpm generate`) re-run twice after the final state, stable file list
  both times.
- **Migration** `migrations/00012_tenant_branding_timezone.sql`: `ALTER TABLE tenant_branding
  ADD COLUMN timezone text NOT NULL DEFAULT 'America/Argentina/Buenos_Aires'` (Down: `DROP
  COLUMN`). sqlc query `UpsertTenantBranding` (`internal/store/queries/branding.sql`) updated
  to insert/update the new column.
- **`internal/branding` (service.go)**: `Branding.Timezone string`; new `DefaultTimezone`
  constant and `ResolveLocation(tz string) *time.Location` (empty → `DefaultTimezone`; a name
  that fails `time.LoadLocation` → `time.UTC`, defensive — should be unreachable once
  `Update`'s own validation runs and tzdata is embedded, but a watermark download must never
  fail outright over a time zone lookup). `Get`'s zero-value `Branding` (no row configured
  yet) now carries `Timezone: DefaultTimezone`. `Input.Timezone *string`; `Update` validates it
  (trim, reject empty, `time.LoadLocation` must succeed) before ever reaching the database,
  same synchronous-validation shape as the existing owner-name-length and
  remove-logo-plus-logo checks. `Delete` resets `timezone` to `DefaultTimezone` alongside
  clearing owner name/logo.
- **`internal/watermark/text.go`**: `Text(seenAt time.Time, ownerName string, loc
  *time.Location) string` (was `Text(seenAt, ownerName)`, always UTC). Renders
  `seenAt.In(loc)` with Go's `"2006-01-02 15:04:05 -07:00"` layout — a real, explicit numeric
  offset computed by the Go runtime's own (now embedded, see tzdata below) IANA tz database,
  not a fixed `"UTC+00:00"` placeholder. `loc == nil` still defaults to UTC (used by one
  existing test's owner-less case, and as a safe zero-value).
- **Callers updated**: `internal/media/gateway.go`'s `burnPhotoWatermark` (photo download,
  PDW-3) and `internal/clipwatermark/service.go`'s `CreateJob` (clip job, frozen at request
  time, PDW-4) both now call `watermark.Text(lr.SeenAt, b.OwnerName,
  branding.ResolveLocation(b.Timezone))` instead of the old two-argument call — the tenant's
  configured zone flows into both burn-in paths identically, no divergence between them.
- **tzdata embedding**: `apps/api/main.go` and `apps/worker/main.go` both gained a blank
  `import _ "time/tzdata"`. **Correction of an assumption made while writing the first draft
  of this change**: the doc comments originally claimed distroless "ships no zoneinfo at all"
  and that the worker's debian-slim image "does not install tzdata" — both stated as fact
  without having checked. Verified directly at `make up` time (see below) and found **both
  claims wrong** for the images this repo actually builds: `docker create
  gcr.io/distroless/static-debian12:nonroot + docker cp /usr/share/zoneinfo` shows that image
  does ship the full IANA zoneinfo tree, and `docker compose exec worker dpkg -l | grep
  tzdata` shows the `tzdata` package present in the worker image too (pulled in transitively
  by `ffmpeg`/`ca-certificates`, not installed explicitly by
  `deploy/docker/go.Dockerfile`). So `time.LoadLocation` would in fact already work in both
  images today, without the embed. The doc comments in both files were corrected to state
  this accurately rather than leave a false claim in the code. The embed itself is kept
  regardless — not as a fix for a confirmed gap, but as a correctness guarantee that no
  longer depends on an incidental, transitive property of the current base images (a
  future ffmpeg release dropping its tzdata dependency, or switching the api's base image to
  plain `gcr.io/distroless/static` without the `-debian12` zoneinfo bundle, would otherwise
  silently break every non-UTC watermark). This is the kind of unverified claim the task's own
  "verify before stating" instruction exists for, caught before it shipped as a code comment
  read by future maintainers as fact.
- **Web**: `apps/web/src/lib/format.ts`'s `fmtWatermarkTimestamp(iso, timeZone)` (was
  `fmtWatermarkTimestamp(iso)`, always UTC) — renders the date/time via
  `Intl.DateTimeFormat(..., { timeZone, hour12: false, ... }).formatToParts` and the numeric
  offset via a second `Intl.DateTimeFormat(..., { timeZone, timeZoneName: "longOffset" })`
  call (yields `"GMT-03:00"`/`"GMT+00:00"`; stripping `"GMT"` gives the same `±HH:MM` shape Go
  produces). New export `DEFAULT_WATERMARK_TIMEZONE` mirrors `branding.DefaultTimezone`.
  `apps/web/src/components/PlateDetailModal.tsx`: `WatermarkOverlay` and the "Fecha" field
  both take a `timezone` prop, sourced from `branding.data?.timezone ??
  DEFAULT_WATERMARK_TIMEZONE`. `apps/web/src/routes/Branding.tsx`: new "Zona horaria" field
  (a `<Select>` populated via `Intl.supportedValuesOf("timeZone")`), gated the same way as the
  existing owner-name field (disabled without `tenant.manage`), sent in the PUT body only when
  changed (same "send only what changed" pattern as `owner_name`).
  - **Real bug found and fixed during this task's own RED/GREEN cycle** (not a pre-existing
    finding, a fresh one from building the `<select>`): `Intl.supportedValuesOf("timeZone")`
    only enumerates ICU's *canonical* zone identifiers — it does **not** include
    `"America/Argentina/Buenos_Aires"` itself (ICU's canonical form for that offset is
    `"America/Buenos_Aires"`), even though the alias is a perfectly valid IANA name that both
    `Intl.DateTimeFormat` and Go's `time.LoadLocation` accept directly (confirmed with a
    one-line check of each). Discovered via this task's own new integration test (below):
    setting the select to `America/Argentina/Buenos_Aires` — the *task-specified default* —
    silently sent `timezone: ""` in the PUT body, because setting a `<select>`'s `.value` to a
    string with no matching `<option>` resets it to `""` per the HTML spec. Fixed by having
    `timeZoneOptions()` always union in `current` and `DEFAULT_WATERMARK_TIMEZONE`, regardless
    of whether the canonical enumeration includes them.
- **RED/GREEN #1** (`internal/watermark/photo_test.go`, rewritten `TestText` +
  new `TestTextComputesDSTOffsetFromRealTZData`): written against the new 3-arg signature
  before it existed. RED: `git stash push -- internal/watermark/text.go && go test
  ./internal/watermark/... -run TestText -v` — build failure (`too many arguments in call to
  Text`) against the pre-PDW-7 `text.go`. GREEN: `git stash pop`, same command, all 6 subtests
  pass, including Europe/Madrid rendering `+01:00` in January and `+02:00` in July for the
  same code path — proving the offset is computed from real tzdata, not hardcoded.
- **RED/GREEN #2** (`internal/branding/service_test.go`, new `TestUpdateRejectsInvalidTimezone`
  + `TestResolveLocation`): written against `Input.Timezone`/`ResolveLocation`/
  `DefaultTimezone` before any of them existed. RED: `go test ./internal/branding/... -run
  'TestUpdateRejectsInvalidTimezone|TestResolveLocation' -v` — compile failure (`unknown field
  Timezone in struct literal`, `undefined: ResolveLocation`, `undefined: DefaultTimezone`).
  GREEN: same command after implementing, all pass (rejects a non-IANA name, empty, and
  whitespace-only; `ResolveLocation` defaults empty to `DefaultTimezone`, degrades an invalid
  name to UTC, passes a valid name through).
- **RED/GREEN #3** (web, `apps/web/src/lib/format.test.ts`, new file): written against the new
  2-arg `fmtWatermarkTimestamp` signature before the implementation changed. RED: `git stash
  push -- apps/web/src/lib/format.ts && pnpm --filter web exec vitest run
  src/lib/format.test.ts` — 3 of 4 tests failed (old code ignored the new `timeZone` argument
  entirely and always returned the fixed `"... UTC+00:00"` string). GREEN: `git stash pop`,
  same command, all 4 pass.
- **RED/GREEN #4** (web, `apps/web/src/components/PlateDetailModal.test.tsx`, new test
  "renders the watermark timestamp in the tenant's configured time zone"): RED via `git stash
  push -- apps/web/src/components/PlateDetailModal.tsx && pnpm --filter web exec vitest run
  src/components/PlateDetailModal.test.tsx -t "configured time zone"` — failed (old
  single-argument call site rendered `"2024-01-01 10:00:00 +00:00"`, not the expected Madrid
  `"...11:00:00 +01:00"`). GREEN: `git stash pop`, same command passes. This test run also
  surfaced a genuine test-authoring issue (not an app bug): the "Fecha" `dd` and both overlay
  `span`s render the identical string when `ownerName` is empty, so an unscoped
  `findByText` threw "found multiple elements" — fixed by scoping with `{ selector: "dd" }` /
  `{ selector: "span" }`, the same fix shape PDW-2's own doc already used for this exact
  ambiguity.
- **RED/GREEN #5** (web, `apps/web/src/routes/Branding.test.tsx`, new test "shows and saves
  the configured watermark time zone"): RED (no select existed yet): `pnpm --filter web exec
  vitest run src/routes/Branding.test.tsx` — failed (`findByLabelText(/Zona horaria/)` timed
  out). First GREEN attempt (the `<select>` from `Intl.supportedValuesOf` alone, no union with
  `current`/default) still failed — this is the real bug described above, caught by this very
  test, not a separate contrived RED — fixed by the `timeZoneOptions` union fix. Final GREEN:
  same command, all 4 subtests pass.
- **Existing tests updated for the new format** (not new coverage, a mechanical consequence of
  the timestamp format change): `apps/web/src/routes/Plates.test.tsx`'s
  "opens the plate detail modal..." test's branding stub gained `timezone:
  "America/Argentina/Buenos_Aires"` and its expected overlay text changed from `"2024-01-01
  10:00:00 UTC+00:00"` to `"2024-01-01 07:00:00 -03:00"` (10:00 UTC in a fixed -03:00 zone).
  Ran before/after to confirm it would have failed unmodified against the new `format.ts`
  (UTC+00:00 no longer appears anywhere) and passes with the update.
- **Integration** (`internal/api/branding_integration_test.go`, extended
  `TestTenantBrandingAPI`): empty branding now asserts `timezone == branding.DefaultTimezone`;
  the admin PUT now also sets `timezone: "Europe/Madrid"` and asserts it round-trips; a new
  assertion PUTs `{"timezone":"Not/AZone"}` and expects 400; DELETE now also asserts
  `timezone` resets to `DefaultTimezone`. Non-vacuousness proven by targeted mutation (not a
  full revert, since half the repo depends on the new `branding.Branding.Timezone` field and
  won't build without it): temporarily hardcoded `toTenantBranding` to always emit
  `Timezone: "BROKEN"`. RED: `go test -tags integration ./internal/api/... -run
  TestTenantBrandingAPI -v` — failed on both new timezone assertions (`empty branding timezone
  = BROKEN, want America/Argentina/Buenos_Aires`; `updated branding timezone = BROKEN, want
  Europe/Madrid`). GREEN: reverted, same command passes in full (5.1s, includes the
  logo/audit/cross-tenant assertions from PDW-1, unaffected).
- **Full verification**: `go build ./...` clean; `go vet ./...` clean; `golangci-lint run
  ./...` — 0 issues; `go test ./...` PASS (all packages); `go test -tags integration
  ./internal/...` PASS (all packages, Docker/testcontainers, full re-run); `go test -race
  ./...` PASS; `pnpm --filter web test` PASS (14 files / 63 tests); `pnpm typecheck` clean;
  `pnpm lint` clean; `make generate` (`go generate ./...` + `pnpm generate`) re-run twice,
  stable `git status` both times (contract, sqlc, and openapi-typescript outputs all
  regenerate identically).
- Deviation/decision documented inline above (not a stop-worthy product decision, a
  discovered-during-implementation correctness fix): the `<select>`'s option list is the union
  of `Intl.supportedValuesOf("timeZone")` with the tenant's current value and the default,
  rather than the canonical list alone, because the canonical list omits the task's own
  specified default zone name.
- Commit: pending (this task's own commit, created immediately after this document update).

### PDW-8: PlateDetailModal tabs + watermark timestamp confirmation
- Route: delegated direct (writer: this session, same continued session). Web-only; no Go
  changes (the timestamp-source confirmation below found the backend already correct).
- **Timestamp confirmation (user report: "the watermark date/time is wrong")**: traced the
  full source chain before touching any code.
  - Backend: `internal/media/gateway.go`'s `burnPhotoWatermark` reads `lr.SeenAt` (populated
    by `lprReadCamera`'s SQL, `SELECT ... seen_at FROM lpr_reads`) — the same
    `lpr_reads.seen_at` column the API serves as `PlateRead.seen_at`. `internal/clipwatermark
    /service.go`'s `CreateJob` reads the identical `lr.SeenAt` (its own `lookupRead` query,
    `SELECT ... seen_at FROM lpr_reads WHERE id = $1`) and freezes it into
    `watermark_text` at request time. Both burn-in paths and the API response are the exact
    same database column — never re-derived, never a different field (e.g. `created_at`) —
    so there was no source-mismatch bug to find.
  - The actual bug was PDW-7's own subject: before PDW-7, `watermark.Text` and
    `fmtWatermarkTimestamp` both hardcoded UTC (`"... UTC+00:00"` always), so a tenant not in
    UTC (e.g. Argentina, UTC-03:00) saw a time three hours ahead of their own clock — which is
    exactly "the date/time is wrong" from that tenant's point of view, not a wrong data
    source. PDW-7 already fixes this (tenant-configured IANA zone, real computed offset); this
    task's job was to confirm that, not re-fix it.
  - **Confirmed identical rendering**: `internal/watermark/photo_test.go`'s `TestText`
    "Buenos Aires (no DST)" case and `apps/web/src/lib/format.test.ts`'s "matches Go's
    watermark.Text format exactly" case both assert the exact same output
    (`"2026-09-28 13:05:30 -03:00"`/`"2026-09-28 13:05:30 -03:00"`, respectively, for the
    same instant translated by hand) for the same instant/zone; `PlateDetailModal.test.tsx`'s
    "renders the watermark timestamp in the tenant's configured time zone" test (from PDW-7,
    still passing) exercises the real component end to end. No new test was needed here
    beyond re-confirming PDW-7's own passing suite (re-run as part of this task's own full
    verification below) — this is a confirmation, not a fix, and is recorded as such rather
    than manufacturing busywork test churn.
- **Tabs**: `apps/web/src/components/PlateDetailModal.tsx` restructured from two
  always-rendered sections (photo, then clip, stacked) into a `role="tablist"` with two
  `role="tab"` buttons ("Foto"/"Clip", `id`s `plate-detail-tab-{id}`, `aria-controls` pointing
  at `plate-detail-panel-{id}`, `aria-selected`, roving `tabIndex` (0 for the active tab, -1
  for the other, standard WAI-ARIA Tabs pattern) and one `role="tabpanel"`
  (`aria-labelledby`, `tabIndex={0}`) that renders **only** the active tab's content — a
  single dynamically-swapped panel, not both panels toggled by CSS `hidden`, specifically so
  the `<video>` element is never even created in the DOM until the Clip tab is selected
  (`preload="metadata"` alone does not prevent the browser from starting a fetch the instant
  the element mounts). Arrow-key navigation (`ArrowLeft`/`ArrowRight`, wrapping) moves both
  focus and the active tab together, matching the WAI-ARIA APG's "automatic activation" tabs
  pattern. Each tab's panel keeps its own `WatermarkOverlay` and its own download control
  (photo's download link / `ClipWatermarkDownload`), unchanged from PDW-2/PDW-5 otherwise —
  only their container moved.
- **RED/GREEN** (`apps/web/src/components/PlateDetailModal.test.tsx`, new `describe("tabs
  (PDW-8)")` block, 4 tests: default-tab/no-video-yet, mount-on-switch/photo-unmounts,
  arrow-key navigation, per-tab overlay+download): written against the tabbed structure before
  it existed. RED: `git stash push -- apps/web/src/components/PlateDetailModal.tsx && pnpm
  --filter web exec vitest run src/components/PlateDetailModal.test.tsx` — 7 of 9 tests
  failed against the pre-PDW-8 component (no `role="tab"` elements existed at all,
  `findByRole("tab", ...)` failed outright, cascading into the tests that click a tab to reach
  the clip UI). GREEN: `git stash pop`, same command, all 9 pass (5 pre-existing + 4 new).
  Two pre-existing tests in this file (and `apps/web/src/routes/Plates.test.tsx`'s modal test)
  needed mechanical updates for the new tab-gated flow (click "Clip" before the
  clip-job UI is reachable; assert `video` absent until then) — proven via the same
  stash/RED/GREEN cycle against `Plates.test.tsx` (1 failure: the "no video yet" assertion,
  since the pre-PDW-8 component always rendered it) before restoring.
- **Full verification**: `pnpm --filter web test` PASS (14 files / 67 tests); `pnpm typecheck`
  clean; `pnpm lint` clean. Go side untouched this task — `go build ./...`, `go vet ./...`,
  `go test ./...` re-run anyway for safety (all clean/PASS), no `golangci-lint`/integration
  re-run needed (no Go files changed) beyond the combined final pass below.
- Commit: pending (this task's own commit, created immediately after this document update).

## Final combined verification (after all eight tasks)
Run once, after PDW-8's commit, not concurrently with anything else:
- `go test ./...`: PASS (all packages).
- `go test -tags integration ./internal/...`: PASS (all packages, Docker/testcontainers).
- `go test -race ./...`: PASS.
- `pnpm --filter web test`: PASS (14 files / 67 tests).
- `pnpm typecheck`: clean.
- `make lint` (`go vet` + `golangci-lint run` + `pnpm lint` + `pnpm typecheck`): clean, 0
  golangci-lint issues.
- `make generate` (`go generate ./...` + `pnpm generate`) followed by `git diff --exit-code`:
  clean — the committed tree is already byte-identical to a fresh regeneration.

## `make up` (the only docker-compose rebuild/restart in this session; ran once, at the end)
`VERSION=dev docker compose up -d --build` — rebuilt `api`, `worker`, `web` (the only three
images this branch's changes touch) and restarted them; every other service (postgres, nats,
valkey, seaweedfs, mosquitto) was already running from before this session and untouched.
- `api`: logs show `"migration applied" ... "file":"00012_tenant_branding_timezone.sql"` then
  `"database schema ready" ... "version":12` then `"listening"`; `GET /health/ready` → 200,
  `{"status":"ok"}` with postgres/valkey/nats/object_storage all `"ok"`.
- `worker`: started cleanly, no errors; `docker compose exec worker ffmpeg -version` → ffmpeg
  5.1.9-0+deb12u1 (per the task's own explicit closing check).
- `web`: Caddy started, serving; `curl 127.0.0.1:8000/` → 200.
- **Live end-to-end proof of PDW-7's tzdata concern** (the strongest available evidence,
  exercising the actual built image rather than inspecting its filesystem): bootstrapped a
  platform admin token (`docker compose exec api /vmsctl bootstrap`) against the existing demo
  tenant ("Casa", seeded in an earlier session) and, through the real running `api` container:
  `GET .../branding` → `timezone: "America/Argentina/Buenos_Aires"` (DB default); `PUT
  {"timezone":"Europe/Madrid"}` → 200 with the new value persisted, proving
  `time.LoadLocation("Europe/Madrid")` succeeds *inside the actual distroless process*; `PUT
  {"timezone":"Not/AZone"}` → 400 (validation rejects it end to end); downloaded a real plate
  read's watermarked photo (`GET .../snapshot.jpg?download=1`) with the Madrid zone still
  configured — 200, valid JPEG (`file` confirms `JPEG image data, baseline, ... 640x360`),
  153722 bytes. Reset the tenant's timezone back to the default afterward and removed the
  downloaded file, leaving the demo environment as found. This check also **caught and
  corrected an unverified claim** made while writing the tzdata doc comments (see the
  PDW-7 section above and the follow-up `docs` commit): both apps' comments originally
  asserted their base images ship no zoneinfo at all, which turned out to be false for the
  images this repo currently builds — corrected before merge, not left as a wrong comment for
  a future reader to trust.
- `docker compose ps`: all eight services `Up`, no restarts/crash loops observed.

## Status: complete
All eight PDW tasks (PDW-1 through PDW-8) are implemented, tested (strict TDD throughout —
every task's RED observed before its GREEN, per the resolved mode recorded in each section
above), committed on `feat/plate-detail-watermark`, and verified against the deployed stack.
Not pushed and not merged, per this session's own standing instruction — that remains the
user's decision.
