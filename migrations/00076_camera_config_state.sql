-- Universal camera configuration: the desired state OpenVMS keeps per camera, as a sparse
-- normalized patch accumulated over every apply, next to the revision bookkeeping that tells
-- whether the engine confirmed it. The engine stays the source of the effective state; this
-- table only records intent and the last verification outcome. Tenant isolation is the same
-- RLS policy as the other camera-owned tables.
-- +goose Up
CREATE TABLE camera_config_state (
    camera_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    desired jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(desired) = 'object'),
    desired_revision bigint NOT NULL DEFAULT 0 CHECK (desired_revision >= 0),
    applied_revision bigint NOT NULL DEFAULT 0 CHECK (applied_revision >= 0),
    sync_state text NOT NULL DEFAULT 'synced'
        CHECK (sync_state IN ('synced', 'pending', 'drift', 'error', 'unverifiable')),
    last_error text NOT NULL DEFAULT '',
    last_applied_at timestamptz,
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT camera_config_state_camera_tenant_fk FOREIGN KEY (camera_id, tenant_id)
        REFERENCES cameras (id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT camera_config_state_applied_le_desired CHECK (applied_revision <= desired_revision)
);

CREATE INDEX camera_config_state_tenant_idx ON camera_config_state (tenant_id);

ALTER TABLE camera_config_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_config_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON camera_config_state
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS camera_config_state;
