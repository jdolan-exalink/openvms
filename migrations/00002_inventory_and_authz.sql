-- +goose Up
-- Inventory (PRD §5-6, §33) and authorization model (PRD §24-30).
-- Every row gets a UUID; Frigate names, hostnames and IPs are never keys (PRD §6).

CREATE TABLE tenants (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        text NOT NULL,
    name        text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    deleted_by  uuid
);
CREATE UNIQUE INDEX tenants_slug_key ON tenants (slug) WHERE deleted_at IS NULL;

-- Users are the subjects of authorization. Credentials, MFA and sessions arrive in M4.
-- tenant_id NULL marks a platform-level user (e.g. Platform Super Admin).
CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid REFERENCES tenants (id),
    username      text NOT NULL,
    email         text,
    display_name  text NOT NULL,
    status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'locked', 'pending')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz,
    deleted_by    uuid
);
CREATE UNIQUE INDEX users_username_key ON users (lower(username)) WHERE deleted_at IS NULL;

-- Machine and bootstrap credentials. Only the SHA-256 of the token is stored.
CREATE TABLE api_tokens (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id),
    name        text NOT NULL,
    token_hash  bytea NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz,
    revoked_at  timestamptz,
    last_used_at timestamptz
);

CREATE TABLE user_groups (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid REFERENCES tenants (id),
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz,
    deleted_by   uuid
);

