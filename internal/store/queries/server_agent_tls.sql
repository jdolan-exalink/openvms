-- name: GetServerAgentTLS :one
SELECT server_id, tenant_id, secure_port, trust_mode, ca_pem
FROM server_agent_tls
WHERE server_id = @server_id
FOR UPDATE;

-- name: UpsertServerAgentTLS :exec
INSERT INTO server_agent_tls (server_id, tenant_id, secure_port, trust_mode, ca_pem)
VALUES (@server_id, @tenant_id, @secure_port, @trust_mode, @ca_pem)
ON CONFLICT (server_id) DO UPDATE SET
    tenant_id = EXCLUDED.tenant_id,
    secure_port = EXCLUDED.secure_port,
    trust_mode = EXCLUDED.trust_mode,
    ca_pem = EXCLUDED.ca_pem;

-- name: DeleteServerAgentTLS :execrows
DELETE FROM server_agent_tls WHERE server_id = @server_id;
