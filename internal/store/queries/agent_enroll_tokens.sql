-- name: DeleteUnusedAgentEnrollTokensByServer :exec
-- A server keeps one live token: creating a new one drops the older unused ones.
DELETE FROM agent_enroll_tokens
WHERE server_id = @server_id AND used_at IS NULL;

-- name: InsertAgentEnrollToken :exec
INSERT INTO agent_enroll_tokens (tenant_id, server_id, token_hash, expires_at, created_by)
VALUES (@tenant_id, @server_id, @token_hash, @expires_at, @created_by);

-- name: ConsumeAgentEnrollToken :one
-- Single statement so two redemptions of the same token cannot both succeed. Run
-- all-tenants: the tenant is only known once the token resolves.
UPDATE agent_enroll_tokens SET used_at = now()
WHERE token_hash = @token_hash AND used_at IS NULL AND expires_at > now()
RETURNING tenant_id, server_id;
