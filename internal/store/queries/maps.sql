-- name: ListMapRegions :many
SELECT * FROM map_regions
WHERE tenant_id = @tenant_id
ORDER BY name ASC;

-- name: GetMapRegion :one
SELECT * FROM map_regions
WHERE id = @id AND tenant_id = @tenant_id;

-- name: CreateMapRegion :one
INSERT INTO map_regions (tenant_id, parent_id, name)
VALUES (@tenant_id, @parent_id, @name)
RETURNING *;

-- name: DeleteMapRegion :exec
DELETE FROM map_regions
WHERE id = @id AND tenant_id = @tenant_id;

-- name: UpdateSiteGeo :one
UPDATE sites
SET lat = coalesce(sqlc.narg('lat'), lat),
    lng = coalesce(sqlc.narg('lng'), lng),
    default_zoom = coalesce(sqlc.narg('default_zoom'), default_zoom),
    region_id = coalesce(sqlc.narg('region_id'), region_id),
    updated_at = now()
WHERE id = @id AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id')) AND deleted_at IS NULL
RETURNING id, tenant_id, name, lat, lng, default_zoom, region_id, updated_at;

-- name: GetSiteGeo :one
SELECT id, tenant_id, name, lat, lng, default_zoom, region_id
FROM sites
WHERE id = @id AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id')) AND deleted_at IS NULL;

-- name: ListSitesGeo :many
SELECT id, tenant_id, name, lat, lng, default_zoom, region_id
FROM sites
WHERE tenant_id = @tenant_id AND deleted_at IS NULL
ORDER BY name ASC;

-- name: ListMapBuildingsBySite :many
SELECT * FROM map_buildings
WHERE site_id = @site_id AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id')) AND deleted_at IS NULL
ORDER BY name ASC;

-- name: GetMapBuilding :one
SELECT * FROM map_buildings
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL;

-- name: CreateMapBuilding :one
INSERT INTO map_buildings (tenant_id, site_id, name, footprint, lat, lng)
VALUES (@tenant_id, @site_id, @name, @footprint, @lat, @lng)
RETURNING *;

-- name: UpdateMapBuilding :one
UPDATE map_buildings
SET name = coalesce(sqlc.narg('name'), name),
    footprint = coalesce(sqlc.narg('footprint'), footprint),
    lat = coalesce(sqlc.narg('lat'), lat),
    lng = coalesce(sqlc.narg('lng'), lng),
    revision = nextval('map_revision'),
    updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL
RETURNING *;

-- name: DeleteMapBuilding :exec
UPDATE map_buildings
SET deleted_at = now(), revision = nextval('map_revision')
WHERE id = @id AND tenant_id = @tenant_id;

-- name: ListMapFloorsByBuilding :many
SELECT * FROM map_floors
WHERE building_id = @building_id AND tenant_id = @tenant_id AND deleted_at IS NULL
ORDER BY ordinal ASC;

-- name: GetMapFloor :one
SELECT * FROM map_floors
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL;

-- name: CreateMapFloor :one
INSERT INTO map_floors (
    tenant_id, building_id, name, ordinal, plan_key, plan_content_type, plan_width_px, plan_height_px, georef
)
VALUES (
    @tenant_id, @building_id, @name, @ordinal, @plan_key, @plan_content_type, @plan_width_px, @plan_height_px, @georef
)
RETURNING *;

-- name: UpdateMapFloor :one
UPDATE map_floors
SET name = coalesce(sqlc.narg('name'), name),
    ordinal = coalesce(sqlc.narg('ordinal'), ordinal),
    plan_key = coalesce(sqlc.narg('plan_key'), plan_key),
    plan_content_type = coalesce(sqlc.narg('plan_content_type'), plan_content_type),
    plan_width_px = coalesce(sqlc.narg('plan_width_px'), plan_width_px),
    plan_height_px = coalesce(sqlc.narg('plan_height_px'), plan_height_px),
    georef = coalesce(sqlc.narg('georef'), georef),
    revision = nextval('map_revision'),
    updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL
RETURNING *;

-- name: DeleteMapFloor :exec
UPDATE map_floors
SET deleted_at = now(), revision = nextval('map_revision')
WHERE id = @id AND tenant_id = @tenant_id;

-- name: ListMapDevicesBySite :many
SELECT * FROM map_devices
WHERE site_id = @site_id AND tenant_id = @tenant_id AND deleted_at IS NULL
ORDER BY name ASC;

-- name: GetMapDevice :one
SELECT * FROM map_devices
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL;

-- name: CreateMapDevice :one
INSERT INTO map_devices (tenant_id, site_id, kind, name, status, props)
VALUES (@tenant_id, @site_id, @kind, @name, @status, @props)
RETURNING *;

