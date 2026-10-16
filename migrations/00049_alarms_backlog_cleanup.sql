-- +goose Up
-- Backlog cleanup: close open alarms whose last activity is older than 24 hours as auto_closed,
-- recording a system transition and audit entry while preserving all rows and history.
-- +goose StatementBegin
DO $$
DECLARE
    closed_count bigint;
BEGIN
    WITH closed AS (
        UPDATE alarms
        SET status      = 'closed',
            auto_closed = true,
            closed_at   = COALESCE(closed_at, now()),
            updated_at  = now()
        WHERE status = 'open'
          AND COALESCE(last_event_at, first_event_at, created_at) < now() - interval '24 hours'
        RETURNING id, tenant_id
    ), transitions AS (
        INSERT INTO alarm_transitions (tenant_id, alarm_id, from_status, to_status, actor_id, comment, at)
        SELECT tenant_id, id, 'open', 'closed', NULL, 'Auto-closed by 24h backlog cleanup migration', now()
        FROM closed
    ), audits AS (
        INSERT INTO audit_log (tenant_id, actor_id, actor_name, action, target_type, target_id, details)
        SELECT tenant_id,
               NULL,
               'system',
               'ALARM_CLOSED',
               'alarm',
               id,
               jsonb_build_object(
                   'previous_status', 'open',
                   'comment', 'Auto-closed by 24h backlog cleanup migration',
                   'auto_closed', true
               )
        FROM closed
    )
    SELECT count(*) INTO closed_count FROM closed;

    RAISE NOTICE 'alarms backlog cleanup migration closed % open alarms older than 24h', closed_count;
END $$;
-- +goose StatementEnd

-- +goose Down
-- Data migration is forward-only; closed backlog rows and transitions are preserved.
SELECT 1;
