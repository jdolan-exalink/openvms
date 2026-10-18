-- name: ListFaceWatchlists :many
SELECT w.id, w.tenant_id, w.name, w.description, w.created_at, w.updated_at,
  coalesce(array_agg(m.person_id ORDER BY m.created_at, m.person_id) FILTER (WHERE m.person_id IS NOT NULL), '{}')::uuid[] AS person_ids
FROM face_watchlists w
LEFT JOIN face_watchlist_members m ON m.watchlist_id = w.id AND m.tenant_id = w.tenant_id
WHERE w.tenant_id = @tenant_id
GROUP BY w.id
ORDER BY lower(w.name), w.id;

-- name: GetFaceWatchlist :one
SELECT w.id, w.tenant_id, w.name, w.description, w.created_at, w.updated_at,
  coalesce(array_agg(m.person_id ORDER BY m.created_at, m.person_id) FILTER (WHERE m.person_id IS NOT NULL), '{}')::uuid[] AS person_ids
FROM face_watchlists w
LEFT JOIN face_watchlist_members m ON m.watchlist_id = w.id AND m.tenant_id = w.tenant_id
WHERE w.tenant_id = @tenant_id AND w.id = @id
GROUP BY w.id;

-- name: CreateFaceWatchlist :one
INSERT INTO face_watchlists (tenant_id, name, description, created_by, updated_by)
VALUES (@tenant_id, @name, @description, @created_by, @created_by)
RETURNING *;

-- name: UpdateFaceWatchlist :one
UPDATE face_watchlists
SET name = @name, description = @description, updated_by = @updated_by, updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id
RETURNING *;

-- name: DeleteFaceWatchlist :execrows
DELETE FROM face_watchlists WHERE tenant_id = @tenant_id AND id = @id;

-- name: ClearFaceWatchlistMembers :exec
DELETE FROM face_watchlist_members WHERE tenant_id = @tenant_id AND watchlist_id = @watchlist_id;

-- name: AddFaceWatchlistMembers :exec
INSERT INTO face_watchlist_members (tenant_id, watchlist_id, person_id)
SELECT @tenant_id, @watchlist_id, p.id
FROM face_persons p
WHERE p.tenant_id = @tenant_id AND p.deleted_at IS NULL AND p.id = ANY(@person_ids::uuid[])
ON CONFLICT DO NOTHING;

-- name: RemoveFacePersonFromWatchlists :exec
DELETE FROM face_watchlist_members WHERE tenant_id = @tenant_id AND person_id = @person_id;

-- name: CountFacePersonsByIDs :one
SELECT count(*) FROM face_persons
WHERE tenant_id = @tenant_id AND deleted_at IS NULL AND id = ANY(@ids::uuid[]);

-- name: LockFaceWatchlistsByIDs :many
-- Shared row lock on the watchlists a rule is about to reference: a concurrent delete waits for
-- the rule's transaction (and then sees the rule). Ordered by id so two lockers never deadlock.
SELECT id FROM face_watchlists
WHERE tenant_id = @tenant_id AND id = ANY(@ids::uuid[])
ORDER BY id
FOR SHARE;

-- name: LockFaceWatchlist :one
-- Exclusive row lock taken before counting the rules that use the watchlist, so a rule create or
-- update that already checked it commits first and is counted.
SELECT id FROM face_watchlists WHERE tenant_id = @tenant_id AND id = @id FOR UPDATE;

-- name: CountRulesUsingFaceWatchlist :one
SELECT count(*) FROM rules
WHERE tenant_id = @tenant_id AND conditions -> 'face_watchlist_ids' @> to_jsonb(@id::text);

-- name: ListFaceNameIndex :many
-- Live persons of a tenant: the rules resolver maps person ids to the sub_label Frigate reports.
SELECT id, frigate_name FROM face_persons WHERE tenant_id = @tenant_id AND deleted_at IS NULL;

-- name: ListFaceMembershipIndex :many
SELECT watchlist_id, person_id FROM face_watchlist_members WHERE tenant_id = @tenant_id;
