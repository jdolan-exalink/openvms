-- +goose Up
-- S2-11: external notification channels.
-- notification_channels are tenant-level and reusable; rules pick channels through
-- rules.actions.channel_ids. Non-secret settings live in config; every secret (webhook signing
-- secret and headers, SMTP password, Telegram bot token) is one AES-GCM sealed JSON blob in
-- secrets_sealed, never returned by the API.
-- notification_deliveries is the outbox: rule firing inserts one pending row per channel
-- destination, the worker leases due rows (next_attempt_at), sends them and records the outcome.

CREATE TABLE notification_channels (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    name           text NOT NULL,
    type           text NOT NULL CHECK (type IN ('webhook', 'email', 'whatsapp', 'telegram')),
    config         jsonb NOT NULL DEFAULT '{}'::jsonb,
    secrets_sealed bytea,
    enabled        boolean NOT NULL DEFAULT true,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

ALTER TABLE notification_channels ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_channels FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notification_channels USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- Deliveries outlive their channel and rule (history), hence SET NULL plus denormalized names.
CREATE TABLE notification_deliveries (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    channel_id      uuid REFERENCES notification_channels (id) ON DELETE SET NULL,
    channel_name    text NOT NULL,
    channel_type    text NOT NULL,
    rule_id         uuid REFERENCES rules (id) ON DELETE SET NULL,
    notification_id uuid REFERENCES notifications (id) ON DELETE SET NULL,
    destination     text NOT NULL DEFAULT '',
    payload         jsonb NOT NULL,
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts        integer NOT NULL DEFAULT 0,
    last_error      text,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz
);

CREATE INDEX idx_notification_deliveries_due ON notification_deliveries (next_attempt_at) WHERE status = 'pending';
CREATE INDEX idx_notification_deliveries_tenant ON notification_deliveries (tenant_id, created_at DESC);

ALTER TABLE notification_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_deliveries FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notification_deliveries USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS notification_deliveries;
DROP TABLE IF EXISTS notification_channels;
