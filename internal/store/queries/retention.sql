-- name: GetRetentionSettings :one
SELECT * FROM retention_settings WHERE singleton;

-- name: UpdateRetentionSettings :one
UPDATE retention_settings
SET face_unknown_days = @face_unknown_days,
    notification_delivery_days = @notification_delivery_days,
    notification_read_days = @notification_read_days,
    pressure_cleanup = @pressure_cleanup,
    pressure_percent = @pressure_percent,
    updated_at = now(),
    updated_by = @updated_by
WHERE singleton
RETURNING *;

-- name: ExpireFaceSightings :one
-- Erases up to max_rows pending face sightings seen before the cutoff: the crop is queued for
-- deletion, and the crop reference, the embedding vector and its profile are cleared. The row stays
-- (as rejected) so suggestion verdicts that name it keep their reference. SKIP LOCKED lets several
-- workers run without waiting for each other. Returns how many rows were erased.
WITH due AS (
    SELECT id, tenant_id, crop_key
    FROM face_sightings
    WHERE review_status = 'pending' AND COALESCE(source_seen_at, first_seen_at) < @cutoff::timestamptz
    ORDER BY first_seen_at
    LIMIT @max_rows::int
    FOR UPDATE SKIP LOCKED
), erased AS (
    UPDATE face_sightings fs
    SET review_status = 'rejected', reviewed_at = now(),
        crop_key = NULL, crop_content_type = NULL, crop_size_bytes = NULL, crop_sha256 = NULL, ingest_error = '',
        embedding = NULL, embedding_profile = NULL, embedding_model = NULL, embedded_at = NULL
    FROM due
    WHERE fs.id = due.id
    RETURNING due.tenant_id, due.crop_key
), queued AS (
    INSERT INTO face_blob_purge (storage_key, tenant_id)
    SELECT crop_key, tenant_id FROM erased WHERE crop_key IS NOT NULL AND crop_key <> ''
    ON CONFLICT (storage_key) DO NOTHING
    RETURNING 1
)
SELECT count(*)::bigint FROM erased;

-- name: CountExpirableFaceSightings :one
-- How many pending sightings a retention of cutoff would erase, and the crop bytes they hold.
SELECT count(*)::bigint AS records, COALESCE(sum(crop_size_bytes), 0)::bigint AS bytes
FROM face_sightings
WHERE review_status = 'pending' AND COALESCE(source_seen_at, first_seen_at) < @cutoff::timestamptz;

-- name: CountPendingFaceSightings :one
SELECT count(*)::bigint AS records, COALESCE(sum(crop_size_bytes), 0)::bigint AS bytes
FROM face_sightings
WHERE review_status = 'pending';
