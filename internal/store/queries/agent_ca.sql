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
INSERT INTO agent_certificates (serial, tenant_id, server_id, fingerprint, not_before, not_after, parent_serial)
VALUES (@serial, @tenant_id, @server_id, @fingerprint, @not_before, @not_after, sqlc.narg('parent_serial'));

-- name: GetAgentCertificate :one
SELECT serial, tenant_id, server_id, fingerprint, not_before, not_after, revoked_at, created_at, parent_serial, first_used_at
FROM agent_certificates
WHERE serial = @serial;

-- name: ListAgentCertificatesByServer :many
SELECT serial, tenant_id, server_id, fingerprint, not_before, not_after, revoked_at, created_at, parent_serial, first_used_at
FROM agent_certificates
WHERE server_id = @server_id
ORDER BY not_after DESC;

-- name: RevokeAgentCertificate :execrows
UPDATE agent_certificates SET revoked_at = now()
WHERE serial = @serial AND revoked_at IS NULL;

-- name: RevokeAgentCertificatesByServer :execrows
UPDATE agent_certificates SET revoked_at = now()
WHERE server_id = @server_id AND revoked_at IS NULL;

-- name: LockAgentServerCertificates :exec
-- Serializes everything that changes the set of live certificates of one server (renewals and
-- first uses) for the rest of the transaction.
SELECT pg_advisory_xact_lock(hashtextextended(@server_id::text, 0));

-- name: GetAgentCertificateForRenewal :one
-- The certificate an agent presents to renew: it must belong to the server and tenant and not be
-- revoked. renewable is the database-clock test for the minimum age (half of the lifetime);
-- used_successor is true when a certificate renewed from it has already been used.
SELECT (now() >= p.not_before + (p.not_after - p.not_before) / 2)::boolean AS renewable,
    EXISTS (SELECT 1 FROM agent_certificates c WHERE c.parent_serial = p.serial AND c.first_used_at IS NOT NULL)::boolean AS used_successor
FROM agent_certificates p
WHERE p.serial = @serial AND p.server_id = @server_id AND p.tenant_id = @tenant_id AND p.revoked_at IS NULL;

-- name: RevokeUnusedAgentSuccessors :execrows
-- Revokes the presenting certificate's own pending successors: certificates renewed from
-- parent_serial that never authenticated a call (the result of an earlier attempt whose response
-- may have been lost). Certificates with another parent, or none (a fresh re-enrollment), are
-- not touched; they supersede the old chain by themselves on their first use.
UPDATE agent_certificates SET revoked_at = now()
WHERE parent_serial = @parent_serial AND server_id = @server_id AND tenant_id = @tenant_id
  AND revoked_at IS NULL AND first_used_at IS NULL;

-- name: MarkAgentCertificateUsed :execrows
UPDATE agent_certificates SET first_used_at = now()
WHERE serial = @serial AND first_used_at IS NULL AND revoked_at IS NULL;

-- name: RevokeOtherAgentCertificates :execrows
UPDATE agent_certificates SET revoked_at = now()
WHERE server_id = @server_id AND tenant_id = @tenant_id AND serial <> @keep_serial AND revoked_at IS NULL;
