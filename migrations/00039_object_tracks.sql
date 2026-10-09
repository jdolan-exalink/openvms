-- +goose Up
-- Frigate tracked-object trajectories. Frigate keeps data.box (latest bounding box, [x, y, w, h])
-- and data.path_data (position samples) on every tracked object; the syncer used to discard
-- them. Evidence playback draws the real trajectory from this table instead of inventing one.
-- Rows are keyed like object_snapshots, by Frigate's own object id, which is what
-- events.detection_ids lists, so an event finds its tracks through (server_id, remote_object_id).
--
-- path is a JSON array of {"x": 0..1, "y": 0..1, "t": unix seconds}, oldest first, with the
-- coordinates normalized to the frame. box is a JSON array [x, y, w, h], normalized as well.
CREATE TABLE object_tracks (
    server_id         uuid NOT NULL REFERENCES frigate_servers (id),
    tenant_id         uuid NOT NULL REFERENCES tenants (id),
    camera_id         uuid NOT NULL REFERENCES cameras (id),
    remote_object_id  text NOT NULL,
    label             text NOT NULL DEFAULT '',
    zones             text[] NOT NULL DEFAULT '{}',
    box               jsonb,
    path              jsonb NOT NULL DEFAULT '[]',
    start_time        timestamptz NOT NULL,
    end_time          timestamptz,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, remote_object_id)
);

ALTER TABLE object_tracks ENABLE ROW LEVEL SECURITY;
ALTER TABLE object_tracks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON object_tracks USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS object_tracks;
