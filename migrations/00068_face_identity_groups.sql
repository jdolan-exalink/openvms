-- FACE-07.3a: manager-confirmed, tenant-scoped grouping of pending face sightings.
-- A sighting has at most one group. The API locks grouping operations per tenant so
-- merging concurrent operator decisions cannot split or lose group membership.
-- +goose Up
CREATE TABLE face_identity_groups (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT face_identity_groups_id_tenant_key UNIQUE (id, tenant_id)
);

ALTER TABLE face_sightings ADD COLUMN identity_group_id uuid;
ALTER TABLE face_sightings ADD CONSTRAINT face_sightings_identity_group_fk
    FOREIGN KEY (identity_group_id, tenant_id)
    REFERENCES face_identity_groups (id, tenant_id);
CREATE INDEX face_sightings_identity_group_idx ON face_sightings (tenant_id, identity_group_id)
    WHERE identity_group_id IS NOT NULL;

ALTER TABLE face_identity_groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE face_identity_groups FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON face_identity_groups
    USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
ALTER TABLE face_sightings DROP CONSTRAINT IF EXISTS face_sightings_identity_group_fk;
ALTER TABLE face_sightings DROP COLUMN IF EXISTS identity_group_id;
DROP TABLE IF EXISTS face_identity_groups;
