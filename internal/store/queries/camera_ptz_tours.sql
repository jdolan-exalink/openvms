-- name: ListCameraPTZTours :many
SELECT id, tenant_id, camera_id, name, steps, repeat_count, created_at, updated_at
FROM camera_ptz_tours
WHERE tenant_id = @tenant_id AND camera_id = @camera_id
ORDER BY created_at, id;

-- name: CreateCameraPTZTour :one
INSERT INTO camera_ptz_tours (tenant_id, camera_id, name, steps, repeat_count)
VALUES (@tenant_id, @camera_id, @name, @steps, @repeat_count)
RETURNING id, tenant_id, camera_id, name, steps, repeat_count, created_at, updated_at;

-- name: UpdateCameraPTZTour :one
UPDATE camera_ptz_tours
SET name = @name, steps = @steps, repeat_count = @repeat_count, updated_at = now()
WHERE tenant_id = @tenant_id AND camera_id = @camera_id AND id = @id
RETURNING id, tenant_id, camera_id, name, steps, repeat_count, created_at, updated_at;

-- name: DeleteCameraPTZTour :execrows
DELETE FROM camera_ptz_tours
WHERE tenant_id = @tenant_id AND camera_id = @camera_id AND id = @id;
