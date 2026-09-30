-- name: ListNotifications :many
SELECT id, tenant_id, user_id, rule_id, title, body, link, severity, read_at, created_at, server_id, camera_id
FROM notifications
WHERE tenant_id = @tenant_id
  AND (user_id IS NULL OR user_id = @user_id)
  AND (sqlc.narg('unread_only')::boolean IS NOT TRUE OR read_at IS NULL)
ORDER BY created_at DESC
LIMIT @limit_count;

-- name: CountUnreadNotifications :one
SELECT count(*)::bigint
FROM notifications
WHERE tenant_id = @tenant_id
  AND (user_id IS NULL OR user_id = @user_id)
  AND read_at IS NULL;

-- name: CreateNotification :one
INSERT INTO notifications (tenant_id, user_id, rule_id, title, body, link, severity, server_id, camera_id)
VALUES (@tenant_id, @user_id, @rule_id, @title, @body, @link, @severity, @server_id, @camera_id)
RETURNING id, tenant_id, user_id, rule_id, title, body, link, severity, read_at, created_at, server_id, camera_id;

-- name: MarkNotificationRead :one
UPDATE notifications
SET read_at = now()
WHERE id = @id
  AND tenant_id = @tenant_id
  AND (user_id IS NULL OR user_id = @user_id)
RETURNING id, tenant_id, user_id, rule_id, title, body, link, severity, read_at, created_at, server_id, camera_id;

-- name: MarkAllNotificationsRead :execrows
UPDATE notifications
SET read_at = now()
WHERE tenant_id = @tenant_id
  AND (user_id IS NULL OR user_id = @user_id)
  AND read_at IS NULL;

-- name: PruneReadNotifications :execrows
-- Deletes in-app notifications that were read before the cutoff, at most max_rows per call.
-- Unread notifications are never touched.
DELETE FROM notifications
WHERE id IN (
  SELECT n.id FROM notifications n
  WHERE n.read_at IS NOT NULL AND n.read_at < @cutoff::timestamptz
  ORDER BY n.read_at
  LIMIT @max_rows
  FOR UPDATE SKIP LOCKED);