-- name: UpdateMapDevice :one
UPDATE map_devices
SET name = coalesce(sqlc.narg('name'), name),
    kind = coalesce(sqlc.narg('kind'), kind),
    status = coalesce(sqlc.narg('status'), status),
    props = coalesce(sqlc.narg('props'), props),
    updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL
RETURNING *;

-- name: DeleteMapDevice :exec
UPDATE map_devices
SET deleted_at = now()
WHERE id = @id AND tenant_id = @tenant_id;

-- name: ListMapPlacementsBySite :many
SELECT * FROM map_placements
WHERE site_id = @site_id AND tenant_id = @tenant_id;

-- name: ListMapPlacementsByFloor :many
SELECT * FROM map_placements
WHERE floor_id = @floor_id AND tenant_id = @tenant_id;

-- name: GetMapPlacement :one
SELECT * FROM map_placements
WHERE id = @id AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id'));

-- name: GetMapPlacementByEntityGeo :one
SELECT * FROM map_placements
WHERE entity_type = @entity_type AND entity_id = @entity_id AND floor_id IS NULL AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id'));

-- name: GetMapPlacementByEntityFloor :one
SELECT * FROM map_placements
WHERE entity_type = @entity_type AND entity_id = @entity_id AND floor_id = @floor_id AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id'));

-- name: UpsertGeoPlacement :one
INSERT INTO map_placements (
    tenant_id, site_id, entity_type, entity_id, floor_id,
    lat, lng, bearing_deg, fov_deg, range_m, props,
    revision, created_by, updated_by
) VALUES (
    @tenant_id, @site_id, @entity_type, @entity_id, NULL,
    @lat, @lng, @bearing_deg, @fov_deg, @range_m, @props,
    nextval('map_revision'), @user_id, @user_id
)
ON CONFLICT (entity_type, entity_id) WHERE floor_id IS NULL
DO UPDATE SET
    site_id = EXCLUDED.site_id,
    lat = EXCLUDED.lat,
    lng = EXCLUDED.lng,
    bearing_deg = EXCLUDED.bearing_deg,
    fov_deg = EXCLUDED.fov_deg,
    range_m = EXCLUDED.range_m,
    props = EXCLUDED.props,
    revision = EXCLUDED.revision,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING *;

-- name: UpsertFloorPlacement :one
INSERT INTO map_placements (
    tenant_id, site_id, entity_type, entity_id, floor_id,
    x, y, bearing_deg, fov_deg, range_m, props,
    revision, created_by, updated_by
) VALUES (
    @tenant_id, @site_id, @entity_type, @entity_id, @floor_id,
    @x, @y, @bearing_deg, @fov_deg, @range_m, @props,
    nextval('map_revision'), @user_id, @user_id
)
ON CONFLICT (entity_type, entity_id, floor_id) WHERE floor_id IS NOT NULL
DO UPDATE SET
    site_id = EXCLUDED.site_id,
    x = EXCLUDED.x,
    y = EXCLUDED.y,
    bearing_deg = EXCLUDED.bearing_deg,
    fov_deg = EXCLUDED.fov_deg,
    range_m = EXCLUDED.range_m,
    props = EXCLUDED.props,
    revision = EXCLUDED.revision,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING *;

-- name: DeleteMapPlacement :exec
DELETE FROM map_placements
WHERE id = @id AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id'));

-- name: DeletePlacementByEntity :exec
DELETE FROM map_placements
WHERE entity_type = @entity_type AND entity_id = @entity_id AND tenant_id = @tenant_id;

-- name: ListUnplacedCamerasBySite :many
SELECT c.id, c.display_name, c.remote_name, c.site_id, c.status
FROM cameras c
WHERE c.site_id = @site_id
  AND (sqlc.narg('tenant_id')::uuid IS NULL OR c.tenant_id = sqlc.narg('tenant_id'))
  AND c.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM map_placements p
      WHERE p.entity_type = 'camera' AND p.entity_id = c.id AND p.floor_id IS NULL
  )
ORDER BY c.display_name ASC;

-- name: ListSiteCamerasForImport :many
SELECT c.id, c.display_name, c.remote_name
FROM cameras c
WHERE c.site_id = @site_id
  AND (sqlc.narg('tenant_id')::uuid IS NULL OR c.tenant_id = sqlc.narg('tenant_id'))
  AND c.deleted_at IS NULL
ORDER BY c.id;

-- name: ListMapZonesBySite :many
SELECT * FROM map_zones
WHERE site_id = @site_id AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id')) AND deleted_at IS NULL
ORDER BY name ASC;

