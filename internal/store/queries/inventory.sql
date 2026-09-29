-- name: ListTenants :many
SELECT * FROM tenants
WHERE deleted_at IS NULL AND (sqlc.narg('id')::uuid IS NULL OR id = sqlc.narg('id'))
ORDER BY name;

-- name: GetTenant :one
SELECT * FROM tenants WHERE id = @id AND deleted_at IS NULL;

-- name: GetTenantBySlug :one
SELECT * FROM tenants WHERE slug = @slug AND deleted_at IS NULL;

-- name: CreateTenant :one
INSERT INTO tenants (slug, name) VALUES (@slug, @name) RETURNING *;

-- name: ListSites :many
SELECT s.*,
    (SELECT count(*) FROM frigate_servers fs WHERE fs.site_id = s.id AND fs.deleted_at IS NULL)::int AS server_count
FROM sites s
WHERE s.deleted_at IS NULL
  AND s.id = ANY(@ids::uuid[])
  AND (sqlc.narg('tenant_id')::uuid IS NULL OR s.tenant_id = sqlc.narg('tenant_id'))
ORDER BY s.name;

-- name: GetSite :one
SELECT s.*,
    (SELECT count(*) FROM frigate_servers fs WHERE fs.site_id = s.id AND fs.deleted_at IS NULL)::int AS server_count
FROM sites s WHERE s.id = @id AND s.deleted_at IS NULL;

-- name: CreateSite :one
INSERT INTO sites (tenant_id, name, timezone, address)
VALUES (@tenant_id, @name, @timezone, @address)
RETURNING id;

-- name: UpdateSite :exec
UPDATE sites SET
    name = coalesce(sqlc.narg('name'), name),
    timezone = coalesce(sqlc.narg('timezone'), timezone),
    address = coalesce(sqlc.narg('address'), address),
    updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: SoftDeleteSite :exec
UPDATE sites SET deleted_at = now(), deleted_by = @deleted_by WHERE id = @id AND deleted_at IS NULL;

-- name: ListServers :many
SELECT fs.*,
    (SELECT count(*) FROM cameras c WHERE c.server_id = fs.id AND c.deleted_at IS NULL)::int AS camera_count
FROM frigate_servers fs
WHERE fs.deleted_at IS NULL
  AND fs.id = ANY(@ids::uuid[])
  AND (sqlc.narg('site_id')::uuid IS NULL OR fs.site_id = sqlc.narg('site_id'))
ORDER BY fs.name;

-- name: ListAllActiveServers :many
SELECT * FROM frigate_servers WHERE deleted_at IS NULL ORDER BY id;

-- name: GetServer :one
SELECT fs.*,
    (SELECT count(*) FROM cameras c WHERE c.server_id = fs.id AND c.deleted_at IS NULL)::int AS camera_count
FROM frigate_servers fs WHERE fs.id = @id AND fs.deleted_at IS NULL;

-- name: CreateServer :exec
INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url, auth_mode, username, password_sealed, tls_skip_verify, frigate_version, capabilities)
VALUES (@id, @tenant_id, @site_id, @name, @base_url, @auth_mode, @username, @password_sealed, @tls_skip_verify, @frigate_version, @capabilities);

-- name: UpdateServer :exec
UPDATE frigate_servers SET
    name = @name, site_id = @site_id, base_url = @base_url, auth_mode = @auth_mode, username = @username,
    password_sealed = @password_sealed, tls_skip_verify = @tls_skip_verify, updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: UpdateServerDiscovery :exec
UPDATE frigate_servers SET frigate_version = @frigate_version, capabilities = @capabilities, updated_at = now()
WHERE id = @id;

-- name: UpdateServerHealth :exec
UPDATE frigate_servers SET
    status = @status,
    last_checked_at = now(),
    last_seen_at = CASE WHEN @reachable::bool THEN now() ELSE last_seen_at END,
    last_error = @last_error,
    stats = CASE WHEN @reachable::bool THEN @stats ELSE stats END,
    frigate_version = CASE WHEN @frigate_version::text <> '' THEN @frigate_version ELSE frigate_version END
WHERE id = @id;

-- name: MoveServerCameras :exec
UPDATE cameras SET site_id = @site_id, updated_at = now() WHERE server_id = @server_id;