CREATE TABLE user_group_members (
    group_id  uuid NOT NULL REFERENCES user_groups (id) ON DELETE CASCADE,
    user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX user_group_members_user_idx ON user_group_members (user_id);

CREATE TABLE sites (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants (id),
    name        text NOT NULL,
    timezone    text NOT NULL DEFAULT 'America/Argentina/Cordoba',
    address     text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    deleted_by  uuid
);
CREATE UNIQUE INDEX sites_tenant_name_key ON sites (tenant_id, lower(name)) WHERE deleted_at IS NULL;

CREATE TABLE frigate_servers (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL REFERENCES tenants (id),
    site_id           uuid NOT NULL REFERENCES sites (id),
    name              text NOT NULL,
    base_url          text NOT NULL,
    username          text NOT NULL,
    -- AES-256-GCM sealed password, bound to this row's id (see internal/secrets).
    password_sealed   bytea NOT NULL,
    tls_skip_verify   boolean NOT NULL DEFAULT false,
    frigate_version   text NOT NULL DEFAULT '',
    capabilities      jsonb NOT NULL DEFAULT '{}'::jsonb,
    status            text NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown', 'online', 'degraded', 'offline')),
    last_seen_at      timestamptz,
    last_checked_at   timestamptz,
    last_error        text NOT NULL DEFAULT '',
    stats             jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz,
    deleted_by        uuid
);
CREATE UNIQUE INDEX frigate_servers_tenant_name_key ON frigate_servers (tenant_id, lower(name)) WHERE deleted_at IS NULL;
CREATE INDEX frigate_servers_site_idx ON frigate_servers (site_id);

CREATE TABLE cameras (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants (id),
    site_id        uuid NOT NULL REFERENCES sites (id),
    server_id      uuid NOT NULL REFERENCES frigate_servers (id),
    -- Camera name inside its Frigate. Unique per server only (PRD §6).
    remote_name    text NOT NULL,
    display_name   text NOT NULL,
    enabled        boolean NOT NULL DEFAULT true,
    zones          text[] NOT NULL DEFAULT '{}',
    lpr            boolean NOT NULL DEFAULT false,
    live_stream    text NOT NULL DEFAULT '',
    hq_stream      text NOT NULL DEFAULT '',
    status         text NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown', 'online', 'degraded', 'offline')),
    fps            real,
    missing_since  timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    deleted_at     timestamptz,
    deleted_by     uuid
);
CREATE UNIQUE INDEX cameras_server_remote_key ON cameras (server_id, remote_name) WHERE deleted_at IS NULL;
CREATE INDEX cameras_site_idx ON cameras (site_id);
CREATE INDEX cameras_tenant_idx ON cameras (tenant_id);

CREATE TABLE camera_groups (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants (id),
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz,
    deleted_by   uuid
);
CREATE UNIQUE INDEX camera_groups_tenant_name_key ON camera_groups (tenant_id, lower(name)) WHERE deleted_at IS NULL;

CREATE TABLE camera_group_members (
    group_id   uuid NOT NULL REFERENCES camera_groups (id) ON DELETE CASCADE,
    camera_id  uuid NOT NULL REFERENCES cameras (id) ON DELETE CASCADE,
    tenant_id  uuid NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (group_id, camera_id)
);
CREATE INDEX camera_group_members_camera_idx ON camera_group_members (camera_id);

-- A grant is permission + scope + effect for a user or a user group (PRD §27-30).
-- scope_id is NULL only for PLATFORM scope. tenant_id is NULL only for platform grants.
CREATE TABLE permission_grants (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid REFERENCES tenants (id),
    subject_type  text NOT NULL CHECK (subject_type IN ('user', 'group')),
    subject_id    uuid NOT NULL,
    permission    text NOT NULL,
    effect        text NOT NULL CHECK (effect IN ('allow', 'deny')),
    scope_type    text NOT NULL CHECK (scope_type IN ('platform', 'tenant', 'site', 'server', 'camera_group', 'camera')),
    scope_id      uuid,
    created_by    uuid,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CHECK ((scope_type = 'platform') = (scope_id IS NULL)),
    CHECK (scope_type = 'platform' OR tenant_id IS NOT NULL)
);
CREATE UNIQUE INDEX permission_grants_unique ON permission_grants
    (subject_type, subject_id, permission, effect, scope_type, coalesce(scope_id, '00000000-0000-0000-0000-000000000000'::uuid));
CREATE INDEX permission_grants_subject_idx ON permission_grants (subject_type, subject_id, permission);

-- Append-only audit trail (PRD §66). The application role never updates or deletes rows.
CREATE TABLE audit_log (
    id           bigserial PRIMARY KEY,
    occurred_at  timestamptz NOT NULL DEFAULT now(),
    tenant_id    uuid,
    actor_id     uuid,
    actor_name   text NOT NULL DEFAULT '',
    action       text NOT NULL,
    target_type  text NOT NULL DEFAULT '',
    target_id    uuid,
    request_id   text NOT NULL DEFAULT '',
    ip           inet,
    details      jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX audit_log_tenant_time_idx ON audit_log (tenant_id, occurred_at DESC);

-- +goose StatementBegin
CREATE FUNCTION audit_log_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_log_no_update BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_immutable();

-- Tenant isolation as a second barrier behind the repository filters (PRD §69).
-- The API sets app.tenant_id for tenant users or app.all_tenants for platform users
-- inside each transaction; with neither set, these tables return no rows.
-- +goose StatementBegin
CREATE FUNCTION app_tenant_visible(row_tenant uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT coalesce(current_setting('app.all_tenants', true), '') = 'on'
        OR (row_tenant IS NOT NULL AND row_tenant::text = current_setting('app.tenant_id', true));
$$;
-- +goose StatementEnd

ALTER TABLE sites ENABLE ROW LEVEL SECURITY;
ALTER TABLE sites FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sites USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE frigate_servers ENABLE ROW LEVEL SECURITY;
ALTER TABLE frigate_servers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON frigate_servers USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE cameras ENABLE ROW LEVEL SECURITY;
ALTER TABLE cameras FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cameras USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE camera_groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_groups FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON camera_groups USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE camera_group_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_group_members FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON camera_group_members USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- The API and worker run every query as openvms_app (SET ROLE after connecting), a role
-- without BYPASSRLS, so the policies above apply even when the login role is privileged.
-- audit_log gets no UPDATE/DELETE privilege at all.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'openvms_app') THEN
        CREATE ROLE openvms_app NOLOGIN NOBYPASSRLS;
    END IF;
    EXECUTE format('GRANT openvms_app TO %I', current_user);
END;
$$;
-- +goose StatementEnd
GRANT USAGE ON SCHEMA public TO openvms_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO openvms_app;
REVOKE UPDATE, DELETE, TRUNCATE ON audit_log FROM openvms_app;
REVOKE ALL ON goose_db_version FROM openvms_app;
GRANT SELECT ON goose_db_version TO openvms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO openvms_app;
-- New tables in later migrations get the same privileges; restrict append-only ones there.
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO openvms_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO openvms_app;

-- +goose Down
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM openvms_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM openvms_app;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM openvms_app;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM openvms_app;
REVOKE USAGE ON SCHEMA public FROM openvms_app;
DROP TABLE IF EXISTS audit_log;
DROP FUNCTION IF EXISTS audit_log_immutable();
DROP TABLE IF EXISTS permission_grants;
DROP TABLE IF EXISTS camera_group_members;
DROP TABLE IF EXISTS camera_groups;
DROP TABLE IF EXISTS cameras;
DROP TABLE IF EXISTS frigate_servers;
DROP TABLE IF EXISTS sites;
DROP TABLE IF EXISTS user_group_members;
DROP TABLE IF EXISTS user_groups;
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;
DROP FUNCTION IF EXISTS app_tenant_visible(uuid);
