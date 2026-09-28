-- +goose Up
-- PDW-4: asynchronous clip watermark jobs, processed by the worker (ffmpeg drawtext +
-- overlay). watermark_text and logo_key are frozen at request time (from the tenant's
-- branding then) rather than re-read when the worker later processes the job, so the
-- burned-in watermark always matches what the requester saw when they clicked download —
-- the same reasoning the photo download's synchronous burn-in already follows.
CREATE TABLE clip_watermark_jobs (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants (id),
    site_id          uuid NOT NULL REFERENCES sites (id),
    server_id        uuid NOT NULL REFERENCES frigate_servers (id),
    camera_id        uuid NOT NULL REFERENCES cameras (id),
    lpr_read_id      uuid NOT NULL REFERENCES lpr_reads (id),
    remote_event_id  text NOT NULL,
    requested_by     uuid NOT NULL REFERENCES users (id),
    watermark_text   text NOT NULL,
    logo_key         text NOT NULL DEFAULT '',
    status           text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed')),
    error            text NOT NULL DEFAULT '',
    -- Object store key of the finished, watermarked clip once status = 'done'.
    output_key       text NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX clip_watermark_jobs_tenant_idx ON clip_watermark_jobs (tenant_id, created_at DESC);
-- Backs the worker's claim query (status IN ('queued') ORDER BY created_at ... FOR UPDATE
-- SKIP LOCKED); running rows are included too so a crashed/stuck job is still visible for
-- operational queries without a second index.
CREATE INDEX clip_watermark_jobs_active_idx ON clip_watermark_jobs (created_at) WHERE status IN ('queued', 'running');

ALTER TABLE clip_watermark_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE clip_watermark_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON clip_watermark_jobs USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS clip_watermark_jobs;
