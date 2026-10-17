-- +goose Up
-- Camera zones are VMS-owned and kept independently from Frigate config.yml so the API can
-- assign stable IDs while preserving normalized frame coordinates.
ALTER TABLE cameras ADD CONSTRAINT cameras_id_tenant_id_key UNIQUE (id, tenant_id);

-- +goose StatementBegin
CREATE FUNCTION camera_zone_points_normalized(points jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE
    point jsonb;
BEGIN
    IF jsonb_typeof(points) <> 'array' THEN
        RETURN false;
    END IF;
    IF jsonb_array_length(points) < 3 THEN
        RETURN false;
    END IF;
    FOR point IN SELECT value FROM jsonb_array_elements(points) LOOP
        IF jsonb_typeof(point) <> 'object'
            OR jsonb_typeof(point->'x') IS DISTINCT FROM 'number'
            OR jsonb_typeof(point->'y') IS DISTINCT FROM 'number' THEN
            RETURN false;
        END IF;
        IF (point->>'x')::numeric < 0 OR (point->>'x')::numeric > 1
            OR (point->>'y')::numeric < 0 OR (point->>'y')::numeric > 1 THEN
            RETURN false;
        END IF;
    END LOOP;
    RETURN true;
END;
$$;
-- +goose StatementEnd

CREATE TABLE camera_zones (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    camera_id uuid NOT NULL,
    name text NOT NULL CHECK (name ~ '^[a-z0-9_]+$'),
    points jsonb NOT NULL CHECK (camera_zone_points_normalized(points)),
    settings jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(settings) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT camera_zones_camera_tenant_fk FOREIGN KEY (camera_id, tenant_id)
        REFERENCES cameras (id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT camera_zones_camera_name_unique UNIQUE (camera_id, name)
);

CREATE INDEX camera_zones_tenant_camera_idx ON camera_zones (tenant_id, camera_id, created_at, id);

ALTER TABLE camera_zones ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_zones FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON camera_zones
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS camera_zones;
DROP FUNCTION IF EXISTS camera_zone_points_normalized(jsonb);
ALTER TABLE cameras DROP CONSTRAINT IF EXISTS cameras_id_tenant_id_key;
