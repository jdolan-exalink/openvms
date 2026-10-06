-- name: UpsertServerAgent :exec
INSERT INTO server_agents (server_id, tenant_id, host, port, variant, token_sealed, version)
VALUES (@server_id, @tenant_id, @host, @port, @variant, @token_sealed, @version)
ON CONFLICT (server_id) DO UPDATE SET
    tenant_id = EXCLUDED.tenant_id,
    host = EXCLUDED.host,
    port = EXCLUDED.port,
    variant = EXCLUDED.variant,
    token_sealed = EXCLUDED.token_sealed,
    version = EXCLUDED.version,
    updated_at = now();

-- name: GetServerAgent :one
SELECT server_id, tenant_id, host, port, variant, token_sealed, version, updated_at
FROM server_agents
WHERE server_id = @server_id;

-- name: GetServerAgentForUpdate :one
SELECT server_id, tenant_id, host, port, variant, token_sealed, version, updated_at
FROM server_agents
WHERE server_id = @server_id
FOR UPDATE;

-- name: UpdateServerAgentVersion :exec
UPDATE server_agents SET version = @version, updated_at = now()
WHERE server_id = @server_id;

-- name: RegisterServerAgentIfAbsent :execrows
-- Local Compose registration must not replace a previously provisioned agent token.
INSERT INTO server_agents (server_id, tenant_id, host, port, variant, token_sealed, version)
VALUES (@server_id, @tenant_id, @host, @port, @variant, @token_sealed, @version)
ON CONFLICT (server_id) DO NOTHING;
