-- +goose Up
-- The internal CA that signs edge agent certificates. One CA per installation: the
-- agent identity (tenant and server) lives in the leaf certificate, not in the CA, so
-- the table is global and carries no row-level security. The private key is sealed
-- with the master key (AES-256-GCM) like every other stored secret.
CREATE TABLE agent_ca (
    id         smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    cert_pem   text NOT NULL,
    key_sealed bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Every certificate the CA signed, kept so a listener can reject a revoked or unknown
-- serial. serial is the lowercase hex serial number; fingerprint is the hex SHA-256 of
-- the DER certificate. The composite foreign key ties the server to the tenant that owns
-- it (frigate_servers_id_tenant_unique, added in 00034), so a certificate can never
-- carry a server under a foreign tenant; foreign-key checks bypass RLS.
CREATE TABLE agent_certificates (
    serial      text PRIMARY KEY,
    tenant_id   uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    server_id   uuid NOT NULL,
    fingerprint text NOT NULL,
    not_before  timestamptz NOT NULL,
    not_after   timestamptz NOT NULL,
    revoked_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT agent_certificates_server_tenant_fk
        FOREIGN KEY (server_id, tenant_id)
        REFERENCES frigate_servers (id, tenant_id) ON DELETE CASCADE
);

CREATE INDEX agent_certificates_server_idx ON agent_certificates (server_id, not_after DESC);

ALTER TABLE agent_certificates ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_certificates FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_certificates
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS agent_certificates;
DROP TABLE IF EXISTS agent_ca;
