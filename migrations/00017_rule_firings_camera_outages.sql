-- +goose Up
-- S2-6b: state the rules worker needs.
-- rule_firings debounces a rule per resource (camera or server): the evaluator claims
-- (rule_id, resource_id) atomically before running actions.
-- camera_outages records when a camera was first seen offline, since cameras.status carries no
-- "since" (server outages use frigate_servers.last_seen_at instead).

CREATE TABLE rule_firings (
    rule_id     uuid NOT NULL REFERENCES rules (id) ON DELETE CASCADE,
    resource_id uuid NOT NULL,
    tenant_id   uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    fired_at    timestamptz NOT NULL,
    PRIMARY KEY (rule_id, resource_id)
);

ALTER TABLE rule_firings ENABLE ROW LEVEL SECURITY;
ALTER TABLE rule_firings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON rule_firings USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

CREATE TABLE camera_outages (
    camera_id uuid PRIMARY KEY REFERENCES cameras (id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    since     timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE camera_outages ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_outages FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON camera_outages USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS camera_outages;
DROP TABLE IF EXISTS rule_firings;
