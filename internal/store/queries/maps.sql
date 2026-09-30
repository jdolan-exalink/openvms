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
SET lat = @lat,
    lng = @lng,
    default_zoom = @default_zoom,
    region_id = @region_id,
    updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL
RETURNING id, tenant_id, name, lat, lng, default_zoom, region_id, updated_at;

-- name: GetSiteGeo :one
SELECT id, tenant_id, name, lat, lng, default_zoom, region_id
FROM sites
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL;

-- name: ListSitesGeo :many
SELECT id, tenant_id, name, lat, lng, default_zoom, region_id
FROM sites
WHERE tenant_id = @tenant_id AND deleted_at IS NULL
ORDER BY name ASC;

-- name: ListMapBuildingsBySite :many
SELECT * FROM map_buildings
WHERE site_id = @site_id AND tenant_id = @tenant_id AND deleted_at IS NULL
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
WHERE id = @id AND tenant_id = @tenant_id;

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
    revision = nextval('map_revision'),
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
    revision = nextval('map_revision'),
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING *;

-- name: DeleteMapPlacement :exec
DELETE FROM map_placements
WHERE id = @id AND tenant_id = @tenant_id;

-- name: DeletePlacementByEntity :exec
DELETE FROM map_placements
WHERE entity_type = @entity_type AND entity_id = @entity_id AND tenant_id = @tenant_id;

-- name: ListUnplacedCamerasBySite :many
SELECT c.id, c.display_name, c.remote_name, c.site_id, c.status
FROM cameras c
WHERE c.site_id = @site_id
  AND c.tenant_id = @tenant_id
  AND c.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM map_placements p
      WHERE p.entity_type = 'camera' AND p.entity_id = c.id AND p.floor_id IS NULL
  )
ORDER BY c.display_name ASC;

-- name: ListMapZonesBySite :many
SELECT * FROM map_zones
WHERE site_id = @site_id AND tenant_id = @tenant_id AND deleted_at IS NULL
ORDER BY name ASC;

-- name: GetMapZone :one
SELECT * FROM map_zones
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL;

-- name: CreateMapZone :one
INSERT INTO map_zones (
    tenant_id, site_id, floor_id, name, kind, geometry,
    min_lat, min_lng, max_lat, max_lng, style, metadata,
    created_by, updated_by
) VALUES (
    @tenant_id, @site_id, @floor_id, @name, @kind, @geometry,
    @min_lat, @min_lng, @max_lat, @max_lng, @style, @metadata,
    @user_id, @user_id
)
RETURNING *;

-- name: UpdateMapZone :one
UPDATE map_zones
SET name = coalesce(sqlc.narg('name'), name),
    kind = coalesce(sqlc.narg('kind'), kind),
    geometry = coalesce(sqlc.narg('geometry'), geometry),
    min_lat = coalesce(sqlc.narg('min_lat'), min_lat),
    min_lng = coalesce(sqlc.narg('min_lng'), min_lng),
    max_lat = coalesce(sqlc.narg('max_lat'), max_lat),
    max_lng = coalesce(sqlc.narg('max_lng'), max_lng),
    style = coalesce(sqlc.narg('style'), style),
    metadata = coalesce(sqlc.narg('metadata'), metadata),
    revision = nextval('map_revision'),
    updated_by = @user_id,
    updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL
RETURNING *;

-- name: DeleteMapZone :exec
UPDATE map_zones
SET deleted_at = now(), revision = nextval('map_revision'), updated_by = @user_id
WHERE id = @id AND tenant_id = @tenant_id;

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
