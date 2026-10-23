-- name: GetFacePerson :one
SELECT * FROM face_persons WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListFacePersons :many
SELECT p.*, (SELECT count(*) FROM face_person_images i WHERE i.person_id = p.id)::int AS image_count,
  -- The nil uuid stands for "no photo yet" (sqlc cannot type a nullable scalar subquery).
  COALESCE((SELECT i.id FROM face_person_images i WHERE i.person_id = p.id ORDER BY i.created_at, i.id LIMIT 1), '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS first_image_id
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

-- name: ListFaceCapableServersAny :many
-- Worker-only background discovery; RLS is explicitly bypassed through store.AllTenants.
SELECT * FROM frigate_servers
WHERE deleted_at IS NULL AND capabilities @> '{"face_recognition": true}'::jsonb
ORDER BY tenant_id, lower(name), id;

-- name: InsertFaceSighting :one
INSERT INTO face_sightings (tenant_id, server_id, source_file, source_seen_at, source_label, source_score)
VALUES (@tenant_id, @server_id, @source_file, @source_seen_at, @source_label, @source_score)
ON CONFLICT (tenant_id, server_id, source_file) DO NOTHING
RETURNING *;

-- name: GetFaceSightingBySource :one
SELECT * FROM face_sightings
WHERE tenant_id = @tenant_id AND server_id = @server_id AND source_file = @source_file;

-- name: SetFaceSightingCrop :exec
UPDATE face_sightings
SET crop_key = @crop_key, crop_content_type = @crop_content_type,
    crop_size_bytes = @crop_size_bytes, crop_sha256 = @crop_sha256,
    ingest_error = ''
WHERE id = @id AND review_status = 'pending';

-- name: SetFaceSightingRetry :exec
UPDATE face_sightings SET ingest_error = @ingest_error
WHERE id = @id AND review_status = 'pending' AND crop_key IS NULL;

-- name: ListFaceReviewSightings :many
SELECT fs.id, fs.tenant_id, fs.server_id, fs.source_file, fs.source_seen_at, fs.source_label,
       fs.source_score, fs.first_seen_at, fs.crop_key, fs.crop_content_type, fs.crop_size_bytes,
       fs.crop_sha256, fs.ingest_error, fs.review_status, fs.person_id, fs.reviewed_at,
       fs.identity_group_id, COALESCE(fs.identity_group_id, fs.id) AS identity_id,
       s.name AS server_name, s.site_id,
       (fs.crop_key IS NOT NULL)::boolean AS crop_available
FROM face_sightings fs
JOIN frigate_servers s ON s.id = fs.server_id AND s.tenant_id = fs.tenant_id
WHERE fs.tenant_id = ANY(@tenant_ids::uuid[])
  AND fs.review_status = 'pending'
  AND (cardinality(@site_ids::uuid[]) = 0 OR s.site_id = ANY(@site_ids::uuid[]))
  AND (cardinality(@server_ids::uuid[]) = 0 OR fs.server_id = ANY(@server_ids::uuid[]))
ORDER BY fs.source_seen_at DESC NULLS LAST, fs.first_seen_at DESC, fs.id DESC;

-- name: LockFaceIdentityGroups :exec
SELECT pg_advisory_xact_lock(hashtextextended(@tenant_id::text, 65065));

-- name: GetFaceSightingsForGrouping :many
SELECT id, review_status, crop_key, identity_group_id
FROM face_sightings
WHERE tenant_id = @tenant_id AND id = ANY(@sighting_ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: GetFaceIdentityGroupsForMerge :many
SELECT id, created_at
FROM face_identity_groups
WHERE tenant_id = @tenant_id AND id = ANY(@group_ids::uuid[])
ORDER BY created_at, id
FOR UPDATE;

-- name: CreateFaceIdentityGroup :exec
INSERT INTO face_identity_groups (id, tenant_id) VALUES (@id, @tenant_id);

-- name: MoveFaceIdentityGroupMembers :exec
UPDATE face_sightings
SET identity_group_id = @survivor_id
WHERE tenant_id = @tenant_id AND identity_group_id = ANY(@group_ids::uuid[]);

-- name: SetFaceSightingsIdentityGroup :exec
UPDATE face_sightings
SET identity_group_id = @group_id
WHERE tenant_id = @tenant_id AND id = ANY(@sighting_ids::uuid[]) AND identity_group_id IS NULL;

-- name: DeleteFaceIdentityGroups :exec
DELETE FROM face_identity_groups
WHERE tenant_id = @tenant_id AND id = ANY(@group_ids::uuid[]);

-- name: CountFaceIdentityGroupMembers :one
SELECT count(*)::int FROM face_sightings
WHERE tenant_id = @tenant_id AND identity_group_id = @group_id;

-- name: GetFaceSighting :one
SELECT * FROM face_sightings WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetFaceSightingForUpdate :one
SELECT * FROM face_sightings WHERE tenant_id = @tenant_id AND id = @id FOR UPDATE;

-- name: AssignFaceSighting :execrows
UPDATE face_sightings
SET review_status = 'assigned', person_id = @person_id, reviewed_at = now(),
    crop_key = NULL, crop_content_type = NULL, crop_size_bytes = NULL, crop_sha256 = NULL, ingest_error = ''
WHERE tenant_id = @tenant_id AND id = @id AND review_status = 'pending' AND crop_key IS NOT NULL;

-- name: RejectFaceSighting :execrows
UPDATE face_sightings
SET review_status = 'rejected', reviewed_at = now(),
    crop_key = NULL, crop_content_type = NULL, crop_size_bytes = NULL, crop_sha256 = NULL, ingest_error = ''
WHERE tenant_id = @tenant_id AND id = @id AND review_status = 'pending';

-- name: ListFacePersonLastRecognitions :many
-- The newest event per live person on the given cameras (the caller's events.view set). The
-- lateral join stops at the first match per person, so the cost is one index probe each.
SELECT p.id AS person_id, r.id AS event_id, r.start_time, r.has_thumbnail
FROM face_persons p
CROSS JOIN LATERAL (
  SELECT e.id, e.start_time, (e.thumb_key <> '')::boolean AS has_thumbnail
  FROM events e
  WHERE e.tenant_id = p.tenant_id AND e.sub_labels @> ARRAY[p.frigate_name]
    AND e.camera_id = ANY(@camera_ids::uuid[])
  ORDER BY e.start_time DESC, e.id DESC
  LIMIT 1
) r
WHERE p.tenant_id = @tenant_id AND p.deleted_at IS NULL;

-- name: ListFaceSightingsToEmbed :many
-- A crop the detector rejected (face_detected = false) is never listed. With @detect on, unchecked
-- crops are listed even when their vector is current so the detector can still judge them.
SELECT id, tenant_id, crop_key, face_detected,
       (embedding IS NOT NULL AND embedding_profile IS NOT DISTINCT FROM @profile::text AND embedding_model IS NOT DISTINCT FROM @model::text)::boolean AS embedded_current
FROM face_sightings
WHERE review_status = 'pending' AND crop_key IS NOT NULL
  AND face_detected IS DISTINCT FROM false
  AND embed_failures < @max_failures::smallint
  AND (embedding IS NULL OR embedding_profile IS DISTINCT FROM @profile::text OR embedding_model IS DISTINCT FROM @model::text
       OR (@detect::boolean AND face_detected IS NULL))
ORDER BY embed_failures, first_seen_at, id
LIMIT @row_limit;

-- name: RecordFaceSightingEmbedFailure :exec
UPDATE face_sightings SET embed_failures = LEAST(embed_failures + 1, 32767)::smallint
WHERE id = @id AND review_status = 'pending';

-- name: SetFaceSightingEmbedding :execrows
UPDATE face_sightings
SET embedding = @embedding::real[], embedding_profile = @profile::text, embedding_model = @model::text,
    embedded_at = now(), embed_failures = 0,
    face_detected = COALESCE(sqlc.narg('face_detected')::boolean, face_detected)
WHERE id = @id AND review_status = 'pending' AND crop_key IS NOT NULL;

-- name: MarkFaceSightingFace :execrows
UPDATE face_sightings SET face_detected = true, embed_failures = 0
WHERE id = @id AND review_status = 'pending';

-- name: MarkFaceSightingNotFace :execrows
UPDATE face_sightings
SET face_detected = false, embedding = NULL, embedding_profile = NULL, embedding_model = NULL,
    embedded_at = NULL, embed_failures = 0
WHERE id = @id AND review_status = 'pending';

-- name: ListFaceSightingEmbeddings :many
SELECT id, COALESCE(identity_group_id, id) AS identity_id, embedding_profile, embedding_model, embedding
FROM face_sightings
WHERE id = ANY(@ids::uuid[]) AND review_status = 'pending' AND embedding IS NOT NULL
ORDER BY first_seen_at DESC, id DESC;

-- name: UpsertFaceSuggestionDecision :exec
INSERT INTO face_suggestion_decisions (tenant_id, sighting_lo, sighting_hi, decision, similarity, decided_by)
VALUES (@tenant_id, @sighting_lo, @sighting_hi, @decision, @similarity, @decided_by)
ON CONFLICT (tenant_id, sighting_lo, sighting_hi) DO UPDATE
SET decision = EXCLUDED.decision, similarity = EXCLUDED.similarity, decided_by = EXCLUDED.decided_by, decided_at = now();

-- name: ListFaceDifferentSightingPairs :many
SELECT sighting_lo, sighting_hi FROM face_suggestion_decisions
WHERE decision = 'different' AND tenant_id = ANY(@tenant_ids::uuid[])
  AND (sighting_lo = ANY(@sighting_ids::uuid[]) OR sighting_hi = ANY(@sighting_ids::uuid[]));

-- name: FaceSightingIsPending :one
SELECT EXISTS (
  SELECT 1 FROM face_sightings WHERE tenant_id = @tenant_id AND id = @sighting_id AND review_status = 'pending'
);
