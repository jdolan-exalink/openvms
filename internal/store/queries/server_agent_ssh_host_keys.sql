-- name: ClaimServerAgentSSHHostKey :execrows
INSERT INTO server_agent_ssh_host_keys (server_id, tenant_id, host, ssh_port, fingerprint)
VALUES (@server_id, @tenant_id, @host, @ssh_port, @fingerprint)
ON CONFLICT (server_id, host, ssh_port) DO NOTHING;

-- name: GetServerAgentSSHHostKey :one
SELECT server_id, tenant_id, host, ssh_port, fingerprint
FROM server_agent_ssh_host_keys
WHERE server_id = @server_id AND host = @host AND ssh_port = @ssh_port
FOR UPDATE;
