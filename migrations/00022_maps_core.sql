-- +goose Up
-- Maps MVP. All map data is VMS-owned; the Frigate inventory sync never writes these tables.

ALTER TABLE sites
    ADD COLUMN lat double precision CHECK (lat BETWEEN -90 AND 90),
    ADD COLUMN lng double precision CHECK (lng BETWEEN -180 AND 180),
    ADD COLUMN default_zoom real,
    ADD COLUMN region_id uuid;

CREATE SEQUENCE map_revision;

CREATE TABLE map_regions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    parent_id uuid REFERENCES map_regions (id),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE sites ADD CONSTRAINT sites_region_fk FOREIGN KEY (region_id) REFERENCES map_regions (id);

CREATE TABLE map_buildings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    site_id uuid NOT NULL REFERENCES sites (id),
    name text NOT NULL,
    footprint jsonb,
    lat double precision, lng double precision,
    revision bigint NOT NULL DEFAULT nextval('map_revision'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE TABLE map_floors (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    building_id uuid NOT NULL REFERENCES map_buildings (id),
    name text NOT NULL,
    ordinal int NOT NULL DEFAULT 0,
    plan_key text NOT NULL DEFAULT '',
    plan_content_type text NOT NULL DEFAULT '',
    plan_width_px int,
    plan_height_px int,
    georef jsonb,
    revision bigint NOT NULL DEFAULT nextval('map_revision'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    UNIQUE (building_id, ordinal)
);

CREATE TABLE map_devices (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    site_id uuid NOT NULL REFERENCES sites (id),
    kind text NOT NULL CHECK (kind IN ('sensor','door','alarm_point','lpr','label','custom')),
    name text NOT NULL,
    status text NOT NULL DEFAULT 'unknown',
    props jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE TABLE map_placements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    site_id uuid NOT NULL REFERENCES sites (id),
    entity_type text NOT NULL CHECK (entity_type IN ('camera','server','device')),
    entity_id uuid NOT NULL,
    floor_id uuid REFERENCES map_floors (id),
    lat double precision, lng double precision,
    x real, y real,
    bearing_deg real CHECK (bearing_deg >= 0 AND bearing_deg < 360),
    fov_deg real CHECK (fov_deg > 0 AND fov_deg <= 360),
    range_m real CHECK (range_m > 0),
    props jsonb NOT NULL DEFAULT '{}'::jsonb,
    revision bigint NOT NULL DEFAULT nextval('map_revision'),
    created_by uuid, updated_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((floor_id IS NULL AND lat IS NOT NULL AND lng IS NOT NULL AND x IS NULL)
        OR (floor_id IS NOT NULL AND x BETWEEN 0 AND 1 AND y BETWEEN 0 AND 1))
);

CREATE UNIQUE INDEX map_placements_geo_key ON map_placements (entity_type, entity_id) WHERE floor_id IS NULL;
CREATE UNIQUE INDEX map_placements_floor_key ON map_placements (entity_type, entity_id, floor_id) WHERE floor_id IS NOT NULL;
CREATE INDEX map_placements_site_idx ON map_placements (site_id);
CREATE INDEX map_placements_bbox_idx ON map_placements (tenant_id, lat, lng) WHERE floor_id IS NULL;
CREATE INDEX map_placements_floor_idx ON map_placements (floor_id) WHERE floor_id IS NOT NULL;

CREATE TABLE map_zones (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    site_id uuid NOT NULL REFERENCES sites (id),
    floor_id uuid REFERENCES map_floors (id),
    name text NOT NULL,
    kind text NOT NULL DEFAULT 'custom',
    geometry jsonb NOT NULL,
    min_lat double precision, min_lng double precision, max_lat double precision, max_lng double precision,
    style jsonb NOT NULL DEFAULT '{}'::jsonb,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    revision bigint NOT NULL DEFAULT nextval('map_revision'),
    created_by uuid, updated_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE INDEX map_zones_site_idx ON map_zones (site_id) WHERE deleted_at IS NULL;

CREATE TABLE map_views (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    owner_id uuid NOT NULL REFERENCES users (id),
    name text NOT NULL,
    shared boolean NOT NULL DEFAULT false,
    state jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE TABLE map_user_prefs (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    tenant_id uuid REFERENCES tenants (id),
    prefs jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE map_regions ENABLE ROW LEVEL SECURITY;
ALTER TABLE map_regions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON map_regions USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE map_buildings ENABLE ROW LEVEL SECURITY;
ALTER TABLE map_buildings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON map_buildings USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE map_floors ENABLE ROW LEVEL SECURITY;
ALTER TABLE map_floors FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON map_floors USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE map_devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE map_devices FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON map_devices USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE map_placements ENABLE ROW LEVEL SECURITY;
ALTER TABLE map_placements FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON map_placements USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE map_zones ENABLE ROW LEVEL SECURITY;
ALTER TABLE map_zones FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON map_zones USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE map_views ENABLE ROW LEVEL SECURITY;
ALTER TABLE map_views FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON map_views USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE map_user_prefs ENABLE ROW LEVEL SECURITY;
ALTER TABLE map_user_prefs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON map_user_prefs USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS map_user_prefs;
DROP TABLE IF EXISTS map_views;
DROP TABLE IF EXISTS map_zones;
DROP TABLE IF EXISTS map_placements;
DROP TABLE IF EXISTS map_devices;
DROP TABLE IF EXISTS map_floors;
DROP TABLE IF EXISTS map_buildings;
ALTER TABLE sites DROP CONSTRAINT IF EXISTS sites_region_fk;
DROP TABLE IF EXISTS map_regions;
DROP SEQUENCE IF EXISTS map_revision;
ALTER TABLE sites DROP COLUMN IF EXISTS region_id, DROP COLUMN IF EXISTS default_zoom, DROP COLUMN IF EXISTS lng, DROP COLUMN IF EXISTS lat;
