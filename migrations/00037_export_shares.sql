-- +goose Up
CREATE TABLE export_shares (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (id),
    job_id             uuid NOT NULL REFERENCES export_jobs (id) ON DELETE CASCADE,
    share_token        varchar(64) UNIQUE NOT NULL,
    created_by         uuid NOT NULL REFERENCES users (id),
    expires_at         timestamptz,
    password_hash      text NOT NULL DEFAULT '',
    views_count        integer NOT NULL DEFAULT 0,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    revoked_at         timestamptz
);

CREATE INDEX export_shares_token_idx ON export_shares (share_token) WHERE revoked_at IS NULL;
CREATE INDEX export_shares_job_idx ON export_shares (job_id);

ALTER TABLE export_shares ENABLE ROW LEVEL SECURITY;
ALTER TABLE export_shares FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON export_shares USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS export_shares;
