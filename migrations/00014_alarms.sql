-- +goose Up
-- S2-3a: alarms inbox data. An alarm is the operator-facing work item opened for an
-- alert-severity event; its state machine (open -> acknowledged -> resolved) is independent of
-- events.reviewed. `source` leaves room for rule-created alarms (S2-6) without a schema change:
-- one event may back at most one alarm per source.
CREATE TABLE alarms (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants (id),
    site_id          uuid NOT NULL REFERENCES sites (id),
    camera_id        uuid NOT NULL REFERENCES cameras (id),
    event_id         uuid NOT NULL REFERENCES events (id),
    source           text NOT NULL DEFAULT 'event' CHECK (source IN ('event', 'rule')),
    status           text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'acknowledged', 'resolved')),
    assigned_to      uuid REFERENCES users (id),
    acknowledged_by  uuid REFERENCES users (id),
    acknowledged_at  timestamptz,
    resolved_by      uuid REFERENCES users (id),
    resolved_at      timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT alarms_event_source_key UNIQUE (event_id, source)
);
-- Inbox listing: a tenant's alarms by state, newest first.
CREATE INDEX alarms_inbox_idx ON alarms (tenant_id, status, created_at DESC);
CREATE INDEX alarms_camera_idx ON alarms (camera_id, created_at DESC);
CREATE INDEX alarms_assigned_idx ON alarms (assigned_to) WHERE assigned_to IS NOT NULL;

ALTER TABLE alarms ENABLE ROW LEVEL SECURITY;
ALTER TABLE alarms FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON alarms USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- Single-row deployment setting: the instant alarms were enabled. There is no backfill, so the
-- syncer only opens alarms for events that started at or after this instant; the migration
-- itself creates no alarms. Platform-wide (not per tenant), hence no RLS.
CREATE TABLE alarm_settings (
    singleton   boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    enabled_at  timestamptz NOT NULL
);
INSERT INTO alarm_settings (singleton, enabled_at) VALUES (true, now());

-- +goose Down
DROP TABLE IF EXISTS alarm_settings;
DROP TABLE IF EXISTS alarms;
