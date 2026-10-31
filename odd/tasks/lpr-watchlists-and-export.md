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
- [x] Migration `migrations/00088_lpr_watchlists.sql`:
  - `lpr_watchlists` (tenant_id, name, description, color, created_at, updated_at).
  - `lpr_watchlist_items` (tenant_id, watchlist_id, plate, notes, brand, model, color, owner_name, created_at).
- [x] SQL queries in `internal/store/queries/lpr_watchlists.sql` and `sqlc generate`.
- [x] Store repository tests.

### Slice 3: Rules & Alarm Engine Integration
- [x] Add `LprWatchlistIDs []uuid.UUID` to `rules.Conditions`.
- [x] Add `StoreLprResolver` in `internal/rules/lpr.go` with tenant cache.
- [x] Evaluate rules on plate reads (both direct Provision LPR and Frigate events).
- [x] Unit & integration tests in `internal/rules/`.

### Slice 4: REST API & OpenAPI Contract
- [x] OpenAPI endpoints in `packages/api-contract/openapi.yaml`:
  - `GET /api/v1/lpr/watchlists`
  - `POST /api/v1/lpr/watchlists`
  - `GET /api/v1/lpr/watchlists/{id}`
  - `PATCH /api/v1/lpr/watchlists/{id}`
  - `DELETE /api/v1/lpr/watchlists/{id}`
  - `POST /api/v1/lpr/watchlists/{id}/items`
  - `DELETE /api/v1/lpr/watchlists/{id}/items/{itemId}`
- [x] Handlers in `internal/api/lpr_watchlist_handlers.go`.
- [x] Plate reads enriched with matched watchlists in `internal/api/events_handlers.go`.
- [x] Regeneration of `api.gen.go` and `schema.d.ts`.
- [x] API integration tests in `internal/api/lpr_watchlists_integration_test.go`.

### Slice 5: Web UI Management & Plate Badges
- [x] Watchlist manager dialog (`LprWatchlistsModal.tsx`) in LPR interface.
- [x] Watchlist alert pills on `PlateReadCard` and `PlateDetailModal`.
- [x] Rule creation UI option for LPR Watchlists in `Rules.tsx`.
- [x] Full i18n support in `es`, `en`, and `pt`.
- [x] Vitest coverage in `LprWatchlistsModal.test.tsx` and `Plates.test.tsx`.
