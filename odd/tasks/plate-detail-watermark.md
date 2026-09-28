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
- [ ] PDW-2: Plate detail modal — full-quality photo + clip playback via gateway (Range) + CSS watermark overlay.
- [ ] PDW-3: Watermarked photo download (Go burn-in, embedded font, logo composited).
- [ ] PDW-4: Clip watermark job in the worker (ffmpeg image, job table/state, object store, status + download endpoints).
- [ ] PDW-5: Modal download UI for photo and clip job (progress, errors), audit labels.

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

## Next step
PDW-2: plate detail modal (full-quality photo + clip playback through the media gateway with
Range support, CSS watermark overlay). Extends `internal/media/gateway.go` (new clip-proxy
route) and `apps/web/src/routes/Plates.tsx`.
