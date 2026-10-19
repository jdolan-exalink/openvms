-- name: GetFacePerson :one
SELECT * FROM face_persons WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListFacePersons :many
SELECT p.*, (SELECT count(*) FROM face_person_images i WHERE i.person_id = p.id)::int AS image_count
FROM face_persons p
WHERE p.tenant_id = @tenant_id AND p.deleted_at IS NULL
ORDER BY lower(p.display_name), p.id;

-- name: LockFacePersonForUpdate :one
SELECT * FROM face_persons WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL FOR UPDATE;

-- name: CreateFacePersonIfNameFree :one
-- Collisions on (tenant_id, frigate_name) return no row so the caller retries with a suffix;
-- any other violation (display name) still raises. A name with a pending remote cleanup is also
-- taken, so that cleanup can never delete a new person's face.
INSERT INTO face_persons (tenant_id, display_name, frigate_name, notes, created_by, updated_by)
SELECT @tenant_id, @display_name, @frigate_name, @notes, @created_by, @created_by
WHERE NOT EXISTS (
  SELECT 1 FROM face_remote_cleanup c WHERE c.tenant_id = @tenant_id AND c.frigate_name = @frigate_name
)
ON CONFLICT (tenant_id, frigate_name) DO NOTHING
RETURNING *;

-- name: EnqueueFaceBlobPurge :exec
INSERT INTO face_blob_purge (storage_key, tenant_id) VALUES (@storage_key, @tenant_id)
ON CONFLICT (storage_key) DO NOTHING;

-- name: ListFaceBlobPurge :many
SELECT * FROM face_blob_purge ORDER BY created_at, storage_key LIMIT @max_rows;

-- name: DeleteFaceBlobPurge :exec
DELETE FROM face_blob_purge WHERE storage_key = @storage_key;

-- name: BumpFaceBlobPurgeAttempts :exec
UPDATE face_blob_purge SET attempts = attempts + 1 WHERE storage_key = @storage_key;

-- name: UpdateFacePerson :one
UPDATE face_persons
SET display_name = @display_name, notes = @notes, updated_by = @updated_by, updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteFacePerson :one
UPDATE face_persons
SET deleted_at = now(), updated_by = @updated_by, updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL
RETURNING *;

-- name: InsertFacePersonImage :one
INSERT INTO face_person_images (id, tenant_id, person_id, storage_key, content_type, size_bytes, sha256, width, height, created_by)
VALUES (@id, @tenant_id, @person_id, @storage_key, @content_type, @size_bytes, @sha256, @width, @height, @created_by)
RETURNING *;

-- name: ListFacePersonImages :many
SELECT * FROM face_person_images WHERE tenant_id = @tenant_id AND person_id = @person_id ORDER BY created_at, id;

-- name: GetFacePersonImage :one
SELECT * FROM face_person_images WHERE tenant_id = @tenant_id AND person_id = @person_id AND id = @id;

-- name: CountFacePersonImages :one
SELECT count(*)::int FROM face_person_images WHERE tenant_id = @tenant_id AND person_id = @person_id;

-- name: DeleteFacePersonImage :exec
DELETE FROM face_person_images WHERE tenant_id = @tenant_id AND person_id = @person_id AND id = @id;

-- name: DeleteFacePersonImages :exec
DELETE FROM face_person_images WHERE tenant_id = @tenant_id AND person_id = @person_id;

-- name: ListFaceNamesIn :many
-- Which of these labels are the Frigate name of a registered person (tombstones included: the
-- name stays in stored events until the purge scrubs it). Row level security limits the result
-- to the caller's tenants.
SELECT tenant_id, frigate_name FROM face_persons WHERE frigate_name = ANY(@names::text[]);

-- name: ListFaceSyncSummary :many
-- Per-person count of sync jobs by status (the UI folds them into a few buckets).
SELECT person_id, status, count(*)::int AS n FROM face_server_sync
WHERE tenant_id = @tenant_id GROUP BY person_id, status;

-- name: ListFaceCapableServers :many
SELECT * FROM frigate_servers
WHERE tenant_id = @tenant_id AND deleted_at IS NULL AND capabilities @> '{"face_recognition": true}'::jsonb
ORDER BY lower(name), id;

-- name: ListFaceCapableServersScoped :many
-- Face-capable servers across the given tenants, narrowed by optional site and server filters
-- (an empty array means no restriction).
SELECT * FROM frigate_servers
WHERE tenant_id = ANY(@tenant_ids::uuid[]) AND deleted_at IS NULL AND capabilities @> '{"face_recognition": true}'::jsonb
  AND (cardinality(@site_ids::uuid[]) = 0 OR site_id = ANY(@site_ids::uuid[]))
  AND (cardinality(@server_ids::uuid[]) = 0 OR id = ANY(@server_ids::uuid[]))
ORDER BY lower(name), id;
