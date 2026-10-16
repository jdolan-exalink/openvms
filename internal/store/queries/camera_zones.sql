-- name: ListCameraZonesByCamera :many
SELECT id, tenant_id, camera_id, name, points, settings, created_at, updated_at
FROM camera_zones
WHERE tenant_id = @tenant_id AND camera_id = @camera_id
ORDER BY created_at, id;

-- name: CreateCameraZone :one
INSERT INTO camera_zones (tenant_id, camera_id, name, points, settings)
VALUES (@tenant_id, @camera_id, @name, @points, @settings)
RETURNING id, camera_id, name, points, settings, created_at, updated_at;

-- name: UpdateCameraZone :one
UPDATE camera_zones
SET name = @name, points = @points, settings = @settings, updated_at = now()
WHERE tenant_id = @tenant_id AND camera_id = @camera_id AND id = @id
RETURNING id, camera_id, name, points, settings, created_at, updated_at;

-- name: DeleteCameraZone :execrows
DELETE FROM camera_zones
WHERE tenant_id = @tenant_id AND camera_id = @camera_id AND id = @id;
