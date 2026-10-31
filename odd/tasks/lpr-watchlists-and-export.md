# ODD Task: LPR Watchlists & CSV Export

## 1. Overview
Empower operators with two major LPR capabilities:
1. **LPR CSV Export**: Export filtered plate readings directly to CSV from the Plates explorer with complete vehicle attributes (timestamp, plate, camera, site, brand, model, color, score, zones).
2. **LPR Watchlists & Alarm Integration**: Create and manage plate watchlists (e.g. stolen vehicles, unauthorized access, VIPs), evaluate plate reads against watchlists in the Rules engine (`rules.Conditions.LprWatchlistIDs`), and raise real-time Alarms with notification dispatch (WAHA WhatsApp, Webhooks) and visual alert badges on Plate Cards.

## 2. Work Units & Checklist

### Slice 1: LPR CSV Export in Plates Explorer
- [x] Export helper for plate reads converting items to RFC 4180 CSV with UTF-8 BOM.
- [x] "Exportar CSV" button in `apps/web/src/routes/Plates.tsx` with `Download` icon.
- [x] Support exporting current filtered query (respecting date range, camera, vehicle type, color, plate filter).
- [x] Unit tests in `apps/web/src/routes/Plates.test.tsx`.

### Slice 2: Database Migration & Storage for LPR Watchlists
- [ ] Migration `migrations/00088_lpr_watchlists.sql`:
  - `lpr_watchlists` (tenant_id, name, description, color, created_at, updated_at).
  - `lpr_watchlist_items` (tenant_id, watchlist_id, plate, notes, brand, model, color, owner_name, created_at).
- [ ] SQL queries in `internal/store/queries/lpr_watchlists.sql` and `sqlc generate`.
- [ ] Store repository tests.

### Slice 3: Rules & Alarm Engine Integration
- [ ] Add `LprWatchlistIDs []uuid.UUID` to `rules.Conditions`.
- [ ] Add `StoreLprResolver` in `internal/rules/lpr.go` with tenant cache.
- [ ] Evaluate rules on plate reads (both direct Provision LPR and Frigate events).
- [ ] Unit & integration tests in `internal/rules/`.

### Slice 4: REST API & OpenAPI Contract
- [ ] OpenAPI endpoints in `packages/api-contract/openapi.yaml`:
  - `GET /api/v1/lpr/watchlists`
  - `POST /api/v1/lpr/watchlists`
  - `GET /api/v1/lpr/watchlists/{id}`
  - `PATCH /api/v1/lpr/watchlists/{id}`
  - `DELETE /api/v1/lpr/watchlists/{id}`
  - `POST /api/v1/lpr/watchlists/{id}/items`
  - `DELETE /api/v1/lpr/watchlists/{id}/items/{itemId}`
- [ ] Handlers in `internal/api/lpr_watchlist_handlers.go`.
- [ ] Regeneration of `api.gen.go` and `schema.d.ts`.

### Slice 5: Web UI Management & Plate Badges
- [ ] Watchlist manager dialog / tab in LPR interface.
- [ ] Watchlist alert pill on `PlateReadCard` and `PlateDetailModal`.
- [ ] Rule creation UI option for LPR Watchlists in `Rules.tsx`.
- [ ] Vitest coverage.
