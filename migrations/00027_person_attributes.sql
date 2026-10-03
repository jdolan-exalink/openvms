-- +goose Up
-- Person clothing is estimated from the same snapshot the vehicle worker reads.
-- Existing vehicle colors are recomputed with the pixel-count sampler. Historical
-- jobs do not raise alarms again.

UPDATE vehicle_attribute_jobs
SET status = 'pending', attempts = 0, reevaluate = false, last_error = '', updated_at = now()
WHERE status IN ('completed', 'failed');

CREATE TABLE person_attribute_jobs (
    event_id    uuid PRIMARY KEY REFERENCES events (id) ON DELETE CASCADE,
    tenant_id   uuid NOT NULL REFERENCES tenants (id),
    status      text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    attempts    int NOT NULL DEFAULT 0,
    reevaluate  boolean NOT NULL DEFAULT true,
    last_error  text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX person_attribute_jobs_pending_idx ON person_attribute_jobs (created_at) WHERE status = 'pending';

ALTER TABLE person_attribute_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE person_attribute_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON person_attribute_jobs
    USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

CREATE TABLE person_attributes (
    event_id                  uuid PRIMARY KEY REFERENCES events (id) ON DELETE CASCADE,
    tenant_id                 uuid NOT NULL REFERENCES tenants (id),
    site_id                   uuid NOT NULL REFERENCES sites (id),
    camera_id                 uuid NOT NULL REFERENCES cameras (id),
    upper_color               text NOT NULL DEFAULT 'unknown',
    upper_color_confidence    real NOT NULL DEFAULT 0,
    lower_color               text NOT NULL DEFAULT 'unknown',
    lower_color_confidence    real NOT NULL DEFAULT 0,
    color_quality             text NOT NULL DEFAULT 'low' CHECK (color_quality IN ('good', 'medium', 'low')),
    model_name                text NOT NULL DEFAULT '',
    model_version             text NOT NULL DEFAULT '',
    processed_at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX person_attributes_camera_idx ON person_attributes (camera_id, processed_at DESC);

ALTER TABLE person_attributes ENABLE ROW LEVEL SECURITY;
ALTER TABLE person_attributes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON person_attributes
    USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

INSERT INTO person_attribute_jobs (event_id, tenant_id, reevaluate)
SELECT e.id, e.tenant_id, false
FROM events e
WHERE e.labels && ARRAY['person']::text[]
ON CONFLICT (event_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS person_attributes;
DROP TABLE IF EXISTS person_attribute_jobs;
