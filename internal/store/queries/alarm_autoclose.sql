-- name: ListInactiveAlarms :many
-- Alarms nobody is working (open/acknowledged) whose last activity is older than the cutoff.
-- assigned/investigating alarms are never listed: a person owns them.
SELECT id FROM alarms
WHERE status IN ('open', 'acknowledged')
  AND last_event_at < @cutoff::timestamptz
ORDER BY last_event_at
LIMIT @limit_count
FOR UPDATE SKIP LOCKED;
