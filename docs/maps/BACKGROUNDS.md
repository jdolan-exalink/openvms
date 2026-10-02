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
