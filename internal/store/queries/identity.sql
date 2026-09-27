-- name: GetActorByTokenHash :one
SELECT u.id, u.tenant_id, u.username, u.display_name, t.id AS token_id
FROM api_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = @token_hash
  AND t.revoked_at IS NULL
  AND (t.expires_at IS NULL OR t.expires_at > now())
  AND u.status = 'active'
  AND u.deleted_at IS NULL;

-- name: TouchAPIToken :exec
UPDATE api_tokens SET last_used_at = now()
WHERE id = @id AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');

-- name: GetUser :one
SELECT * FROM users WHERE id = @id AND deleted_at IS NULL;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE lower(username) = lower(@username) AND deleted_at IS NULL;

-- name: CreateUser :one
INSERT INTO users (tenant_id, username, email, display_name)
VALUES (@tenant_id, @username, @email, @display_name)
RETURNING *;

-- name: CreateAPIToken :one
INSERT INTO api_tokens (user_id, name, token_hash, expires_at)
VALUES (@user_id, @name, @token_hash, @expires_at)
RETURNING id;

-- name: GetUserGroup :one
SELECT * FROM user_groups WHERE id = @id AND deleted_at IS NULL;

-- name: GetUserGroupByName :one
SELECT * FROM user_groups
WHERE tenant_id IS NOT DISTINCT FROM @tenant_id AND lower(name) = lower(@name) AND deleted_at IS NULL;

-- name: CreateUserGroup :one
INSERT INTO user_groups (tenant_id, name, description)
VALUES (@tenant_id, @name, @description)
RETURNING *;

-- name: AddUserToGroup :exec
INSERT INTO user_group_members (group_id, user_id) VALUES (@group_id, @user_id)
ON CONFLICT DO NOTHING;

-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at, ip, user_agent)
VALUES (@user_id, @token_hash, @expires_at, @ip, @user_agent)
RETURNING id;

-- name: GetActorBySessionHash :one
SELECT u.id, u.tenant_id, u.username, u.display_name, s.id AS session_id, s.last_seen_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = @token_hash
  AND s.revoked_at IS NULL
  AND s.expires_at > now()
  AND s.last_seen_at > now() - make_interval(secs => @idle_seconds::float8)
  AND u.status = 'active'
  AND u.deleted_at IS NULL;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = now()
WHERE id = @id AND last_seen_at < now() - interval '30 seconds';

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = now() WHERE token_hash = @token_hash AND revoked_at IS NULL;

-- name: RevokeUserSessions :exec
UPDATE sessions SET revoked_at = now() WHERE user_id = @user_id AND revoked_at IS NULL
  AND (sqlc.narg('keep')::uuid IS NULL OR id <> sqlc.narg('keep'));

-- name: RecordLoginFailure :one
UPDATE users SET
    failed_logins = failed_logins + 1,
    locked_until = CASE WHEN failed_logins + 1 >= @max_failures::int THEN now() + make_interval(secs => @lock_seconds::float8) ELSE locked_until END
WHERE id = @id
RETURNING failed_logins, locked_until;

-- name: RecordLoginSuccess :exec
UPDATE users SET failed_logins = 0, locked_until = NULL, last_login_at = now() WHERE id = @id;

-- name: SetPassword :exec
UPDATE users SET password_hash = @password_hash, password_changed_at = now(),
    must_change_password = @must_change_password, failed_logins = 0, locked_until = NULL, updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: SetMFASecret :exec
UPDATE users SET mfa_secret_sealed = @mfa_secret_sealed, mfa_enabled = @mfa_enabled, updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: ListUsers :many
SELECT * FROM users
WHERE deleted_at IS NULL
  AND (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id'))
  AND (@include_platform::bool OR tenant_id IS NOT NULL)
ORDER BY lower(username);

-- name: UpdateUser :exec
UPDATE users SET
    display_name = coalesce(sqlc.narg('display_name'), display_name),
    email = coalesce(sqlc.narg('email'), email),
    status = coalesce(sqlc.narg('status'), status),
    updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: SoftDeleteUser :exec
UPDATE users SET deleted_at = now(), deleted_by = @deleted_by, status = 'disabled' WHERE id = @id AND deleted_at IS NULL;

-- name: ListUserGroupIDs :many
SELECT m.group_id FROM user_group_members m
JOIN user_groups g ON g.id = m.group_id AND g.deleted_at IS NULL
WHERE m.user_id = @user_id ORDER BY m.group_id;

-- name: ListUserGroups :many
SELECT g.*, (SELECT count(*) FROM user_group_members m WHERE m.group_id = g.id)::int AS member_count
FROM user_groups g
WHERE g.deleted_at IS NULL
  AND (sqlc.narg('tenant_id')::uuid IS NULL OR g.tenant_id = sqlc.narg('tenant_id'))
  AND (@include_platform::bool OR g.tenant_id IS NOT NULL)
ORDER BY lower(g.name);

-- name: ListGroupMembers :many
SELECT user_id FROM user_group_members WHERE group_id = @group_id ORDER BY user_id;

-- name: UpdateUserGroup :exec
UPDATE user_groups SET name = @name, description = @description, updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: SoftDeleteUserGroup :exec
UPDATE user_groups SET deleted_at = now(), deleted_by = @deleted_by WHERE id = @id AND deleted_at IS NULL;

-- name: ClearGroupMembers :exec
DELETE FROM user_group_members WHERE group_id = @group_id;

-- name: RemoveUserFromGroups :exec
DELETE FROM user_group_members WHERE user_id = @user_id;
