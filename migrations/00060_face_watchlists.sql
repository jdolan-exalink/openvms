-- FACE-05: watchlists of known persons. A watchlist is a named group of registered persons a
-- tenant uses in alarm rules ("anyone in list X was seen"). Members reference live persons only;
-- the faces service removes a person from every list when it is deleted, so a tombstone is never
-- a member. Rules keep watchlist ids in their JSON conditions (resolved at evaluation).
-- +goose Up
CREATE TABLE face_watchlists (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants (id),
    name         text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    description  text NOT NULL DEFAULT '' CHECK (length(description) <= 500),
    created_by   uuid REFERENCES users (id),
    updated_by   uuid REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT face_watchlists_id_tenant_key UNIQUE (id, tenant_id)
);
CREATE UNIQUE INDEX face_watchlists_name_key ON face_watchlists (tenant_id, lower(name));

CREATE TABLE face_watchlist_members (
    tenant_id     uuid NOT NULL,
    watchlist_id  uuid NOT NULL,
    person_id     uuid NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (watchlist_id, person_id),
    CONSTRAINT face_watchlist_members_list_fk FOREIGN KEY (watchlist_id, tenant_id)
        REFERENCES face_watchlists (id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT face_watchlist_members_person_fk FOREIGN KEY (person_id, tenant_id)
        REFERENCES face_persons (id, tenant_id) ON DELETE CASCADE
);
CREATE INDEX face_watchlist_members_person_idx ON face_watchlist_members (tenant_id, person_id);

ALTER TABLE face_watchlists ENABLE ROW LEVEL SECURITY;
ALTER TABLE face_watchlists FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON face_watchlists USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));
ALTER TABLE face_watchlist_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE face_watchlist_members FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON face_watchlist_members USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS face_watchlist_members;
DROP TABLE IF EXISTS face_watchlists;
