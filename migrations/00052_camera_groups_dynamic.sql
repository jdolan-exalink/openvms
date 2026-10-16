-- +goose Up
-- Alarms phase 2 (P2-04): dynamic camera groups. A NULL filter is a manual group (membership edited by
-- hand); a non-NULL filter is evaluated by the application and materialized into camera_group_members.
-- RLS is unchanged: both columns live on camera_groups, which already has the tenant_isolation policy.
ALTER TABLE camera_groups
    ADD COLUMN filter  jsonb NULL CHECK (filter IS NULL OR jsonb_typeof(filter) = 'object'),
    ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK (version >= 1);

-- +goose Down
ALTER TABLE camera_groups DROP COLUMN IF EXISTS version, DROP COLUMN IF EXISTS filter;
