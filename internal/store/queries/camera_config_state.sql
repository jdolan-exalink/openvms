-- name: GetCameraConfigState :one
SELECT camera_id, tenant_id, desired, desired_revision, applied_revision, sync_state, last_error,
       last_applied_at, updated_by, updated_at
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
          last_applied_at, updated_by, updated_at;

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
          last_applied_at, updated_by, updated_at;