-- name: UpsertCamera :one
INSERT INTO cameras (tenant_id, site_id, server_id, remote_name, display_name, enabled, zones, lpr, live_stream, hq_stream)
VALUES (@tenant_id, @site_id, @server_id, @remote_name, @display_name, @enabled, @zones, @lpr, @live_stream, @hq_stream)
ON CONFLICT (server_id, remote_name) WHERE deleted_at IS NULL DO UPDATE SET
    zones = excluded.zones,
    lpr = excluded.lpr,
    live_stream = excluded.live_stream,
    hq_stream = excluded.hq_stream,
    missing_since = NULL,
    updated_at = now()
RETURNING (xmax = 0) AS inserted;

-- name: MarkMissingCameras :execrows
UPDATE cameras SET missing_since = coalesce(missing_since, now()), status = 'offline', updated_at = now()
WHERE server_id = @server_id AND deleted_at IS NULL AND NOT (remote_name = ANY(@present::text[]));

-- name: ListCameras :many
SELECT c.*,
    coalesce((SELECT array_agg(m.group_id ORDER BY m.group_id) FROM camera_group_members m
              JOIN camera_groups g ON g.id = m.group_id AND g.deleted_at IS NULL
              WHERE m.camera_id = c.id), '{}')::uuid[] AS group_ids
FROM cameras c
WHERE c.deleted_at IS NULL
  AND c.id = ANY(@ids::uuid[])
  AND (sqlc.narg('site_id')::uuid IS NULL OR c.site_id = sqlc.narg('site_id'))
  AND (sqlc.narg('server_id')::uuid IS NULL OR c.server_id = sqlc.narg('server_id'))
  AND (sqlc.narg('group_id')::uuid IS NULL OR EXISTS (
        SELECT 1 FROM camera_group_members m WHERE m.camera_id = c.id AND m.group_id = sqlc.narg('group_id')))
  AND (sqlc.narg('q')::text IS NULL
       OR c.display_name ILIKE '%' || sqlc.narg('q') || '%'
       OR c.remote_name ILIKE '%' || sqlc.narg('q') || '%')
ORDER BY c.display_name;

-- name: ListServerCameras :many
SELECT * FROM cameras WHERE server_id = @server_id AND deleted_at IS NULL;

-- name: GetCamera :one
SELECT c.*,
    coalesce((SELECT array_agg(m.group_id ORDER BY m.group_id) FROM camera_group_members m
              JOIN camera_groups g ON g.id = m.group_id AND g.deleted_at IS NULL
              WHERE m.camera_id = c.id), '{}')::uuid[] AS group_ids
FROM cameras c WHERE c.id = @id AND c.deleted_at IS NULL;

-- name: UpdateCamera :exec
UPDATE cameras SET
    display_name = coalesce(sqlc.narg('display_name'), display_name),
    enabled = coalesce(sqlc.narg('enabled'), enabled),
    default_live_quality = coalesce(sqlc.narg('default_live_quality'), default_live_quality),
    description = coalesce(sqlc.narg('description'), description),
    location = coalesce(sqlc.narg('location'), location),
    tags = coalesce(sqlc.narg('tags')::text[], tags),
    updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: UpdateCameraHealth :exec
UPDATE cameras SET status = @status, fps = @fps
WHERE server_id = @server_id AND remote_name = @remote_name AND deleted_at IS NULL AND missing_since IS NULL;

-- name: SetServerCamerasStatus :exec
UPDATE cameras SET status = @status WHERE server_id = @server_id AND deleted_at IS NULL;

-- name: ListCameraGroups :many
SELECT * FROM camera_groups WHERE deleted_at IS NULL
  AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id'))
ORDER BY name;

-- name: GetCameraGroup :one
SELECT * FROM camera_groups WHERE id = @id AND deleted_at IS NULL;

-- name: CreateCameraGroup :one
INSERT INTO camera_groups (tenant_id, name, description) VALUES (@tenant_id, @name, @description) RETURNING *;

-- name: UpdateCameraGroup :exec
UPDATE camera_groups SET name = @name, description = @description, updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: SoftDeleteCameraGroup :exec
UPDATE camera_groups SET deleted_at = now(), deleted_by = @deleted_by WHERE id = @id AND deleted_at IS NULL;

