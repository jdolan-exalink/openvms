-- +goose Up
-- FC-1: OpenVMS-side history of Frigate configuration changes. Frigate keeps no backup of
-- config.yml, so every write made through the VMS stores the full file before and after,
-- which is what rollback re-applies. The YAML holds credentials (stream URLs, ONVIF
-- passwords): only holders of servers.config.secrets may read before_yaml / after_yaml; the
-- API redacts them for everyone else. patch is the redacted request (camera_patch only).
CREATE TABLE frigate_config_revisions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    server_id     uuid NOT NULL REFERENCES frigate_servers (id) ON DELETE CASCADE,
    camera_id     uuid REFERENCES cameras (id) ON DELETE SET NULL,
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    actor_name    text NOT NULL DEFAULT '',
    kind          text NOT NULL CHECK (kind IN ('camera_patch', 'raw_save', 'rollback')),
    sections      text[] NOT NULL DEFAULT '{}',
    before_yaml   text NOT NULL,
    after_yaml    text NOT NULL,
    patch         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX frigate_config_revisions_server_idx ON frigate_config_revisions (server_id, created_at DESC);
CREATE INDEX frigate_config_revisions_camera_idx ON frigate_config_revisions (camera_id, created_at DESC) WHERE camera_id IS NOT NULL;

ALTER TABLE frigate_config_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE frigate_config_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON frigate_config_revisions USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS frigate_config_revisions;
