-- Per-camera switches for the analytics OpenVMS computes itself (vehicle color, clothing color).
-- A missing row means "on", which is what the worker did before this table existed. Vehicle body
-- classification keeps its own switch (body_classify_cameras). Tenant isolation is the same RLS
-- policy as the other camera-owned tables.
-- +goose Up
CREATE TABLE camera_analytics_settings (
    camera_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    module text NOT NULL CHECK (module IN ('vehicle_color', 'clothing_color')),
    enabled boolean NOT NULL,
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (camera_id, module),
    CONSTRAINT camera_analytics_settings_camera_tenant_fk FOREIGN KEY (camera_id, tenant_id)
        REFERENCES cameras (id, tenant_id) ON DELETE CASCADE
);

CREATE INDEX camera_analytics_settings_tenant_idx ON camera_analytics_settings (tenant_id);

ALTER TABLE camera_analytics_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_analytics_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON camera_analytics_settings
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS camera_analytics_settings;
