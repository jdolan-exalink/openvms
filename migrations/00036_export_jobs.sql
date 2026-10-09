-- +goose Up
CREATE TABLE export_jobs (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (id),
    site_id            uuid REFERENCES sites (id),
    requested_by       uuid NOT NULL REFERENCES users (id),
    name               text NOT NULL,
    start_time         timestamptz NOT NULL,
    end_time           timestamptz NOT NULL,
    status             text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'preparing', 'transferring', 'processing', 'ready', 'failed', 'cancelled', 'expired')),
    progress           real NOT NULL DEFAULT 0,
    error              text NOT NULL DEFAULT '',
    total_bytes        bigint NOT NULL DEFAULT 0,
    transferred_bytes  bigint NOT NULL DEFAULT 0,
    speed_bps          real NOT NULL DEFAULT 0,
    eta_seconds        integer NOT NULL DEFAULT 0,
    local_path         text NOT NULL DEFAULT '',
    manifest           jsonb NOT NULL DEFAULT '{}'::jsonb,
    protected          boolean NOT NULL DEFAULT false,
    expires_at         timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    deleted_at         timestamptz
);

CREATE INDEX export_jobs_tenant_time_idx ON export_jobs (tenant_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX export_jobs_active_idx ON export_jobs (status) WHERE status IN ('queued', 'preparing', 'transferring', 'processing') AND deleted_at IS NULL;

ALTER TABLE export_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE export_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON export_jobs USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

CREATE TABLE export_job_items (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id             uuid NOT NULL REFERENCES export_jobs (id) ON DELETE CASCADE,
    tenant_id          uuid NOT NULL REFERENCES tenants (id),
    camera_id          uuid NOT NULL REFERENCES cameras (id),
    server_id          uuid NOT NULL REFERENCES frigate_servers (id),
    remote_export_id   text NOT NULL DEFAULT '',
    status             text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'preparing', 'transferring', 'processing', 'ready', 'failed', 'cancelled')),
    progress           real NOT NULL DEFAULT 0,
    error              text NOT NULL DEFAULT '',
    total_bytes        bigint NOT NULL DEFAULT 0,
    transferred_bytes  bigint NOT NULL DEFAULT 0,
    remote_path        text NOT NULL DEFAULT '',
    local_path         text NOT NULL DEFAULT '',
    sha256_hash        text NOT NULL DEFAULT '',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX export_job_items_job_idx ON export_job_items (job_id);

ALTER TABLE export_job_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE export_job_items FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON export_job_items USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS export_job_items;
DROP TABLE IF EXISTS export_jobs;
