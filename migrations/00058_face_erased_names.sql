-- FACE-03: names of purged persons. The events syncer drops these from the sub_labels of every
-- review it ingests, because Frigate keeps serving recent reviews (which still carry the name)
-- after the person was erased; without it a re-sync would write the name back. Rows older than
-- the retention window (Frigate review retention) no longer matter and are pruned by the worker.
-- Registering a new person with the same Frigate name removes the marker.
-- +goose Up
CREATE TABLE face_erased_names (
    tenant_id    uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    frigate_name text NOT NULL,
    erased_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, frigate_name)
);
CREATE INDEX face_erased_names_erased_at_idx ON face_erased_names (erased_at);
ALTER TABLE face_erased_names ENABLE ROW LEVEL SECURITY;
ALTER TABLE face_erased_names FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON face_erased_names USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS face_erased_names;
