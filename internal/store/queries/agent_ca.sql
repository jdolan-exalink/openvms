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
-- A revoked_at in the future is a grace period after a renewal; revoking ends it now. A date
-- already in the past keeps its first value.
UPDATE agent_certificates SET revoked_at = now()
WHERE serial = @serial AND (revoked_at IS NULL OR revoked_at > now());

-- name: RevokeAgentCertificatesByServer :execrows
UPDATE agent_certificates SET revoked_at = now()
WHERE server_id = @server_id AND (revoked_at IS NULL OR revoked_at > now());

-- name: LockRenewableAgentCertificate :one
-- The certificate an agent presents to renew: it must belong to the server and tenant, and not
-- be revoked yet (a revocation in the future is a grace period and still counts). Locked so
-- concurrent renewals with the same certificate run one after the other. renewable is the
-- database-clock test for the minimum age: half of the certificate's lifetime has passed.
SELECT (now() >= not_before + (not_after - not_before) / 2)::boolean AS renewable
FROM agent_certificates
WHERE serial = @serial AND server_id = @server_id AND tenant_id = @tenant_id
  AND (revoked_at IS NULL OR revoked_at > now())
FOR UPDATE;

-- name: SupersedeAgentCertificates :execrows
-- Ends the server's other live certificates grace seconds from now, unless they already end
-- sooner. Expired ones are left alone.
UPDATE agent_certificates SET revoked_at = now() + make_interval(secs => @grace_seconds::float8)
WHERE server_id = @server_id AND tenant_id = @tenant_id AND serial <> @keep_serial
  AND not_after > now()
  AND (revoked_at IS NULL OR revoked_at > now() + make_interval(secs => @grace_seconds::float8));
