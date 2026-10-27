-- A deleted camera leaves a tombstone so the Frigate sync does not bring it back as a new row.
-- Deleting a camera is a soft delete (cameras.deleted_at); the partial unique index on live cameras
-- no longer matches it, so without this table the next sync would insert the same remote_name again.
-- retain_days is the recording window the camera had when it was deleted; the history purge of a
-- deleted camera reads it. The row goes away when the camera is restored, or with the camera itself.
-- +goose Up
CREATE TABLE camera_tombstones (
    camera_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    server_id uuid NOT NULL,
    remote_name text NOT NULL,
    retain_days integer CHECK (retain_days IS NULL OR retain_days >= 0),
    deleted_by uuid,
    deleted_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT camera_tombstones_camera_tenant_fk FOREIGN KEY (camera_id, tenant_id)
        REFERENCES cameras (id, tenant_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX camera_tombstones_server_remote_key ON camera_tombstones (server_id, remote_name);
CREATE INDEX camera_tombstones_tenant_idx ON camera_tombstones (tenant_id);

ALTER TABLE camera_tombstones ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_tombstones FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON camera_tombstones
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS camera_tombstones;
