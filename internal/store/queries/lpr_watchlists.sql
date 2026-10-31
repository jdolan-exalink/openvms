-- name: ListLprWatchlists :many
SELECT w.id, w.tenant_id, w.name, w.description, w.color, w.created_at, w.updated_at,
  count(i.id)::int AS item_count
FROM lpr_watchlists w
LEFT JOIN lpr_watchlist_items i ON i.watchlist_id = w.id AND i.tenant_id = w.tenant_id
WHERE w.tenant_id = @tenant_id
GROUP BY w.id
ORDER BY lower(w.name), w.id;

-- name: GetLprWatchlist :one
SELECT w.id, w.tenant_id, w.name, w.description, w.color, w.created_at, w.updated_at,
  count(i.id)::int AS item_count
FROM lpr_watchlists w
LEFT JOIN lpr_watchlist_items i ON i.watchlist_id = w.id AND i.tenant_id = w.tenant_id
WHERE w.tenant_id = @tenant_id AND w.id = @id
GROUP BY w.id;

-- name: CreateLprWatchlist :one
INSERT INTO lpr_watchlists (tenant_id, name, description, color, created_by, updated_by)
VALUES (@tenant_id, @name, @description, @color, @created_by, @created_by)
RETURNING *;

-- name: UpdateLprWatchlist :one
UPDATE lpr_watchlists
SET name = @name, description = @description, color = @color, updated_by = @updated_by, updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id
RETURNING *;

-- name: DeleteLprWatchlist :execrows
DELETE FROM lpr_watchlists WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListLprWatchlistItems :many
SELECT id, tenant_id, watchlist_id, plate, notes, vehicle_brand, vehicle_model, vehicle_color, owner_name, created_at, updated_at
FROM lpr_watchlist_items
WHERE tenant_id = @tenant_id AND watchlist_id = @watchlist_id
ORDER BY plate, id;

-- name: GetLprWatchlistItem :one
SELECT id, tenant_id, watchlist_id, plate, notes, vehicle_brand, vehicle_model, vehicle_color, owner_name, created_at, updated_at
FROM lpr_watchlist_items
WHERE tenant_id = @tenant_id AND watchlist_id = @watchlist_id AND id = @id;

-- name: AddLprWatchlistItem :one
INSERT INTO lpr_watchlist_items (tenant_id, watchlist_id, plate, notes, vehicle_brand, vehicle_model, vehicle_color, owner_name)
VALUES (@tenant_id, @watchlist_id, @plate, @notes, @vehicle_brand, @vehicle_model, @vehicle_color, @owner_name)
ON CONFLICT (watchlist_id, plate) DO UPDATE SET
  notes = EXCLUDED.notes,
  vehicle_brand = EXCLUDED.vehicle_brand,
  vehicle_model = EXCLUDED.vehicle_model,
  vehicle_color = EXCLUDED.vehicle_color,
  owner_name = EXCLUDED.owner_name,
  updated_at = now()
RETURNING *;

-- name: RemoveLprWatchlistItem :execrows
DELETE FROM lpr_watchlist_items
WHERE tenant_id = @tenant_id AND watchlist_id = @watchlist_id AND id = @id;

-- name: LockLprWatchlistsByIDs :many
SELECT id FROM lpr_watchlists
WHERE tenant_id = @tenant_id AND id = ANY(@ids::uuid[])
ORDER BY id
FOR SHARE;

-- name: LockLprWatchlist :one
SELECT id FROM lpr_watchlists WHERE tenant_id = @tenant_id AND id = @id FOR UPDATE;

-- name: CountRulesUsingLprWatchlist :one
SELECT count(*) FROM rules
WHERE tenant_id = @tenant_id AND conditions -> 'lpr_watchlist_ids' @> to_jsonb(@id::text);

-- name: ListLprWatchlistPlatesIndex :many
SELECT watchlist_id, plate FROM lpr_watchlist_items WHERE tenant_id = @tenant_id;

-- name: MatchLprWatchlistsForPlates :many
SELECT w.id AS watchlist_id, w.name AS watchlist_name, w.color AS watchlist_color, i.plate
FROM lpr_watchlist_items i
JOIN lpr_watchlists w ON w.id = i.watchlist_id AND w.tenant_id = i.tenant_id
WHERE i.tenant_id = @tenant_id AND upper(i.plate) = ANY(@plates::text[]);
