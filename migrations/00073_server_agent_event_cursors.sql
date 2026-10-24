-- Provision-ISR stage 2: central pulls LPR readings from the edge agent's durable queue. This row is
-- the pull cursor per server (the last agent sequence stored) plus the counters an operator needs to
-- see whether the pipeline works. The readings themselves go to lpr_reads, keyed by
-- (server_id, remote_event_id) so storing the same reading twice changes nothing.
-- +goose Up
CREATE TABLE server_agent_event_cursors (
    server_id      uuid PRIMARY KEY REFERENCES frigate_servers (id) ON DELETE CASCADE,
    tenant_id      uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    last_seq       bigint NOT NULL DEFAULT 0 CHECK (last_seq >= 0),
    ingested_total bigint NOT NULL DEFAULT 0,
    skipped_total  bigint NOT NULL DEFAULT 0,
    agent_lost     bigint NOT NULL DEFAULT 0,
    agent_depth    integer NOT NULL DEFAULT 0,
    last_pull_at   timestamptz,
    last_error     text NOT NULL DEFAULT '',
    updated_at     timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE server_agent_event_cursors ENABLE ROW LEVEL SECURITY;
ALTER TABLE server_agent_event_cursors FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON server_agent_event_cursors
    USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS server_agent_event_cursors;
