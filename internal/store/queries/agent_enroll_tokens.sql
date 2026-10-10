-- name: UpsertAgentEnrollToken :one
-- A server keeps one live token: the partial unique index makes a concurrent or repeated
-- creation replace the unused row instead of adding a second. expires_at comes from the
-- database clock, the same one ConsumeAgentEnrollToken checks it against.
INSERT INTO agent_enroll_tokens (tenant_id, server_id, token_hash, expires_at, created_by)
VALUES (@tenant_id, @server_id, @token_hash, now() + make_interval(secs => @ttl_seconds::float8), @created_by)
ON CONFLICT (server_id) WHERE used_at IS NULL DO UPDATE
SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at,
    created_by = EXCLUDED.created_by, created_at = now()
RETURNING expires_at;

-- name: ConsumeAgentEnrollToken :one
-- Single statement so two redemptions of the same token cannot both succeed. Run
-- all-tenants: the tenant is only known once the token resolves.
UPDATE agent_enroll_tokens SET used_at = now()
WHERE token_hash = @token_hash AND used_at IS NULL AND expires_at > now()
RETURNING tenant_id, server_id;
