-- Universal camera configuration, level 2: OpenVMS profiles. A profile is a named, tenant-scoped
-- sparse normalized patch (the same document the per-camera overrides use) with a version that
-- grows on every edit. A camera links to at most one profile through camera_config_state;
-- the profile never rewrites the camera's own overrides (camera_config_state.desired).
-- Deleting a profile that cameras still use is refused by the foreign key (ON DELETE RESTRICT).
-- Tenant isolation is the same RLS policy as the other camera-owned tables.
-- +goose Up
CREATE TABLE camera_config_profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    name text NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 120),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    patch jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(patch) = 'object'),
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT camera_config_profiles_id_tenant_key UNIQUE (id, tenant_id)
);

CREATE UNIQUE INDEX camera_config_profiles_tenant_name_key
    ON camera_config_profiles (tenant_id, lower(name));

ALTER TABLE camera_config_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_config_profiles FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON camera_config_profiles
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- profile_version is the version of the profile the camera last received; a smaller number than
-- the profile's own tells that the profile changed since (re-apply from the bulk review).
ALTER TABLE camera_config_state
    ADD COLUMN profile_id uuid,
    ADD COLUMN profile_version bigint CHECK (profile_version IS NULL OR profile_version >= 1),
    ADD CONSTRAINT camera_config_state_profile_fk FOREIGN KEY (profile_id, tenant_id)
        REFERENCES camera_config_profiles (id, tenant_id) ON DELETE RESTRICT,
    ADD CONSTRAINT camera_config_state_profile_version_chk
        CHECK ((profile_id IS NULL) = (profile_version IS NULL));

CREATE INDEX camera_config_state_profile_idx ON camera_config_state (profile_id)
    WHERE profile_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS camera_config_state_profile_idx;
ALTER TABLE camera_config_state
    DROP CONSTRAINT IF EXISTS camera_config_state_profile_version_chk,
    DROP CONSTRAINT IF EXISTS camera_config_state_profile_fk,
    DROP COLUMN IF EXISTS profile_version,
    DROP COLUMN IF EXISTS profile_id;
DROP TABLE IF EXISTS camera_config_profiles;
