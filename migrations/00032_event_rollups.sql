-- +goose Up
-- Hourly rollup of event counts per camera, label and severity for maps analytics and heatmaps.
CREATE TABLE event_counts_hourly (
    tenant_id uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    camera_id uuid NOT NULL REFERENCES cameras (id) ON DELETE CASCADE,
    hour timestamptz NOT NULL,
    label text NOT NULL,
    severity text NOT NULL DEFAULT '',
    n integer NOT NULL DEFAULT 0,
    PRIMARY KEY (camera_id, hour, label, severity)
);

CREATE INDEX event_counts_hourly_tenant_hour_idx ON event_counts_hourly (tenant_id, hour);

ALTER TABLE event_counts_hourly ENABLE ROW LEVEL SECURITY;
ALTER TABLE event_counts_hourly FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON event_counts_hourly
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS event_counts_hourly;
