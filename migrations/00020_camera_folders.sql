-- +goose Up
-- LV-13: shared camera folders for the Live explorer. One tree per tenant: folders belong to a
-- Frigate server (one level, no nesting) and cameras can sit in a folder of their own server or
-- at the server root (folder_id NULL). Deleting a folder returns its cameras to the root; hard
-- deleting a server removes its folders through the cascade.
CREATE TABLE camera_folders (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    server_id  uuid NOT NULL REFERENCES frigate_servers (id) ON DELETE CASCADE,
    name       text NOT NULL,
    sort_order integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX camera_folders_server_name_key ON camera_folders (server_id, lower(name));
CREATE INDEX camera_folders_tenant_idx ON camera_folders (tenant_id);

ALTER TABLE camera_folders ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_folders FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON camera_folders USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE cameras
    ADD COLUMN folder_id uuid REFERENCES camera_folders (id) ON DELETE SET NULL,
    ADD COLUMN sort_order integer NOT NULL DEFAULT 0;
CREATE INDEX cameras_folder_idx ON cameras (folder_id) WHERE folder_id IS NOT NULL;

-- +goose Down
ALTER TABLE cameras DROP COLUMN folder_id, DROP COLUMN sort_order;
DROP TABLE camera_folders;
