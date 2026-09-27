-- name: ListGrantsForUser :many
-- Every grant that reaches the user directly or through an active group.
SELECT g.*
FROM permission_grants g
WHERE (g.subject_type = 'user' AND g.subject_id = @user_id)
   OR (g.subject_type = 'group' AND g.subject_id IN (
        SELECT m.group_id FROM user_group_members m
        JOIN user_groups ug ON ug.id = m.group_id AND ug.deleted_at IS NULL
        WHERE m.user_id = @user_id));

-- name: ListGrants :many
SELECT * FROM permission_grants
WHERE (sqlc.narg('subject_type')::text IS NULL OR subject_type = sqlc.narg('subject_type'))
  AND (sqlc.narg('subject_id')::uuid IS NULL OR subject_id = sqlc.narg('subject_id'))
  AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id'))
ORDER BY created_at;

-- name: GetGrant :one
SELECT * FROM permission_grants WHERE id = @id;

-- name: CreateGrant :one
INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id, created_by)
VALUES (@tenant_id, @subject_type, @subject_id, @permission, @effect, @scope_type, @scope_id, @created_by)
RETURNING *;

-- name: DeleteGrant :exec
DELETE FROM permission_grants WHERE id = @id;

-- Authorized-ID queries implement authz.Decide in SQL so list endpoints filter in the
-- database (PRD §31): a row is visible when some ALLOW covers it and no DENY does.

-- name: AuthorizedCameraIDs :many
WITH subjects AS (
    SELECT 'user'::text AS subject_type, @user_id::uuid AS subject_id
    UNION ALL
    SELECT 'group', m.group_id FROM user_group_members m
    JOIN user_groups ug ON ug.id = m.group_id AND ug.deleted_at IS NULL
    WHERE m.user_id = @user_id::uuid
), grants AS (
    SELECT g.effect, g.scope_type, g.scope_id FROM permission_grants g
    JOIN subjects s ON s.subject_type = g.subject_type AND s.subject_id = g.subject_id
    WHERE g.permission = @permission::text
), live AS (
    SELECT c.id, c.tenant_id, c.site_id, c.server_id FROM cameras c WHERE c.deleted_at IS NULL
), covering AS (
    SELECT id AS camera_id, 'platform'::text AS scope_type, NULL::uuid AS scope_id FROM live
    UNION ALL SELECT id, 'tenant', tenant_id FROM live
    UNION ALL SELECT id, 'site', site_id FROM live
    UNION ALL SELECT id, 'server', server_id FROM live
    UNION ALL SELECT id, 'camera', id FROM live
    UNION ALL SELECT m.camera_id, 'camera_group', m.group_id FROM camera_group_members m
        JOIN camera_groups cg ON cg.id = m.group_id AND cg.deleted_at IS NULL
        JOIN live ON live.id = m.camera_id
)
SELECT cv.camera_id::uuid FROM covering cv
JOIN grants g ON g.scope_type = cv.scope_type AND g.scope_id IS NOT DISTINCT FROM cv.scope_id
GROUP BY cv.camera_id
HAVING bool_or(g.effect = 'allow') AND NOT bool_or(g.effect = 'deny');

-- name: AuthorizedServerIDs :many
WITH subjects AS (
    SELECT 'user'::text AS subject_type, @user_id::uuid AS subject_id
    UNION ALL
    SELECT 'group', m.group_id FROM user_group_members m
    JOIN user_groups ug ON ug.id = m.group_id AND ug.deleted_at IS NULL
    WHERE m.user_id = @user_id::uuid
), grants AS (
    SELECT g.effect, g.scope_type, g.scope_id FROM permission_grants g
    JOIN subjects s ON s.subject_type = g.subject_type AND s.subject_id = g.subject_id
    WHERE g.permission = @permission::text
), live AS (
    SELECT s.id, s.tenant_id, s.site_id FROM frigate_servers s WHERE s.deleted_at IS NULL
), covering AS (
    SELECT id AS server_id, 'platform'::text AS scope_type, NULL::uuid AS scope_id FROM live
    UNION ALL SELECT id, 'tenant', tenant_id FROM live
    UNION ALL SELECT id, 'site', site_id FROM live
    UNION ALL SELECT id, 'server', id FROM live
)
SELECT cv.server_id::uuid FROM covering cv
JOIN grants g ON g.scope_type = cv.scope_type AND g.scope_id IS NOT DISTINCT FROM cv.scope_id
GROUP BY cv.server_id
HAVING bool_or(g.effect = 'allow') AND NOT bool_or(g.effect = 'deny');

-- name: AuthorizedSiteIDs :many
WITH subjects AS (
    SELECT 'user'::text AS subject_type, @user_id::uuid AS subject_id
    UNION ALL
    SELECT 'group', m.group_id FROM user_group_members m
    JOIN user_groups ug ON ug.id = m.group_id AND ug.deleted_at IS NULL
    WHERE m.user_id = @user_id::uuid
), grants AS (
    SELECT g.effect, g.scope_type, g.scope_id FROM permission_grants g
    JOIN subjects s ON s.subject_type = g.subject_type AND s.subject_id = g.subject_id
    WHERE g.permission = @permission::text
), live AS (
    SELECT s.id, s.tenant_id FROM sites s WHERE s.deleted_at IS NULL
), covering AS (
    SELECT id AS site_id, 'platform'::text AS scope_type, NULL::uuid AS scope_id FROM live
    UNION ALL SELECT id, 'tenant', tenant_id FROM live
    UNION ALL SELECT id, 'site', id FROM live
)
SELECT cv.site_id::uuid FROM covering cv
JOIN grants g ON g.scope_type = cv.scope_type AND g.scope_id IS NOT DISTINCT FROM cv.scope_id
GROUP BY cv.site_id
HAVING bool_or(g.effect = 'allow') AND NOT bool_or(g.effect = 'deny');
