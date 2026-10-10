-- name: GetAgentCA :one
SELECT id, cert_pem, key_sealed, created_at
FROM agent_ca
WHERE id = 1;

-- name: InsertAgentCAIfAbsent :execrows
-- Concurrent first callers race here; the loser inserts nothing and re-reads.
INSERT INTO agent_ca (id, cert_pem, key_sealed)
VALUES (1, @cert_pem, @key_sealed)
ON CONFLICT (id) DO NOTHING;

-- name: InsertAgentCertificate :exec
INSERT INTO agent_certificates (serial, tenant_id, server_id, fingerprint, not_before, not_after)
VALUES (@serial, @tenant_id, @server_id, @fingerprint, @not_before, @not_after);

-- name: GetAgentCertificate :one
SELECT serial, tenant_id, server_id, fingerprint, not_before, not_after, revoked_at, created_at
FROM agent_certificates
WHERE serial = @serial;

-- name: ListAgentCertificatesByServer :many
SELECT serial, tenant_id, server_id, fingerprint, not_before, not_after, revoked_at, created_at
FROM agent_certificates
WHERE server_id = @server_id
ORDER BY not_after DESC;

-- name: RevokeAgentCertificate :execrows
UPDATE agent_certificates SET revoked_at = now()
WHERE serial = @serial AND revoked_at IS NULL;

-- name: RevokeAgentCertificatesByServer :execrows
UPDATE agent_certificates SET revoked_at = now()
WHERE server_id = @server_id AND revoked_at IS NULL;
