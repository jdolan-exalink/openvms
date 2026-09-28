-- name: GetTenantBranding :one
SELECT * FROM tenant_branding WHERE tenant_id = @tenant_id;

-- name: UpsertTenantBranding :one
INSERT INTO tenant_branding (tenant_id, owner_name, logo_key, logo_content_type, timezone, updated_by)
VALUES (@tenant_id, @owner_name, @logo_key, @logo_content_type, @timezone, @updated_by)
ON CONFLICT (tenant_id) DO UPDATE SET
    owner_name = EXCLUDED.owner_name,
    logo_key = EXCLUDED.logo_key,
    logo_content_type = EXCLUDED.logo_content_type,
    timezone = EXCLUDED.timezone,
    updated_at = now(),
    updated_by = EXCLUDED.updated_by
RETURNING *;
