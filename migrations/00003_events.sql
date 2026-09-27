-- +goose Up
-- M2/M3: federated event index (PRD §34-40) and global LPR (PRD §41-45).
-- Frigate keeps the recordings; the VMS keeps metadata and a thumbnail per event so
-- search keeps working while a Frigate is offline.

-- Frigate servers can be registered without credentials (port 5000 or 8971 with auth
-- disabled). auth_mode says which; password_sealed is empty for 'none'.
ALTER TABLE frigate_servers
    ADD COLUMN auth_mode text NOT NULL DEFAULT 'credentials' CHECK (auth_mode IN ('credentials', 'none')),
    ALTER COLUMN username SET DEFAULT '',
    ALTER COLUMN password_sealed SET DEFAULT ''::bytea;

-- One row per Frigate review item. Review items group detections of a camera over time
-- and are what Frigate's own UI lists as alerts and detections.
CREATE TABLE events (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (id),
    site_id            uuid NOT NULL REFERENCES sites (id),
    server_id          uuid NOT NULL REFERENCES frigate_servers (id),
    camera_id          uuid NOT NULL REFERENCES cameras (id),
    -- Review id inside its Frigate. Unique per server only.
    remote_id          text NOT NULL,
    severity           text NOT NULL CHECK (severity IN ('alert', 'detection')),
    labels             text[] NOT NULL DEFAULT '{}',
    sub_labels         text[] NOT NULL DEFAULT '{}',
    zones              text[] NOT NULL DEFAULT '{}',
    audio              text[] NOT NULL DEFAULT '{}',
    -- Normalized plates read during this event (see lpr_reads for scores).
    plates             text[] NOT NULL DEFAULT '{}',
    detection_ids      text[] NOT NULL DEFAULT '{}',
    start_time         timestamptz NOT NULL,
    end_time           timestamptz,
    reviewed           boolean NOT NULL DEFAULT false,
    -- Object-storage key of the thumbnail copied from Frigate; empty until copied.
    thumb_key          text NOT NULL DEFAULT '',
    thumb_attempts     int NOT NULL DEFAULT 0,
    first_seen_at      timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX events_server_remote_key ON events (server_id, remote_id);
CREATE INDEX events_tenant_time_idx ON events (tenant_id, start_time DESC, id DESC);
CREATE INDEX events_camera_time_idx ON events (camera_id, start_time DESC);
CREATE INDEX events_labels_idx ON events USING gin (labels);
CREATE INDEX events_plates_idx ON events USING gin (plates);
CREATE INDEX events_thumb_pending_idx ON events (server_id) WHERE thumb_key = '' AND end_time IS NOT NULL;

-- One row per recognized plate (a Frigate tracked object with recognized_license_plate).
CREATE TABLE lpr_reads (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (id),
    site_id            uuid NOT NULL REFERENCES sites (id),
    server_id          uuid NOT NULL REFERENCES frigate_servers (id),
    camera_id          uuid NOT NULL REFERENCES cameras (id),
    -- Tracked-object id inside Frigate; links to events.detection_ids.
    remote_event_id    text NOT NULL,
    plate              text NOT NULL,
    -- Upper case, letters and digits only: "ab 123 cd" and "AB123CD" match.
    plate_normalized   text NOT NULL,
    score              real,
    label              text NOT NULL DEFAULT '',
    sub_label          text NOT NULL DEFAULT '',
    zones              text[] NOT NULL DEFAULT '{}',
    seen_at            timestamptz NOT NULL,
    first_seen_at      timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX lpr_reads_server_event_key ON lpr_reads (server_id, remote_event_id);
CREATE INDEX lpr_reads_tenant_time_idx ON lpr_reads (tenant_id, seen_at DESC, id DESC);
CREATE INDEX lpr_reads_plate_trgm_idx ON lpr_reads USING gin (plate_normalized gin_trgm_ops);
CREATE INDEX lpr_reads_camera_time_idx ON lpr_reads (camera_id, seen_at DESC);

-- Per-server ingestion cursors. Survive restarts so an outage is backfilled from where
-- the VMS stopped, not from "now".
CREATE TABLE event_sync_state (
    server_id          uuid PRIMARY KEY REFERENCES frigate_servers (id),
    tenant_id          uuid NOT NULL REFERENCES tenants (id),
    review_cursor      timestamptz,
    object_cursor      timestamptz,
    last_success_at    timestamptz,
    last_error         text NOT NULL DEFAULT '',
    backfilled_until   timestamptz,
    updated_at         timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE events ENABLE ROW LEVEL SECURITY;
ALTER TABLE events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON events USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE lpr_reads ENABLE ROW LEVEL SECURITY;
ALTER TABLE lpr_reads FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lpr_reads USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE event_sync_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE event_sync_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON event_sync_state USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS event_sync_state;
DROP TABLE IF EXISTS lpr_reads;
DROP TABLE IF EXISTS events;
ALTER TABLE frigate_servers DROP COLUMN IF EXISTS auth_mode;
