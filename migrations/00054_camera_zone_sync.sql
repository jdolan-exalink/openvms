-- P5-02-02: durable, versioned desired-state queue for camera-zone synchronization.
-- +goose Up
ALTER TABLE camera_zones
    ADD COLUMN sync_status text NOT NULL DEFAULT 'pending',
    ADD COLUMN sync_revision bigint NOT NULL DEFAULT 1,
    ADD COLUMN last_applied jsonb,
    ADD COLUMN sync_error text,
    ADD COLUMN deleted_at timestamptz;
ALTER TABLE camera_zones DROP CONSTRAINT camera_zones_camera_name_unique;
CREATE UNIQUE INDEX camera_zones_camera_name_active_unique
    ON camera_zones (camera_id, name) WHERE deleted_at IS NULL;
ALTER TABLE camera_zones ADD CONSTRAINT camera_zones_sync_status_check
    CHECK (sync_status IN ('pending', 'synced', 'failed', 'conflict'));

CREATE TABLE camera_zone_sync_jobs (
    zone_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    camera_id uuid NOT NULL,
    actor_user_id uuid,
    operation text NOT NULL CHECK (operation IN ('upsert', 'delete')),
    desired_revision bigint NOT NULL,
    desired_config jsonb,
    last_applied jsonb,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'synced', 'failed', 'conflict')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX camera_zone_sync_jobs_due_idx
    ON camera_zone_sync_jobs (next_attempt_at, created_at)
    WHERE status IN ('pending', 'processing');

-- Zones created before this migration were never synced and have no initiating actor to
-- authorize a remote write. Backfill them as failed with a retryable job so an operator with
-- cameras.manage can trigger the first sync explicitly; they must not stay pending forever.
-- Row-level security is bypassed for this migration-owned backfill only.
SELECT set_config('app.all_tenants', 'on', true);
UPDATE camera_zones
SET sync_status = 'failed',
    sync_error = 'initial sync requires a manual retry'
WHERE deleted_at IS NULL;
INSERT INTO camera_zone_sync_jobs
    (zone_id, tenant_id, camera_id, operation, desired_revision, desired_config, status, last_error)
SELECT id, tenant_id, camera_id, 'upsert', sync_revision,
       settings || jsonb_build_object('_ovms_points', points, '_ovms_name', name),
       'failed', 'initial sync requires a manual retry'
FROM camera_zones
WHERE deleted_at IS NULL;
SELECT set_config('app.all_tenants', '', true);

ALTER TABLE camera_zone_sync_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_zone_sync_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON camera_zone_sync_jobs
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM camera_zones GROUP BY camera_id, name HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot roll back camera-zone sync while deleted names collide with active zones';
    END IF;
END $$;
DROP TABLE IF EXISTS camera_zone_sync_jobs;
DROP INDEX IF EXISTS camera_zones_camera_name_active_unique;
ALTER TABLE camera_zones DROP CONSTRAINT IF EXISTS camera_zones_sync_status_check;
ALTER TABLE camera_zones DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE camera_zones DROP COLUMN IF EXISTS sync_error;
ALTER TABLE camera_zones DROP COLUMN IF EXISTS last_applied;
ALTER TABLE camera_zones DROP COLUMN IF EXISTS sync_revision;
ALTER TABLE camera_zones DROP COLUMN IF EXISTS sync_status;
ALTER TABLE camera_zones ADD CONSTRAINT camera_zones_camera_name_unique UNIQUE (camera_id, name);
