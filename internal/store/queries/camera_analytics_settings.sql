-- name: ListCameraAnalyticsSettings :many
SELECT camera_id, tenant_id, module, enabled, updated_by, updated_at
FROM camera_analytics_settings
WHERE camera_id = @camera_id
ORDER BY module;

-- name: UpsertCameraAnalyticsSetting :exec
INSERT INTO camera_analytics_settings (camera_id, tenant_id, module, enabled, updated_by)
VALUES (@camera_id, @tenant_id, @module, @enabled, @updated_by)
ON CONFLICT (camera_id, module) DO UPDATE
SET enabled = excluded.enabled, updated_by = excluded.updated_by, updated_at = now();
