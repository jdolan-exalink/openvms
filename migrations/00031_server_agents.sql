-- +goose Up
-- Edge agent identity for a Frigate host. The token is sealed the same way as
-- Frigate passwords (AES-256-GCM) and is never returned by the API.
CREATE TABLE server_agents (
    server_id    uuid PRIMARY KEY REFERENCES frigate_servers (id) ON DELETE CASCADE,
    tenant_id    uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    host         text NOT NULL,
    port         integer NOT NULL DEFAULT 7419,
    variant      text NOT NULL DEFAULT '',
    token_sealed bytea NOT NULL,
    version      text NOT NULL DEFAULT '',
    updated_at   timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE server_agents ENABLE ROW LEVEL SECURITY;
ALTER TABLE server_agents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON server_agents
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS server_agents;
