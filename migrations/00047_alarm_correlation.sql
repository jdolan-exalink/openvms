-- +goose Up
-- Alarm correlation: one alarm per ongoing situation instead of one per event.
-- correlation_key = source|rule_id-or-none|camera_id|label; at most one non-terminal alarm per key.
ALTER TABLE alarms
    ADD COLUMN correlation_key text,
    ADD COLUMN first_event_at  timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN last_event_at   timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN event_count     int         NOT NULL DEFAULT 1,
    ADD COLUMN late            boolean     NOT NULL DEFAULT false,
    ADD COLUMN auto_closed     boolean     NOT NULL DEFAULT false;

-- Existing alarms are one-event alarms: their activity window is the event's start time. A single
-- statement is fine for the expected table size (~150k rows) and is idempotent.
UPDATE alarms a
SET first_event_at = COALESCE(e.start_time, a.created_at),
    last_event_at  = COALESCE(e.start_time, a.created_at)
FROM events e
WHERE e.id = a.event_id;

CREATE INDEX alarms_activity_idx ON alarms (tenant_id, status, last_event_at DESC);
CREATE UNIQUE INDEX alarms_open_correlation_key
    ON alarms (tenant_id, correlation_key)
    WHERE status NOT IN ('resolved', 'closed') AND correlation_key IS NOT NULL;

-- Evidence: every event folded into an alarm. source mirrors alarms.source so an event still
-- backs at most one alarm per source (same rule as alarms.UNIQUE(event_id, source)).
CREATE TABLE alarm_events (
    alarm_id  uuid        NOT NULL REFERENCES alarms (id) ON DELETE CASCADE,
    event_id  uuid        NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    tenant_id uuid        NOT NULL REFERENCES tenants (id),
    source    text        NOT NULL,
    linked_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (alarm_id, event_id),
    CONSTRAINT alarm_events_event_source_key UNIQUE (event_id, source)
);

ALTER TABLE alarm_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE alarm_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON alarm_events
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

INSERT INTO alarm_events (alarm_id, event_id, tenant_id, source, linked_at)
SELECT id, event_id, tenant_id, source, created_at FROM alarms
ON CONFLICT DO NOTHING;

-- Inactivity window after which the worker closes an open/acknowledged alarm (also the window in
-- which a new event still joins an existing alarm). Platform-wide like the rest of alarm_settings.
ALTER TABLE alarm_settings ADD COLUMN auto_close_after interval NOT NULL DEFAULT interval '30 minutes';

-- +goose Down
ALTER TABLE alarm_settings DROP COLUMN IF EXISTS auto_close_after;
DROP TABLE IF EXISTS alarm_events;
DROP INDEX IF EXISTS alarms_open_correlation_key;
DROP INDEX IF EXISTS alarms_activity_idx;
ALTER TABLE alarms
    DROP COLUMN IF EXISTS auto_closed,
    DROP COLUMN IF EXISTS late,
    DROP COLUMN IF EXISTS event_count,
    DROP COLUMN IF EXISTS last_event_at,
    DROP COLUMN IF EXISTS first_event_at,
    DROP COLUMN IF EXISTS correlation_key;
