-- +goose Up
-- Vehicle enrichment is an OpenVMS concern. Frigate still detects the object; a
-- background job reads the stored thumbnail and writes type and color here.
-- object kind stays on the row so later person attributes share the table.

CREATE TABLE vehicle_attribute_jobs (
    event_id    uuid PRIMARY KEY REFERENCES events (id) ON DELETE CASCADE,
    tenant_id   uuid NOT NULL REFERENCES tenants (id),
    status      text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    attempts    int NOT NULL DEFAULT 0,
    last_error  text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX vehicle_attribute_jobs_pending_idx ON vehicle_attribute_jobs (created_at) WHERE status = 'pending';

ALTER TABLE vehicle_attribute_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE vehicle_attribute_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON vehicle_attribute_jobs
    USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

CREATE TABLE vehicle_attributes (
    event_id                  uuid PRIMARY KEY REFERENCES events (id) ON DELETE CASCADE,
    tenant_id                 uuid NOT NULL REFERENCES tenants (id),
    site_id                   uuid NOT NULL REFERENCES sites (id),
    camera_id                 uuid NOT NULL REFERENCES cameras (id),
    vehicle_type              text NOT NULL DEFAULT 'unknown',
    vehicle_type_confidence   real NOT NULL DEFAULT 0,
    vehicle_color             text NOT NULL DEFAULT 'unknown',
    vehicle_color_confidence  real NOT NULL DEFAULT 0,
    color_quality             text NOT NULL DEFAULT 'low' CHECK (color_quality IN ('good', 'medium', 'low')),
    model_name                text NOT NULL DEFAULT '',
    model_version             text NOT NULL DEFAULT '',
    processed_at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX vehicle_attributes_type_idx ON vehicle_attributes (tenant_id, vehicle_type, processed_at DESC);
CREATE INDEX vehicle_attributes_color_idx ON vehicle_attributes (tenant_id, vehicle_color, processed_at DESC);
CREATE INDEX vehicle_attributes_camera_idx ON vehicle_attributes (camera_id, processed_at DESC);
CREATE INDEX vehicle_attributes_site_idx ON vehicle_attributes (site_id, processed_at DESC);

ALTER TABLE vehicle_attributes ENABLE ROW LEVEL SECURITY;
ALTER TABLE vehicle_attributes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON vehicle_attributes
    USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS vehicle_attributes;
DROP TABLE IF EXISTS vehicle_attribute_jobs;
