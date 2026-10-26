-- name: GetCameraConfigState :one
SELECT camera_id, tenant_id, desired, desired_revision, applied_revision, sync_state, last_error,
       last_applied_at, updated_by, updated_at, profile_id, profile_version
FROM camera_config_state
WHERE camera_id = @camera_id;

-- name: EnsureCameraConfigState :exec
INSERT INTO camera_config_state (camera_id, tenant_id)
VALUES (@camera_id, @tenant_id)
ON CONFLICT (camera_id) DO NOTHING;

-- name: RecordCameraConfigDesired :one
-- Optimistic concurrency: only the writer that saw expected_revision wins; no row means a conflict.
UPDATE camera_config_state
SET desired = @desired, desired_revision = desired_revision + 1, sync_state = 'pending',
    last_error = '', updated_by = @updated_by, updated_at = now()
WHERE camera_id = @camera_id AND desired_revision = @expected_revision
RETURNING camera_id, tenant_id, desired, desired_revision, applied_revision, sync_state, last_error,
          last_applied_at, updated_by, updated_at, profile_id, profile_version;

-- name: FinishCameraConfigApply :one
-- Closes one apply. applied_revision only advances when the engine confirmed the change
-- (applied = true); a failed apply keeps the old one and can never leave the state synced.
UPDATE camera_config_state
SET sync_state = @sync_state, last_error = @last_error,
    applied_revision = CASE WHEN @applied::boolean THEN desired_revision ELSE applied_revision END,
    last_applied_at = CASE WHEN @applied::boolean THEN now() ELSE last_applied_at END,
    updated_at = now()
WHERE camera_id = @camera_id AND desired_revision = @desired_revision
RETURNING camera_id, tenant_id, desired, desired_revision, applied_revision, sync_state, last_error,
          last_applied_at, updated_by, updated_at, profile_id, profile_version;

-- name: SetCameraConfigProfile :one
-- Links the camera to a profile (or unlinks it with NULLs) and replaces its overrides. The
-- effective desired state of an unlink does not change the engine, so a synced camera stays
-- synced: applied_revision follows desired_revision only in that case. Optimistic concurrency
-- as RecordCameraConfigDesired.
UPDATE camera_config_state
SET profile_id = sqlc.narg('profile_id'), profile_version = sqlc.narg('profile_version'),
    desired = @desired, desired_revision = desired_revision + 1,
    applied_revision = CASE WHEN sync_state = 'synced' THEN desired_revision + 1 ELSE applied_revision END,
    updated_by = @updated_by, updated_at = now()
WHERE camera_id = @camera_id AND desired_revision = @expected_revision
RETURNING camera_id, tenant_id, desired, desired_revision, applied_revision, sync_state, last_error,
          last_applied_at, updated_by, updated_at, profile_id, profile_version;

-- name: LinkCameraConfigProfile :one
-- Records which profile (and version) the camera follows without touching its overrides or
-- revisions; the apply that follows moves the revisions through the normal pipeline.
UPDATE camera_config_state
SET profile_id = @profile_id, profile_version = @profile_version, updated_by = @updated_by, updated_at = now()
WHERE camera_id = @camera_id
RETURNING camera_id, tenant_id, desired, desired_revision, applied_revision, sync_state, last_error,
          last_applied_at, updated_by, updated_at, profile_id, profile_version;
