-- +goose Up
-- Alarm lifecycle extension: add assigned, investigating, closed states,
-- closed_by/closed_at columns, and alarm_transitions history.
ALTER TABLE alarms DROP CONSTRAINT alarms_status_check;
ALTER TABLE alarms ADD CONSTRAINT alarms_status_check
    CHECK (status IN ('open', 'acknowledged', 'assigned', 'investigating', 'resolved', 'closed'));

ALTER TABLE alarms
    ADD COLUMN closed_by uuid REFERENCES users (id),
    ADD COLUMN closed_at timestamptz;

CREATE TABLE alarm_transitions (
    id bigserial PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    alarm_id uuid NOT NULL REFERENCES alarms (id) ON DELETE CASCADE,
    from_status text,
    to_status text,
    actor_id uuid REFERENCES users (id),
    comment text NOT NULL DEFAULT '',
    at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX alarm_transitions_alarm_idx ON alarm_transitions (alarm_id, at);

ALTER TABLE alarm_transitions ENABLE ROW LEVEL SECURITY;
ALTER TABLE alarm_transitions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON alarm_transitions
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
UPDATE alarms SET status = 'acknowledged' WHERE status IN ('assigned', 'investigating');
UPDATE alarms SET status = 'resolved' WHERE status = 'closed';

ALTER TABLE alarms DROP CONSTRAINT alarms_status_check;
ALTER TABLE alarms ADD CONSTRAINT alarms_status_check
    CHECK (status IN ('open', 'acknowledged', 'resolved'));

ALTER TABLE alarms
    DROP COLUMN IF EXISTS closed_by,
    DROP COLUMN IF EXISTS closed_at;

DROP TABLE IF EXISTS alarm_transitions;
