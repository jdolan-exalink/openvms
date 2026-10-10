-- +goose Up
-- One-time tokens an operator creates so an edge agent can exchange a CSR for a client
-- certificate. Only the SHA-256 of the token is stored, so a database read never yields a
-- usable token. A token is consumed with a single UPDATE (used_at), which is what makes it
-- single use. The composite foreign key ties the server to the tenant that owns it
-- (frigate_servers_id_tenant_unique), exactly like agent_certificates; the tenant is not
-- known when an agent presents a token, so redemption runs all-tenants.
CREATE TABLE agent_enroll_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    server_id  uuid NOT NULL,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT agent_enroll_tokens_server_tenant_fk
        FOREIGN KEY (server_id, tenant_id)
        REFERENCES frigate_servers (id, tenant_id) ON DELETE CASCADE
);

CREATE INDEX agent_enroll_tokens_server_idx ON agent_enroll_tokens (server_id) WHERE used_at IS NULL;

ALTER TABLE agent_enroll_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_enroll_tokens FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_enroll_tokens
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS agent_enroll_tokens;