-- name: GetMapZone :one
SELECT * FROM map_zones
WHERE id = @id AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id')) AND deleted_at IS NULL;

-- name: CreateMapZone :one
INSERT INTO map_zones (
    tenant_id, site_id, floor_id, name, kind, geometry,
    min_lat, min_lng, max_lat, max_lng, style, metadata,
    revision, created_by, updated_by
) VALUES (
    @tenant_id, @site_id, @floor_id, @name, @kind, @geometry,
    @min_lat, @min_lng, @max_lat, @max_lng,
    coalesce(sqlc.narg('style')::jsonb, '{}'::jsonb),
    coalesce(sqlc.narg('metadata')::jsonb, '{}'::jsonb),
    nextval('map_revision'), @user_id, @user_id
)
RETURNING *;

-- name: UpdateMapZone :one
UPDATE map_zones
SET name = coalesce(sqlc.narg('name'), name),
    kind = coalesce(sqlc.narg('kind'), kind),
    geometry = coalesce(sqlc.narg('geometry'), geometry),
    floor_id = coalesce(sqlc.narg('floor_id'), floor_id),
    min_lat = coalesce(sqlc.narg('min_lat'), min_lat),
    min_lng = coalesce(sqlc.narg('min_lng'), min_lng),
    max_lat = coalesce(sqlc.narg('max_lat'), max_lat),
    max_lng = coalesce(sqlc.narg('max_lng'), max_lng),
    style = coalesce(sqlc.narg('style'), style),
    metadata = coalesce(sqlc.narg('metadata'), metadata),
    revision = nextval('map_revision'),
    updated_by = @user_id,
    updated_at = now()
WHERE id = @id AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id')) AND deleted_at IS NULL
RETURNING *;

-- name: DeleteMapZone :exec
UPDATE map_zones
SET deleted_at = now(), revision = nextval('map_revision'), updated_by = @user_id
WHERE id = @id AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id'));

-- name: ListMapViews :many
SELECT * FROM map_views
WHERE tenant_id = @tenant_id AND (owner_id = @owner_id OR shared = true) AND deleted_at IS NULL
ORDER BY name ASC;

-- name: GetMapView :one
SELECT * FROM map_views
WHERE id = @id AND tenant_id = @tenant_id AND (owner_id = @owner_id OR shared = true) AND deleted_at IS NULL;

-- name: CreateMapView :one
INSERT INTO map_views (tenant_id, owner_id, name, shared, state)
VALUES (@tenant_id, @owner_id, @name, @shared, @state)
RETURNING *;

-- name: DeleteMapView :exec
UPDATE map_views
SET deleted_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND owner_id = @owner_id;

-- name: GetMapUserPrefs :one
SELECT * FROM map_user_prefs
WHERE user_id = @user_id;

-- name: UpsertMapUserPrefs :one
INSERT INTO map_user_prefs (user_id, tenant_id, prefs, updated_at)
VALUES (@user_id, @tenant_id, @prefs, now())
ON CONFLICT (user_id)
DO UPDATE SET prefs = EXCLUDED.prefs, tenant_id = EXCLUDED.tenant_id, updated_at = now()
RETURNING *;

-- name: ListMapSiteOverviews :many
SELECT
    s.id,
    s.tenant_id,
    s.name,
    s.lat,
    s.lng,
    s.default_zoom,
    s.region_id,
    r.name AS region_name,
    coalesce(count(DISTINCT c.id) FILTER (WHERE c.deleted_at IS NULL), 0)::int AS camera_count,
    coalesce(count(DISTINCT c.id) FILTER (WHERE c.deleted_at IS NULL AND c.status = 'online'), 0)::int AS online_cameras,
    coalesce(count(DISTINCT c.id) FILTER (WHERE c.deleted_at IS NULL AND c.status = 'offline'), 0)::int AS offline_cameras,
    coalesce(count(DISTINCT c.id) FILTER (WHERE c.deleted_at IS NULL AND c.status = 'degraded'), 0)::int AS degraded_cameras,
    coalesce(count(DISTINCT a.id) FILTER (WHERE a.status = ANY(ARRAY['open', 'acknowledged', 'assigned', 'investigating'])), 0)::int AS alarm_count
