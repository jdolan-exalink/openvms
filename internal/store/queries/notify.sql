-- name: ListNotificationChannels :many
SELECT * FROM notification_channels
WHERE tenant_id = @tenant_id
ORDER BY name ASC, created_at DESC;

-- name: GetNotificationChannel :one
SELECT * FROM notification_channels
WHERE id = @id AND tenant_id = @tenant_id;

-- name: CreateNotificationChannel :one
INSERT INTO notification_channels (id, tenant_id, name, type, config, secrets_sealed, enabled)
VALUES (@id, @tenant_id, @name, @type, @config, @secrets_sealed, @enabled)
RETURNING *;

-- name: UpdateNotificationChannel :one
UPDATE notification_channels
SET
  name = coalesce(sqlc.narg('name'), name),
  config = coalesce(sqlc.narg('config'), config),
  secrets_sealed = coalesce(sqlc.narg('secrets_sealed'), secrets_sealed),
  enabled = coalesce(sqlc.narg('enabled'), enabled),
  updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id
RETURNING *;

-- name: DeleteNotificationChannel :exec
DELETE FROM notification_channels
WHERE id = @id AND tenant_id = @tenant_id;

-- name: CountNotificationChannelsByIDs :one
SELECT count(*) FROM notification_channels
WHERE tenant_id = @tenant_id AND id = ANY(@ids::uuid[]);

-- name: ListEnabledNotificationChannelsByIDs :many
SELECT * FROM notification_channels
WHERE tenant_id = @tenant_id AND enabled = true AND id = ANY(@ids::uuid[]);

-- name: StripChannelFromRules :exec
UPDATE rules
SET actions = jsonb_set(
      actions, '{channel_ids}',
      coalesce((SELECT jsonb_agg(v) FROM jsonb_array_elements(actions->'channel_ids') AS v WHERE (v #>> '{}') <> @channel_id::text), '[]'::jsonb))
WHERE tenant_id = @tenant_id
  AND jsonb_typeof(actions->'channel_ids') = 'array'
  AND actions->'channel_ids' @> to_jsonb(@channel_id::text);

-- name: InsertNotificationDelivery :exec
INSERT INTO notification_deliveries (tenant_id, channel_id, channel_name, channel_type, rule_id, notification_id, destination, payload)
VALUES (@tenant_id, @channel_id, @channel_name, @channel_type, @rule_id, @notification_id, @destination, @payload);

-- name: ListNotificationDeliveries :many
SELECT * FROM notification_deliveries
WHERE tenant_id = @tenant_id
  AND (sqlc.narg('channel_id')::uuid IS NULL OR channel_id = sqlc.narg('channel_id'))
ORDER BY created_at DESC, id DESC
LIMIT @max_rows;

-- ClaimDueNotificationDeliveries leases due rows: attempts is bumped and next_attempt_at pushed
-- out by the lease, so a worker that dies mid-send only delays the row instead of losing it.
-- name: ClaimDueNotificationDeliveries :many
UPDATE notification_deliveries
SET attempts = attempts + 1,
    next_attempt_at = now() + make_interval(secs => @lease_seconds::float8)
WHERE id IN (
  SELECT d.id FROM notification_deliveries d
  WHERE d.status = 'pending' AND d.next_attempt_at <= now()
  ORDER BY d.next_attempt_at
  LIMIT @max_rows
  FOR UPDATE SKIP LOCKED)
RETURNING *;

-- name: MarkNotificationDeliverySent :exec
UPDATE notification_deliveries
SET status = 'sent', sent_at = now(), last_error = NULL
WHERE id = @id;

-- name: MarkNotificationDeliveryRetry :exec
UPDATE notification_deliveries
SET last_error = @last_error,
    next_attempt_at = now() + make_interval(secs => @delay_seconds::float8)
WHERE id = @id;

-- name: MarkNotificationDeliveryFailed :exec
UPDATE notification_deliveries
SET status = 'failed', last_error = @last_error
WHERE id = @id;

-- name: PruneNotificationDeliveries :execrows
-- Deletes terminal (sent/failed) deliveries created before the cutoff, at most max_rows per call.
-- Pending rows are never touched, whatever their age.
DELETE FROM notification_deliveries
WHERE id IN (
  SELECT d.id FROM notification_deliveries d
  WHERE d.status IN ('sent', 'failed') AND d.created_at < @cutoff::timestamptz
  ORDER BY d.created_at
  LIMIT @max_rows
  FOR UPDATE SKIP LOCKED);
