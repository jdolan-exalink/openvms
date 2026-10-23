-- FACE-07.1: durable, tenant-scoped observations from Frigate's unclassified face attempts.
-- Crop bytes remain in the object store. Source filenames are private idempotency keys, while
-- the UUID is the stable operator-facing identifier.
-- +goose Up
ALTER TABLE frigate_servers ADD CONSTRAINT frigate_servers_id_tenant_key UNIQUE (id, tenant_id);

CREATE TABLE face_sightings (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants (id),
    server_id           uuid NOT NULL,
    source_file         text NOT NULL CHECK (length(source_file) BETWEEN 1 AND 255),
    source_seen_at      timestamptz,
    source_label        text NOT NULL DEFAULT '',
    source_score        double precision,
    first_seen_at       timestamptz NOT NULL DEFAULT now(),
    crop_key            text,
    crop_content_type   text CHECK (crop_content_type IS NULL OR crop_content_type IN ('image/jpeg', 'image/png', 'image/webp')),
    crop_size_bytes     integer CHECK (crop_size_bytes IS NULL OR crop_size_bytes > 0),
    crop_sha256         text CHECK (crop_sha256 IS NULL OR crop_sha256 ~ '^[0-9a-f]{64}$'),
    ingest_error        text NOT NULL DEFAULT '',
    review_status       text NOT NULL DEFAULT 'pending' CHECK (review_status IN ('pending', 'assigned', 'rejected')),
    person_id           uuid,
    reviewed_at         timestamptz,
    CONSTRAINT face_sightings_server_fk FOREIGN KEY (server_id, tenant_id)
        REFERENCES frigate_servers (id, tenant_id),
    CONSTRAINT face_sightings_person_fk FOREIGN KEY (person_id, tenant_id)
        REFERENCES face_persons (id, tenant_id),
    CONSTRAINT face_sightings_id_tenant_key UNIQUE (id, tenant_id),
    CONSTRAINT face_sightings_source_key UNIQUE (tenant_id, server_id, source_file),
    CONSTRAINT face_sightings_review_state CHECK (
        (review_status = 'assigned' AND person_id IS NOT NULL AND reviewed_at IS NOT NULL)
        OR (review_status <> 'assigned' AND person_id IS NULL)
    )
);
CREATE INDEX face_sightings_pending_idx ON face_sightings (tenant_id, first_seen_at DESC, id)
    WHERE review_status = 'pending';
CREATE INDEX face_sightings_server_idx ON face_sightings (tenant_id, server_id, first_seen_at DESC);

ALTER TABLE face_sightings ENABLE ROW LEVEL SECURITY;
ALTER TABLE face_sightings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON face_sightings
    USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS face_sightings;
ALTER TABLE frigate_servers DROP CONSTRAINT IF EXISTS frigate_servers_id_tenant_key;