-- name: ListCameraGroupMembers :many
SELECT camera_id FROM camera_group_members WHERE group_id = @group_id ORDER BY camera_id;

-- name: ClearCameraGroupMembers :exec
DELETE FROM camera_group_members WHERE group_id = @group_id;

-- name: AddCameraGroupMembers :exec
INSERT INTO camera_group_members (group_id, camera_id, tenant_id)
SELECT @group_id, c.id, c.tenant_id FROM cameras c
WHERE c.id = ANY(@camera_ids::uuid[]) AND c.tenant_id = @tenant_id AND c.deleted_at IS NULL
ON CONFLICT DO NOTHING;

-- name: CountCamerasInTenant :one
SELECT count(*)::int FROM cameras WHERE id = ANY(@ids::uuid[]) AND tenant_id = @tenant_id AND deleted_at IS NULL;

-- name: GetServerRow :one
-- Full row with the sealed credentials, for code that connects to Frigate.
SELECT * FROM frigate_servers WHERE id = @id AND deleted_at IS NULL;

-- Hard delete of a server and everything that hangs off it. Order matters: children first,
-- because the foreign keys to frigate_servers, cameras, events and lpr_reads do not cascade.
-- The audit log is append-only and is never touched.

-- name: ListServerBlobKeys :many
SELECT k::text FROM (
    SELECT e.thumb_key AS k FROM events e WHERE e.server_id = @server_id
    UNION ALL SELECT e.preview_key FROM events e WHERE e.server_id = @server_id
    UNION ALL SELECT j.output_key FROM clip_watermark_jobs j WHERE j.server_id = @server_id
) keys WHERE k <> '';

-- name: PurgeServerClipJobs :execrows
DELETE FROM clip_watermark_jobs WHERE server_id = @server_id;

-- name: PurgeServerAlarms :execrows
DELETE FROM alarms WHERE camera_id IN (SELECT id FROM cameras WHERE server_id = @server_id);

-- name: PurgeServerExports :execrows
DELETE FROM exports WHERE server_id = @server_id;

-- name: PurgeServerLprReads :execrows
DELETE FROM lpr_reads WHERE server_id = @server_id;

-- name: PurgeServerEvents :execrows
DELETE FROM events WHERE server_id = @server_id;

-- name: PurgeServerObjectSnapshots :execrows
DELETE FROM object_snapshots WHERE server_id = @server_id;

-- name: PurgeServerSyncState :execrows
DELETE FROM event_sync_state WHERE server_id = @server_id;

-- name: PurgeServerRuleFirings :execrows
DELETE FROM rule_firings
WHERE resource_id = @server_id OR resource_id IN (SELECT id FROM cameras WHERE server_id = @server_id);

-- name: PurgeServerGrants :execrows
DELETE FROM permission_grants
WHERE (scope_type = 'server' AND scope_id = @server_id::uuid)
   OR (scope_type = 'camera' AND scope_id IN (SELECT id FROM cameras WHERE server_id = @server_id::uuid));

-- Saved views keep their shape: cells showing a deleted camera become empty.
-- name: ClearServerViewCells :execrows
UPDATE views v SET layout = jsonb_set(v.layout, '{cells}', (
    SELECT coalesce(jsonb_agg(CASE WHEN cell->>'camera_id' IN (SELECT cam.id::text FROM cameras cam WHERE cam.server_id = @server_id)
                                   THEN 'null'::jsonb ELSE cell END ORDER BY ord), '[]'::jsonb)
    FROM jsonb_array_elements(v.layout->'cells') WITH ORDINALITY AS t(cell, ord)
)), updated_at = now()
WHERE jsonb_typeof(v.layout->'cells') = 'array'
  AND EXISTS (
      SELECT 1 FROM jsonb_array_elements(v.layout->'cells') AS c(cell)
      WHERE cell->>'camera_id' IN (SELECT cam.id::text FROM cameras cam WHERE cam.server_id = @server_id));

-- Camera outages and group memberships cascade with the cameras.
-- name: PurgeServerCameras :execrows
DELETE FROM cameras WHERE server_id = @server_id;

-- name: PurgeServer :execrows
DELETE FROM frigate_servers WHERE id = @id;
