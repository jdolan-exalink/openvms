-- name: InsertAudit :exec
INSERT INTO audit_log (tenant_id, actor_id, actor_name, action, target_type, target_id, request_id, ip, details)
VALUES (@tenant_id, @actor_id, @actor_name, @action, @target_type, @target_id, @request_id, @ip, @details);

-- name: ListAudit :many
SELECT * FROM audit_log
WHERE (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id'))
  AND (@all_tenants::bool OR tenant_id IS NOT NULL)
  AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action'))
  AND (sqlc.narg('actor_id')::uuid IS NULL OR actor_id = sqlc.narg('actor_id'))
  AND (sqlc.narg('from_time')::timestamptz IS NULL OR occurred_at >= sqlc.narg('from_time'))
  AND (sqlc.narg('to_time')::timestamptz IS NULL OR occurred_at < sqlc.narg('to_time'))
  AND (sqlc.narg('before_id')::bigint IS NULL OR id < sqlc.narg('before_id'))
ORDER BY id DESC
LIMIT @max_rows;
