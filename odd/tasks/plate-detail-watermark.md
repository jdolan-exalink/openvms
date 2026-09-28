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

## Next step
All five PDW tasks are implemented and committed on `feat/plate-detail-watermark` (not
pushed — per instructions, this writer does not push/merge). Remaining: final full
verification pass (already run per-task; one more combined run before reporting), then a
single `make up` at the very end to confirm the stack deploys and
`docker compose exec worker ffmpeg -version` works, per the task's own closing instruction.
