-- FACE-01: central per-tenant face registry. Persons and their enrollment images live here;
-- the image bytes live in the object store (face_person_images.storage_key), never in Postgres.
-- frigate_name is the identifier pushed to every Frigate face library: normalized, unique per
-- tenant and stable across display_name renames. Deleted persons stay as tombstones
-- (deleted_at) so the registry sync can propagate the deletion to Frigate; their images are
-- removed at delete time.
-- +goose Up
CREATE TABLE face_persons (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants (id),
    display_name  text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 120),
    frigate_name  text NOT NULL CHECK (frigate_name ~ '^[a-z0-9_]{1,50}$'),
    notes         text NOT NULL DEFAULT '' CHECK (length(notes) <= 2000),
    created_by    uuid REFERENCES users (id),
    updated_by    uuid REFERENCES users (id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz,
    CONSTRAINT face_persons_id_tenant_key UNIQUE (id, tenant_id)
);
-- frigate_name is unique across tombstones too: a deleted name must not be reused before the
-- deletion has been propagated to every Frigate.
CREATE UNIQUE INDEX face_persons_frigate_name_key ON face_persons (tenant_id, frigate_name);
CREATE UNIQUE INDEX face_persons_display_name_key ON face_persons (tenant_id, lower(display_name)) WHERE deleted_at IS NULL;
CREATE INDEX face_persons_tenant_idx ON face_persons (tenant_id, lower(display_name)) WHERE deleted_at IS NULL;

CREATE TABLE face_person_images (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL,
    person_id     uuid NOT NULL,
    -- Object store key: tenant/{tenant_id}/faces/{person_id}/{id}.
    storage_key   text NOT NULL,
    content_type  text NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png', 'image/webp')),
    size_bytes    integer NOT NULL CHECK (size_bytes > 0),
    sha256        text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    width         integer CHECK (width IS NULL OR width > 0),
    height        integer CHECK (height IS NULL OR height > 0),
    created_by    uuid REFERENCES users (id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT face_person_images_person_fk FOREIGN KEY (person_id, tenant_id)
        REFERENCES face_persons (id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT face_person_images_sha_unique UNIQUE (person_id, sha256)
);
CREATE INDEX face_person_images_person_idx ON face_person_images (tenant_id, person_id, created_at, id);

ALTER TABLE face_persons ENABLE ROW LEVEL SECURITY;
ALTER TABLE face_persons FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON face_persons USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));
ALTER TABLE face_person_images ENABLE ROW LEVEL SECURITY;
ALTER TABLE face_person_images FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON face_person_images USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- Seed grants: faces.manage mirrors tenant.manage (allow and deny), faces.view mirrors lpr.view
-- (the closest privacy-sensitive recognition permission) and is also implied by a faces.manage
-- allow. Only platform/tenant-scoped grants carry over: faces.* are tenant-scoped, so camera,
-- site or server scoped LPR grants have no equivalent. Idempotent (ON CONFLICT DO NOTHING).
INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id, created_by)
SELECT g.tenant_id, g.subject_type, g.subject_id, 'faces.manage', g.effect, g.scope_type, g.scope_id, g.created_by
FROM permission_grants g
WHERE g.scope_type IN ('platform', 'tenant') AND g.permission = 'tenant.manage'
ON CONFLICT DO NOTHING;

INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id, created_by)
SELECT g.tenant_id, g.subject_type, g.subject_id, 'faces.view', g.effect, g.scope_type, g.scope_id, g.created_by
FROM permission_grants g
WHERE g.scope_type IN ('platform', 'tenant')
  AND (g.permission = 'lpr.view' OR (g.permission = 'tenant.manage' AND g.effect = 'allow'))
ON CONFLICT DO NOTHING;

-- +goose Down
-- Forward-only in practice: dropping removes the registry and every person/image row (object
-- store blobs under tenant/*/faces/ are NOT deleted) and the faces.* grants, including any an
-- operator created by hand.
DELETE FROM permission_grants WHERE permission IN ('faces.view', 'faces.manage');
DROP TABLE IF EXISTS face_person_images;
DROP TABLE IF EXISTS face_persons;
