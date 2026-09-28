-- +goose Up
-- PDW-1: owner branding for the watermark burned into plate detail downloads (photo and
-- clip) and shown as an on-screen overlay. One row per tenant; a missing row means no
-- branding has been configured yet (owner name/logo are both empty).
CREATE TABLE tenant_branding (
    tenant_id          uuid PRIMARY KEY REFERENCES tenants (id),
    owner_name         text NOT NULL DEFAULT '',
    -- Object store key of the logo (tenant/{tenant_id}/branding/logo), empty when no logo
    -- is configured. The bytes live in the object store, not in Postgres.
    logo_key           text NOT NULL DEFAULT '',
    logo_content_type  text NOT NULL DEFAULT '',
    updated_at         timestamptz NOT NULL DEFAULT now(),
    updated_by         uuid REFERENCES users (id)
);

ALTER TABLE tenant_branding ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_branding FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_branding USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS tenant_branding;
