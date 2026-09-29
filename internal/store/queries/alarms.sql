-- name: GetAlarm :one
SELECT a.id, a.tenant_id, a.site_id, a.camera_id, a.event_id, a.source, a.status,
       a.assigned_to, a.acknowledged_by, a.acknowledged_at, a.resolved_by, a.resolved_at,
       a.created_at, a.updated_at,
       s.name AS site_name,
       c.display_name AS camera_name,
       e.severity AS event_severity,
       e.start_time AS event_start_time,
       e.end_time AS event_end_time,
       e.labels AS event_labels,
       e.sub_labels AS event_sub_labels,
       COALESCE(u_assignee.display_name, '')::text AS assigned_to_name,
       COALESCE(u_ack.display_name, '')::text AS acknowledged_by_name,
       COALESCE(u_res.display_name, '')::text AS resolved_by_name
FROM alarms a
JOIN sites s ON s.id = a.site_id
JOIN cameras c ON c.id = a.camera_id
JOIN events e ON e.id = a.event_id
LEFT JOIN users u_assignee ON u_assignee.id = a.assigned_to
LEFT JOIN users u_ack ON u_ack.id = a.acknowledged_by
LEFT JOIN users u_res ON u_res.id = a.resolved_by
WHERE a.id = @id;

-- name: ListAlarms :many
SELECT a.id, a.tenant_id, a.site_id, a.camera_id, a.event_id, a.source, a.status,
       a.assigned_to, a.acknowledged_by, a.acknowledged_at, a.resolved_by, a.resolved_at,
       a.created_at, a.updated_at,
       s.name AS site_name,
       c.display_name AS camera_name,
       e.severity AS event_severity,
       e.start_time AS event_start_time,
       e.end_time AS event_end_time,
       e.labels AS event_labels,
       e.sub_labels AS event_sub_labels,
       COALESCE(u_assignee.display_name, '')::text AS assigned_to_name,
       COALESCE(u_ack.display_name, '')::text AS acknowledged_by_name,
       COALESCE(u_res.display_name, '')::text AS resolved_by_name
FROM alarms a
JOIN sites s ON s.id = a.site_id
JOIN cameras c ON c.id = a.camera_id
JOIN events e ON e.id = a.event_id
LEFT JOIN users u_assignee ON u_assignee.id = a.assigned_to
LEFT JOIN users u_ack ON u_ack.id = a.acknowledged_by
LEFT JOIN users u_res ON u_res.id = a.resolved_by
WHERE a.camera_id = ANY(@camera_ids::uuid[])
  AND (sqlc.narg('status')::text IS NULL OR a.status = sqlc.narg('status'))
  AND (sqlc.narg('site_id')::uuid IS NULL OR a.site_id = sqlc.narg('site_id'))
  AND (sqlc.narg('camera_id')::uuid IS NULL OR a.camera_id = sqlc.narg('camera_id'))
  AND (sqlc.narg('assigned_to')::uuid IS NULL OR a.assigned_to = sqlc.narg('assigned_to'))
ORDER BY a.created_at DESC
LIMIT @limit_count;

-- name: UpdateAlarmStatus :one
UPDATE alarms
SET status = @status,
    acknowledged_by = CASE WHEN @status = 'acknowledged' THEN @actor_id ELSE acknowledged_by END,
    acknowledged_at = CASE WHEN @status = 'acknowledged' THEN now() ELSE acknowledged_at END,
    resolved_by = CASE WHEN @status = 'resolved' THEN @actor_id ELSE resolved_by END,
    resolved_at = CASE WHEN @status = 'resolved' THEN now() ELSE resolved_at END,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: AssignAlarm :one
UPDATE alarms
SET assigned_to = @assigned_to,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: UsersWithCameraPermission :many
WITH live_camera AS (
    SELECT c.id, c.tenant_id, c.site_id, c.server_id FROM cameras c WHERE c.id = @camera_id::uuid AND c.deleted_at IS NULL
), covering AS (
    SELECT 'platform'::text AS scope_type, NULL::uuid AS scope_id FROM live_camera
    UNION ALL SELECT 'tenant', tenant_id FROM live_camera
    UNION ALL SELECT 'site', site_id FROM live_camera
    UNION ALL SELECT 'server', server_id FROM live_camera
    UNION ALL SELECT 'camera', id FROM live_camera
    UNION ALL SELECT 'camera_group', m.group_id FROM camera_group_members m
        JOIN camera_groups cg ON cg.id = m.group_id AND cg.deleted_at IS NULL
        JOIN live_camera ON live_camera.id = m.camera_id
), matching_grants AS (
    SELECT g.subject_type, g.subject_id, g.effect
    FROM permission_grants g
    JOIN covering cv ON cv.scope_type = g.scope_type AND cv.scope_id IS NOT DISTINCT FROM g.scope_id
    WHERE g.permission = @permission::text
), user_eval AS (
    SELECT u.id, u.username, u.display_name, mg.effect
    FROM users u
    JOIN live_camera lc ON lc.tenant_id = u.tenant_id
    JOIN matching_grants mg ON mg.subject_type = 'user' AND mg.subject_id = u.id
    WHERE u.deleted_at IS NULL
    UNION ALL
    SELECT u.id, u.username, u.display_name, mg.effect
    FROM users u
    JOIN live_camera lc ON lc.tenant_id = u.tenant_id
    JOIN user_group_members m ON m.user_id = u.id
    JOIN user_groups ug ON ug.id = m.group_id AND ug.deleted_at IS NULL
    JOIN matching_grants mg ON mg.subject_type = 'group' AND mg.subject_id = ug.id
    WHERE u.deleted_at IS NULL
)
SELECT id, username, display_name
FROM user_eval
GROUP BY id, username, display_name
HAVING bool_or(effect = 'allow') AND NOT bool_or(effect = 'deny')
ORDER BY display_name;
