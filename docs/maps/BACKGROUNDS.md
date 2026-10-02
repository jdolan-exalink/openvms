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
