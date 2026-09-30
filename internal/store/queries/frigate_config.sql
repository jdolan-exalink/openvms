-- name: InsertFrigateConfigRevision :one
INSERT INTO frigate_config_revisions (tenant_id, server_id, camera_id, actor_user_id, actor_name, kind, sections, before_yaml, after_yaml, patch)
VALUES (@tenant_id, @server_id, @camera_id, @actor_user_id, @actor_name, @kind, @sections, @before_yaml, @after_yaml, @patch)
RETURNING id, tenant_id, server_id, camera_id, actor_user_id, actor_name, kind, sections, patch, created_at;

-- name: ListFrigateConfigRevisions :many
SELECT id, tenant_id, server_id, camera_id, actor_user_id, actor_name, kind, sections, patch, created_at
FROM frigate_config_revisions
WHERE server_id = @server_id
  AND (sqlc.narg('camera_id')::uuid IS NULL OR camera_id = sqlc.narg('camera_id'))
  AND (sqlc.narg('before')::timestamptz IS NULL OR created_at < sqlc.narg('before'))
ORDER BY created_at DESC, id DESC
LIMIT @max_rows;

-- name: GetFrigateConfigRevision :one
SELECT * FROM frigate_config_revisions WHERE id = @id AND server_id = @server_id;
