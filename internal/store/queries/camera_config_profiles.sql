-- name: CreateCameraConfigProfile :one
INSERT INTO camera_config_profiles (tenant_id, name, description, patch, created_by, updated_by)
VALUES (@tenant_id, @name, @description, @patch, @actor, @actor)
RETURNING id, tenant_id, name, description, patch, version, created_by, created_at, updated_by, updated_at;

-- name: GetCameraConfigProfile :one
SELECT id, tenant_id, name, description, patch, version, created_by, created_at, updated_by, updated_at
FROM camera_config_profiles
WHERE id = @id;

-- name: ListCameraConfigProfiles :many
SELECT p.id, p.tenant_id, p.name, p.description, p.patch, p.version, p.created_by, p.created_at,
       p.updated_by, p.updated_at,
       (SELECT count(*) FROM camera_config_state s
          JOIN cameras c ON c.id = s.camera_id AND c.tenant_id = s.tenant_id AND c.deleted_at IS NULL
         WHERE s.profile_id = p.id)::int AS camera_count
FROM camera_config_profiles p
WHERE p.tenant_id = @tenant_id
ORDER BY lower(p.name), p.id;

-- name: UpdateCameraConfigProfile :one
-- Optimistic concurrency on version: no row means somebody edited the profile first.
UPDATE camera_config_profiles
SET name = @name, description = @description, patch = @patch, version = version + 1,
    updated_by = @actor, updated_at = now()
WHERE id = @id AND version = @expected_version
RETURNING id, tenant_id, name, description, patch, version, created_by, created_at, updated_by, updated_at;

-- name: DeleteCameraConfigProfile :execrows
DELETE FROM camera_config_profiles WHERE id = @id;

-- name: CountCameraConfigProfileCameras :one
SELECT count(*)::int FROM camera_config_state s
JOIN cameras c ON c.id = s.camera_id AND c.tenant_id = s.tenant_id AND c.deleted_at IS NULL
WHERE s.profile_id = @profile_id;

-- name: ListCameraConfigProfileCameraIDs :many
SELECT s.camera_id FROM camera_config_state s
JOIN cameras c ON c.id = s.camera_id AND c.tenant_id = s.tenant_id AND c.deleted_at IS NULL
WHERE s.profile_id = @profile_id
ORDER BY s.camera_id;

-- name: ListCameraConfigProfileCameras :many
SELECT s.camera_id, s.profile_version FROM camera_config_state s
JOIN cameras c ON c.id = s.camera_id AND c.tenant_id = s.tenant_id AND c.deleted_at IS NULL
WHERE s.profile_id = @profile_id
ORDER BY s.camera_id;
