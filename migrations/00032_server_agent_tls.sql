-- +goose Up
-- Optional trust material is separate from the agent bearer token. Custom CA
-- bundles are operator-provided public certificates; private keys are never stored.
ALTER TABLE server_agents
    ADD CONSTRAINT server_agents_server_tenant_unique UNIQUE (server_id, tenant_id);

CREATE TABLE server_agent_tls (
    server_id  uuid PRIMARY KEY,
    tenant_id  uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    secure_port integer NOT NULL CHECK (secure_port BETWEEN 1 AND 65535),
    trust_mode text NOT NULL CHECK (trust_mode IN ('system', 'custom')),
    ca_pem     text,
    CHECK ((trust_mode = 'system' AND ca_pem IS NULL) OR
           (trust_mode = 'custom' AND ca_pem IS NOT NULL AND length(ca_pem) > 0)),
    CONSTRAINT server_agent_tls_server_tenant_fk
        FOREIGN KEY (server_id, tenant_id)
        REFERENCES server_agents (server_id, tenant_id) ON DELETE CASCADE
);

ALTER TABLE server_agent_tls ENABLE ROW LEVEL SECURITY;
ALTER TABLE server_agent_tls FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON server_agent_tls
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS server_agent_tls;
ALTER TABLE server_agents
    DROP CONSTRAINT IF EXISTS server_agents_server_tenant_unique;
