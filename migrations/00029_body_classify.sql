-- +goose Up
-- Fine vehicle classification is on for every camera until a row says otherwise.
-- A missing row means enabled. Turning a server off covers all of its cameras;
-- a camera row turns that camera off even when the server stays on.
CREATE TABLE body_classify_servers (
    server_id uuid PRIMARY KEY REFERENCES frigate_servers (id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    enabled   boolean NOT NULL
);
ALTER TABLE body_classify_servers ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON body_classify_servers
    USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

CREATE TABLE body_classify_cameras (
    camera_id uuid PRIMARY KEY REFERENCES cameras (id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    enabled   boolean NOT NULL
);
ALTER TABLE body_classify_cameras ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON body_classify_cameras
    USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE body_classify_cameras;
DROP TABLE body_classify_servers;
