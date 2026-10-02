# Private map backgrounds

Custom floor plans use canonical PNG bytes, not active SVG/PDF documents. Geographic placements
remain independent: a camera may have one geographic position and a position on each floor.

## Image bounds

The backend independently checks at most 20 MiB encoded input, 8192 pixels per side, and
16 million pixels total before full decoding. It then reencodes PNG, discarding metadata and
trailing content. MIME headers and client-reported dimensions are not authoritative.
SVG/PDF browser conversion must finish before uploading; raw documents are never accepted.

## Verification

`go test -race ./internal/maps -run TestCanonicalPlanPNG` covers valid PNG, trailing active
content, corrupt image data, non-PNG documents, encoded-size limits and oversized dimensions.
These are bounded image-validation tests, not browser rendering or production deployment proof.

## Hierarchy and concurrency

Named buildings group floor maps within one tenant/site. Active floor/building/site ownership is
checked for placements, zones and floor entity reads; unplaced queries may select a floor without
changing the existing geographic default. Row locks serialize map removal against placement writes.
Renaming/replacing/deleting a map requires its exact revision; wildcard overwrites are rejected.
Deleting an occupied floor or a building with active floors is refused, preserving user layouts.
Floor ordinals remain unique even after soft deletion in the existing schema; a reused ordinal
returns conflict. No coordinate/schema migration is required for this foundation.

## HTTP contract

All paths below are under `/api/v1/maps/sites/{siteId}` and require authentication.

| Operation | Path | Body / permission |
| --- | --- | --- |
| Create grouping | `POST /buildings` | `{name}` / `maps.edit` |
| Rename/remove grouping | `PATCH` / `DELETE /buildings/{buildingId}` | Rename `{name}` / `maps.edit` |
| Create plan | `POST /buildings/{buildingId}/floors` | `{name, ordinal}` / `maps.edit` |
| Rename/reorder/remove plan | `PATCH` / `DELETE /floors/{floorId}` | Update `{name, ordinal}` / `maps.edit` |
| Replace background | `PUT /floors/{floorId}/plan` | Binary `image/png` / `maps.edit` |
| Read background | `GET /floors/{floorId}/plan` | PNG, `private, no-store` / `maps.view` |

PATCH, DELETE and image PUT require `If-Match` with the exact revision returned by creation,
mutation or site details. Existing maps feature flag gates every new endpoint. Upload dimensions
come from server decoding. `plan_key` is opaque storage metadata, not a public/signed URL; clients
read images only through the authenticated plan endpoint. No filename, object key, raw document,
external resource URL or client dimensions are accepted in the upload contract.

`GET /api/v1/maps/unplaced?site_id={siteId}&floor_id={floorId}` selects floor-unplaced cameras;
omitting `floor_id` retains geographic semantics. Camera visibility remains permission-filtered.
A geographically placed camera may still be unplaced on a selected floor. Positions are normalized
`x/y` on floors and remain separate from latitude/longitude.

Failed or uncertain uploads are cleaned up by a new server-owned UUID key; the prior background
and revision remain unchanged. A successful replacement removes only its previous owned plan blob.
Cleanup failures are logged for operations follow-up, not misreported as successful cleanup.

Inventory moving to another site does not move its stored map coordinates automatically. A stale
placement is excluded from the old site's entity read by current inventory site/tenant checks;
coordinates remain stored for explicit reconciliation, not silently deleted or reassigned.
