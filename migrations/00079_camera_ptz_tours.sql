-- +goose Up
-- PTZ tours are VMS-owned: an ordered sequence of device presets with a dwell time and speed per
-- step. They are persisted per camera; running them is a separate concern (the device or a
-- runner), the row only describes the sequence.
CREATE TABLE camera_ptz_tours (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    camera_id uuid NOT NULL,
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 128),
    -- [{"preset_token": "1", "preset_name": "Gate", "dwell_seconds": 10, "speed": 0.5}, ...]
    steps jsonb NOT NULL CHECK (jsonb_typeof(steps) = 'array' AND jsonb_array_length(steps) BETWEEN 1 AND 64),
    -- 0 means repeat until stopped.
    repeat_count integer NOT NULL DEFAULT 0 CHECK (repeat_count BETWEEN 0 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT camera_ptz_tours_camera_tenant_fk FOREIGN KEY (camera_id, tenant_id)
        REFERENCES cameras (id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT camera_ptz_tours_camera_name_unique UNIQUE (camera_id, name)
);

CREATE INDEX camera_ptz_tours_tenant_camera_idx ON camera_ptz_tours (tenant_id, camera_id, created_at, id);

ALTER TABLE camera_ptz_tours ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_ptz_tours FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON camera_ptz_tours
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS camera_ptz_tours;
