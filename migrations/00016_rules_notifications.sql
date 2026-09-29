-- +goose Up
-- S2-6: Rules Engine and In-App Notifications
-- Automation rules allow operators to define event matching and offline thresholds
-- that trigger alarms and in-app notifications.

CREATE TABLE rules (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    name         text NOT NULL,
    trigger_type text NOT NULL CHECK (trigger_type IN ('event', 'camera_offline', 'server_offline')),
    conditions   jsonb NOT NULL DEFAULT '{}'::jsonb,
    actions      jsonb NOT NULL DEFAULT '{}'::jsonb,
    enabled      boolean NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_rules_tenant_enabled ON rules (tenant_id, enabled);

ALTER TABLE rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE rules FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON rules USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

CREATE TABLE notifications (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    user_id    uuid REFERENCES users (id) ON DELETE CASCADE,
    rule_id    uuid REFERENCES rules (id) ON DELETE SET NULL,
    title      text NOT NULL,
    body       text NOT NULL,
    link       text,
    severity   text NOT NULL DEFAULT 'info' CHECK (severity IN ('info', 'warning', 'critical')),
    read_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_notifications_tenant_user_read ON notifications (tenant_id, user_id, read_at, created_at DESC);
CREATE INDEX idx_notifications_created_at ON notifications (created_at DESC);

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notifications USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS rules;
