-- name: GetAlarmSettings :one
SELECT enabled_at, EXTRACT(EPOCH FROM auto_close_after)::float8 AS auto_close_seconds
FROM alarm_settings;

-- name: AlarmEventLinked :one
SELECT EXISTS (
    SELECT 1 FROM alarm_events WHERE event_id = @event_id AND source = @source
) AS linked;

-- name: FindMergeableAlarm :one
-- The non-terminal alarm of this key that still absorbs events: within the activity window, or
-- held by a person (assigned/investigating alarms keep absorbing until a human closes them).
SELECT id, status, created_at, event_count
FROM alarms
WHERE tenant_id = @tenant_id
  AND correlation_key = @correlation_key
  AND status NOT IN ('resolved', 'closed')
  AND (status IN ('assigned', 'investigating') OR last_event_at >= @cutoff::timestamptz)
FOR UPDATE;

-- name: ListStaleCorrelatedAlarms :many
-- Open/acknowledged alarm of this key that is past its window but not swept yet: it would block
-- the unique index for a new alarm of the same key. Only alarms past the cutoff: an in-window
-- alarm committed by a concurrent writer must be merged into, never closed.
SELECT id FROM alarms
WHERE tenant_id = @tenant_id
  AND correlation_key = @correlation_key
  AND status IN ('open', 'acknowledged')
  AND last_event_at < @cutoff::timestamptz;

-- name: LinkAlarmEvent :exec
INSERT INTO alarm_events (alarm_id, event_id, tenant_id, source)
VALUES (@alarm_id, @event_id, @tenant_id, @source)
ON CONFLICT DO NOTHING;

-- name: BumpAlarmActivity :one
UPDATE alarms
SET event_count = event_count + 1,
    first_event_at = LEAST(first_event_at, @at::timestamptz),
    last_event_at = GREATEST(last_event_at, @at::timestamptz),
    updated_at = now()
WHERE id = @id
RETURNING id, tenant_id, site_id, camera_id, event_id, status, event_count, updated_at;

-- name: InsertCorrelatedAlarm :one
INSERT INTO alarms (tenant_id, site_id, camera_id, event_id, source, correlation_key,
                    first_event_at, last_event_at, late)
VALUES (@tenant_id, @site_id, @camera_id, @event_id, @source, @correlation_key, @at, @at, @late)
ON CONFLICT (tenant_id, correlation_key)
    WHERE status NOT IN ('resolved', 'closed') AND correlation_key IS NOT NULL
DO NOTHING
RETURNING id, status, created_at, late;

-- name: AutoCloseAlarm :one
WITH cur AS (
    SELECT al.id, al.status FROM alarms al
    WHERE al.id = @id AND al.status IN ('open', 'acknowledged')
    FOR UPDATE OF al
)
UPDATE alarms a
SET status = 'closed', auto_closed = true, closed_at = @closed_at::timestamptz, updated_at = now()
FROM cur
WHERE a.id = cur.id
RETURNING a.id, a.tenant_id, a.site_id, a.camera_id, a.event_id, a.updated_at, cur.status AS from_status;
