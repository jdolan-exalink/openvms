-- +goose Up
-- Alarms phase 2 (P2-03): optimistic locking, rule version history and dedicated rules permissions.

ALTER TABLE rules ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK (version >= 1);

-- One immutable snapshot per create/update of a rule, written in the same transaction.
CREATE TABLE rule_versions (
    rule_id    uuid NOT NULL REFERENCES rules (id) ON DELETE CASCADE,
    tenant_id  uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    version    integer NOT NULL CHECK (version >= 1),
    snapshot   jsonb NOT NULL,
    changed_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (rule_id, version)
);

CREATE INDEX idx_rule_versions_tenant_rule ON rule_versions (tenant_id, rule_id, version DESC);

ALTER TABLE rule_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE rule_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON rule_versions USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- Baseline snapshot (version 1) for rules that already exist.
INSERT INTO rule_versions (rule_id, tenant_id, version, snapshot, created_at)
SELECT r.id, r.tenant_id, r.version,
       jsonb_build_object(
           'id', r.id, 'tenant_id', r.tenant_id, 'name', r.name, 'trigger_type', r.trigger_type,
           'conditions', r.conditions, 'actions', r.actions, 'enabled', r.enabled, 'version', r.version,
           'created_at', r.created_at, 'updated_at', r.updated_at),
       r.updated_at
FROM rules r;

-- Dedicated permissions: rules.read and rules.manage are tenant-scoped, so only tenant/platform
-- grants are carried over (camera-scoped alarm grants have no rules equivalent).
-- notifications.manage and alarms.manage (allow) -> rules.manage + rules.read; alarms.view (allow) -> rules.read;
-- notifications.manage (deny) -> deny of both.
INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id, created_by)
SELECT g.tenant_id, g.subject_type, g.subject_id, p.permission, g.effect, g.scope_type, g.scope_id, g.created_by
FROM permission_grants g
JOIN LATERAL (
    VALUES ('rules.manage'), ('rules.read')
) AS p (permission) ON true
WHERE g.scope_type IN ('platform', 'tenant')
  AND ((g.permission IN ('notifications.manage', 'alarms.manage') AND g.effect = 'allow')
       OR (g.permission = 'notifications.manage' AND g.effect = 'deny'))
ON CONFLICT DO NOTHING;

INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id, created_by)
SELECT g.tenant_id, g.subject_type, g.subject_id, 'rules.read', g.effect, g.scope_type, g.scope_id, g.created_by
FROM permission_grants g
WHERE g.scope_type IN ('platform', 'tenant') AND g.permission = 'alarms.view' AND g.effect = 'allow'
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS rule_versions;
ALTER TABLE rules DROP COLUMN IF EXISTS version;
DELETE FROM permission_grants WHERE permission IN ('rules.manage', 'rules.read');