FROM sites s
LEFT JOIN map_regions r ON r.id = s.region_id AND (sqlc.narg('tenant_id')::uuid IS NULL OR r.tenant_id = s.tenant_id)
LEFT JOIN cameras c ON c.site_id = s.id AND (sqlc.narg('tenant_id')::uuid IS NULL OR c.tenant_id = s.tenant_id) AND (sqlc.narg('camera_ids')::uuid[] IS NULL OR c.id = ANY(sqlc.narg('camera_ids')::uuid[]))
LEFT JOIN alarms a ON a.site_id = s.id AND (sqlc.narg('tenant_id')::uuid IS NULL OR a.tenant_id = s.tenant_id) AND (sqlc.narg('camera_ids')::uuid[] IS NULL OR a.camera_id = ANY(sqlc.narg('camera_ids')::uuid[]))
WHERE (sqlc.narg('tenant_id')::uuid IS NULL OR s.tenant_id = sqlc.narg('tenant_id'))
  AND s.deleted_at IS NULL
  AND s.id = ANY(@site_ids::uuid[])
GROUP BY s.id, s.tenant_id, s.name, s.lat, s.lng, s.default_zoom, s.region_id, r.name
ORDER BY s.name ASC;

-- name: ListMapFloorsBySite :many
SELECT f.*
FROM map_floors f
JOIN map_buildings b ON b.id = f.building_id AND (sqlc.narg('tenant_id')::uuid IS NULL OR b.tenant_id = f.tenant_id)
WHERE b.site_id = @site_id AND (sqlc.narg('tenant_id')::uuid IS NULL OR f.tenant_id = sqlc.narg('tenant_id')) AND f.deleted_at IS NULL AND b.deleted_at IS NULL
ORDER BY f.building_id ASC, f.ordinal ASC;

-- name: GetMapSiteRevision :one
SELECT coalesce(max(revision), 0)::bigint AS revision
FROM map_placements
WHERE site_id = @site_id AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id'));

-- name: ListMapPlacementsDetailed :many
SELECT
    p.id,
    p.tenant_id,
    p.site_id,
    p.entity_type,
    p.entity_id,
    p.floor_id,
    p.lat,
    p.lng,
    p.x,
    p.y,
    p.bearing_deg,
    p.fov_deg,
    p.range_m,
    p.props,
    p.revision,
    c.display_name AS camera_name,
    c.server_id AS camera_server_id,
    c.status AS camera_status,
    c.lpr AS camera_lpr,
    fs.name AS server_name,
    fs.status AS server_status,
    md.name AS device_name,
    md.kind AS device_kind,
    md.status AS device_status,
    coalesce(
        CASE WHEN p.entity_type = 'camera' THEN
            (SELECT count(*)::int
             FROM alarms a
             WHERE (sqlc.narg('tenant_id')::uuid IS NULL OR a.tenant_id = p.tenant_id)
               AND a.site_id = p.site_id
               AND a.camera_id = p.entity_id
               AND a.status = ANY(ARRAY['open', 'acknowledged', 'assigned', 'investigating']))
        ELSE 0 END,
        0
    )::int AS alarm_count
FROM map_placements p
LEFT JOIN cameras c ON c.id = p.entity_id AND p.entity_type = 'camera' AND (sqlc.narg('tenant_id')::uuid IS NULL OR c.tenant_id = p.tenant_id) AND c.deleted_at IS NULL
LEFT JOIN frigate_servers fs ON fs.id = p.entity_id AND p.entity_type = 'server' AND (sqlc.narg('tenant_id')::uuid IS NULL OR fs.tenant_id = p.tenant_id) AND fs.deleted_at IS NULL
LEFT JOIN map_devices md ON md.id = p.entity_id AND p.entity_type = 'device' AND (sqlc.narg('tenant_id')::uuid IS NULL OR md.tenant_id = p.tenant_id) AND md.deleted_at IS NULL
WHERE p.site_id = @site_id
  AND (sqlc.narg('tenant_id')::uuid IS NULL OR p.tenant_id = sqlc.narg('tenant_id'))
  AND (sqlc.narg('floor_id')::uuid IS NULL OR p.floor_id = sqlc.narg('floor_id'))
  AND (sqlc.narg('is_geo')::boolean IS NULL OR (sqlc.narg('is_geo') = true AND p.floor_id IS NULL) OR (sqlc.narg('is_geo') = false AND p.floor_id IS NOT NULL))
  AND (
      sqlc.narg('min_lat')::double precision IS NULL
      OR (p.lat >= sqlc.narg('min_lat') AND p.lat <= sqlc.narg('max_lat') AND p.lng >= sqlc.narg('min_lng') AND p.lng <= sqlc.narg('max_lng'))
  )
  AND (
      (p.entity_type = 'camera' AND c.id IS NOT NULL)
      OR (p.entity_type = 'server' AND fs.id IS NOT NULL)
      OR (p.entity_type = 'device' AND md.id IS NOT NULL)
  )
ORDER BY p.revision ASC;

