-- name: ListCameraZonesByCamera :many
SELECT id, tenant_id, camera_id, name, points, settings, created_at, updated_at
FROM camera_zones
WHERE tenant_id = @tenant_id AND camera_id = @camera_id AND deleted_at IS NULL
ORDER BY created_at, id;

-- name: CreateCameraZone :one
INSERT INTO camera_zones (tenant_id, camera_id, name, points, settings)
VALUES (@tenant_id, @camera_id, @name, @points, @settings)
RETURNING id, camera_id, name, points, settings, created_at, updated_at;

-- name: UpdateCameraZone :one
UPDATE camera_zones
SET name = @name, points = @points, settings = @settings, updated_at = now()
WHERE tenant_id = @tenant_id AND camera_id = @camera_id AND id = @id AND deleted_at IS NULL
RETURNING id, camera_id, name, points, settings, created_at, updated_at;

-- name: DeleteCameraZone :execrows
DELETE FROM camera_zones
WHERE tenant_id = @tenant_id AND camera_id = @camera_id AND id = @id;

-- name: EnqueueCameraZoneSync :one
INSERT INTO camera_zone_sync_jobs
    (zone_id, tenant_id, camera_id, actor_user_id, operation, desired_revision, desired_config, last_applied, previous_remote_name)
VALUES (@zone_id, @tenant_id, @camera_id, @actor_user_id, @operation, @desired_revision, @desired_config, @last_applied, sqlc.narg('previous_remote_name'))
ON CONFLICT (zone_id) DO UPDATE SET
    tenant_id = EXCLUDED.tenant_id,
    camera_id = EXCLUDED.camera_id,
    actor_user_id = EXCLUDED.actor_user_id,
    operation = EXCLUDED.operation,
    desired_revision = EXCLUDED.desired_revision,
    desired_config = EXCLUDED.desired_config,
    previous_remote_name = coalesce(camera_zone_sync_jobs.previous_remote_name, EXCLUDED.previous_remote_name),
    status = 'pending',
    attempts = 0,
    next_attempt_at = now(),
    lease_until = NULL,
    last_error = NULL,
    updated_at = now()
RETURNING *;

-- name: ClaimCameraZoneSyncJobs :many
UPDATE camera_zone_sync_jobs AS job
SET status = 'processing', attempts = job.attempts + 1,
    lease_until = now() + make_interval(secs => @lease_seconds::float8), updated_at = now()
WHERE job.zone_id IN (
    SELECT queued.zone_id FROM camera_zone_sync_jobs AS queued
    WHERE queued.attempts < @max_attempts
      AND ((queued.status = 'pending' AND queued.next_attempt_at <= now())
        OR (queued.status = 'processing' AND queued.lease_until <= now()))
    ORDER BY queued.next_attempt_at, queued.created_at
    LIMIT @max_rows
    FOR UPDATE SKIP LOCKED
)
RETURNING job.*;

-- name: GetCameraZoneSyncJob :one
SELECT * FROM camera_zone_sync_jobs WHERE zone_id = @zone_id;

-- name: LockCameraZoneSync :exec
SELECT pg_advisory_xact_lock(hashtextextended(@zone_id::text, 0));

-- name: MarkCameraZoneSyncJob :exec
UPDATE camera_zone_sync_jobs SET status = @status, last_error = sqlc.narg('last_error'),
    last_applied = coalesce(sqlc.narg('last_applied'), last_applied),
    previous_remote_name = CASE WHEN @status = 'synced' THEN NULL ELSE previous_remote_name END,
    next_attempt_at = @next_attempt_at, lease_until = NULL, updated_at = now()
WHERE zone_id = @zone_id AND desired_revision = @desired_revision;

-- name: RetryCameraZoneSyncJob :execrows
WITH retried AS (
    UPDATE camera_zone_sync_jobs AS job
    SET status = 'pending', attempts = 0, next_attempt_at = now(), lease_until = NULL,
        last_error = NULL, actor_user_id = @actor_user_id, updated_at = now()
    WHERE job.zone_id = @zone_id AND job.tenant_id = @tenant_id
      AND (job.status IN ('failed', 'conflict')
        OR (job.status = 'processing' AND job.attempts >= 8 AND job.lease_until <= now()))
    RETURNING job.zone_id, job.tenant_id
)
UPDATE camera_zones AS zone SET sync_status = 'pending', sync_error = NULL, updated_at = now()
FROM retried WHERE zone.id = retried.zone_id AND zone.tenant_id = retried.tenant_id;

-- name: GetCameraZoneName :one
SELECT name FROM camera_zones WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(zone_id) AND deleted_at IS NULL;

-- name: UpdateCameraZoneSyncState :execrows
UPDATE camera_zones SET sync_status = @sync_status, sync_error = sqlc.narg('sync_error'),
    sync_revision = @sync_revision, last_applied = coalesce(sqlc.narg('last_applied'), last_applied), updated_at = now()
WHERE id = @zone_id AND tenant_id = @tenant_id;

-- name: SetCameraZoneDesiredSync :exec
UPDATE camera_zones SET sync_status = 'pending', sync_error = NULL,
    sync_revision = sync_revision + 1, updated_at = now()
WHERE id = @zone_id AND tenant_id = @tenant_id;

-- name: GetCameraZoneSyncRevision :one
SELECT sync_revision FROM camera_zones WHERE id = @zone_id AND tenant_id = @tenant_id;

-- name: GetCameraZoneLastApplied :one
SELECT last_applied FROM camera_zones WHERE id = @zone_id AND tenant_id = @tenant_id;

-- name: SetCameraZoneSyncConflict :exec
UPDATE camera_zones SET sync_status = 'conflict', sync_error = @sync_error, updated_at = now()
WHERE id = @zone_id AND tenant_id = @tenant_id;

-- name: DeleteCameraZoneForSync :one
UPDATE camera_zones SET deleted_at = now(), sync_status = 'pending',
    sync_revision = sync_revision + 1, updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND camera_id = @camera_id AND deleted_at IS NULL
RETURNING id, tenant_id, camera_id, name, points, settings, created_at, updated_at, sync_status, sync_revision, last_applied, sync_error, deleted_at;

-- name: GetCameraZoneSyncTarget :one
SELECT z.id, z.tenant_id, z.camera_id, z.name, z.points, z.settings, z.sync_status, z.sync_revision,
       z.last_applied, z.deleted_at, c.remote_name, c.server_id, s.site_id, s.base_url, s.auth_mode,
       s.username, s.password_sealed, s.tls_skip_verify
FROM camera_zones z
JOIN cameras c ON c.id = z.camera_id AND c.tenant_id = z.tenant_id
JOIN frigate_servers s ON s.id = c.server_id AND s.tenant_id = c.tenant_id
WHERE z.id = @zone_id AND z.tenant_id = @tenant_id;
