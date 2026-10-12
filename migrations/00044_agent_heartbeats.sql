-- +goose Up
-- What an enrolled edge agent last reported. One row per server (server_agents and
-- frigate_servers are 1:1 with the agent), written by the agent listener on every heartbeat
-- and read next to the server. It lives in its own table rather than on server_agents
-- because that row exists only for SSH-provisioned agents and its version column records
-- what the installer pushed, while this is what the running agent says about itself.
--
-- last_seen_at is the database clock (never the agent's), so a skewed agent clock cannot
-- make a server look alive. status is the agent-reported operational status; whether the
-- agent is still reporting is decided by comparing last_seen_at with now. The composite
-- foreign key ties the server to the tenant that owns it (frigate_servers_id_tenant_unique),
-- exactly like agent_certificates.
CREATE TABLE agent_heartbeats (
    server_id    uuid PRIMARY KEY,
    tenant_id    uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    version      text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'unknown'
        CHECK (status IN ('unknown', 'online', 'degraded', 'offline', 'maintenance')),
    CONSTRAINT agent_heartbeats_server_tenant_fk
        FOREIGN KEY (server_id, tenant_id)
        REFERENCES frigate_servers (id, tenant_id) ON DELETE CASCADE
);

ALTER TABLE agent_heartbeats ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_heartbeats FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_heartbeats
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS agent_heartbeats;
