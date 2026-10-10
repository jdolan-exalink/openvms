-- +goose Up
-- Renewal bookkeeping for agent certificates. A renewal records the certificate it replaces
-- (parent_serial; NULL for an enrollment). first_used_at is set by the agent listener the
-- first time the certificate authenticates a call; at that moment the server's other
-- certificates are revoked, so a renewal supersedes the old certificate only once the agent has
-- proven it received and uses the new one. Until then the old certificate keeps working (an agent
-- that lost the renewal response simply renews again). Row-level security is unchanged.
ALTER TABLE agent_certificates
    ADD COLUMN parent_serial text,
    ADD COLUMN first_used_at timestamptz;

-- Live (unrevoked) certificates of a server: scanned when a first use revokes the server's other
-- certificates, and by the renewal's lookups of the presenting certificate.
CREATE INDEX agent_certificates_live_idx ON agent_certificates (server_id, tenant_id) WHERE revoked_at IS NULL;
-- A renewal revokes its presenting certificate's unused successors: parent_serial = P AND
-- first_used_at IS NULL AND revoked_at IS NULL.
CREATE INDEX agent_certificates_parent_idx ON agent_certificates (parent_serial) WHERE parent_serial IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS agent_certificates_parent_idx;
DROP INDEX IF EXISTS agent_certificates_live_idx;
ALTER TABLE agent_certificates
    DROP COLUMN IF EXISTS first_used_at,
    DROP COLUMN IF EXISTS parent_serial;
