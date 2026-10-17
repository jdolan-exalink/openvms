-- FACE-02: durable sync of the central face registry to every face-enabled Frigate server.
-- face_persons.sync_revision is the versioned desired state of a person (bumped by every image
-- add/remove and by deletion; display-name edits do not touch Frigate and do not bump it).
-- face_server_sync holds one job per (server, person) and doubles as the per-server status the
-- UI shows. face_server_images maps a central image to the id Frigate assigned to it, so the
-- sync can delete exactly what it registered and recognise anything else as drift.
--
-- Ownership: a Frigate face is "owned" by OpenVMS only when the sync created it (or adopted an
-- empty folder with the same name). Faces that already exist with images and are not owned are
-- never modified or deleted; the job ends in status 'conflict' instead.
-- +goose Up
ALTER TABLE face_persons ADD COLUMN sync_revision bigint NOT NULL DEFAULT 1;

CREATE TABLE face_server_sync (
    server_id        uuid NOT NULL REFERENCES frigate_servers (id) ON DELETE CASCADE,
    person_id        uuid NOT NULL,
    tenant_id        uuid NOT NULL,
    desired_revision bigint NOT NULL,
    synced_revision  bigint NOT NULL DEFAULT 0,
    status           text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'synced', 'failed', 'conflict', 'disabled')),
    owned            boolean NOT NULL DEFAULT false,
    attempts         integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    hard_failures    integer NOT NULL DEFAULT 0 CHECK (hard_failures >= 0),
    next_attempt_at  timestamptz NOT NULL DEFAULT now(),
    lease_until      timestamptz,
    last_error       text,
    last_synced_at   timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, person_id),
    CONSTRAINT face_server_sync_person_fk FOREIGN KEY (person_id, tenant_id)
        REFERENCES face_persons (id, tenant_id) ON DELETE CASCADE
);
CREATE INDEX face_server_sync_due_idx ON face_server_sync (next_attempt_at, created_at)
    WHERE status IN ('pending', 'processing', 'disabled');
CREATE INDEX face_server_sync_person_idx ON face_server_sync (person_id);

CREATE TABLE face_server_images (
    server_id        uuid NOT NULL REFERENCES frigate_servers (id) ON DELETE CASCADE,
    image_id         uuid NOT NULL REFERENCES face_person_images (id) ON DELETE CASCADE,
    person_id        uuid NOT NULL,
    tenant_id        uuid NOT NULL,
    frigate_image_id text NOT NULL CHECK (length(frigate_image_id) BETWEEN 1 AND 255),
    created_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, image_id),
    CONSTRAINT face_server_images_frigate_id_key UNIQUE (server_id, person_id, frigate_image_id)
);

-- Remote faces an owned job may still hold on a server that was face-disabled when the person
-- was purged; deleted once the server is face-capable again. No FK to the (purged) person.
CREATE TABLE face_remote_cleanup (
    tenant_id    uuid NOT NULL,
    server_id    uuid NOT NULL REFERENCES frigate_servers (id) ON DELETE CASCADE,
    frigate_name text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, frigate_name)
);
ALTER TABLE face_remote_cleanup ENABLE ROW LEVEL SECURITY;
ALTER TABLE face_remote_cleanup FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON face_remote_cleanup USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));
ALTER TABLE face_server_sync ENABLE ROW LEVEL SECURITY;
ALTER TABLE face_server_sync FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON face_server_sync USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));
ALTER TABLE face_server_images ENABLE ROW LEVEL SECURITY;
ALTER TABLE face_server_images FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON face_server_images USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS face_remote_cleanup;
DROP TABLE IF EXISTS face_server_images;
DROP TABLE IF EXISTS face_server_sync;
ALTER TABLE face_persons DROP COLUMN IF EXISTS sync_revision;
