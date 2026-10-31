-- LPR-01: watchlists of known plates (e.g. stolen, unauthorized, VIPs).
-- +goose Up
CREATE TABLE lpr_watchlists (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    name         text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    description  text NOT NULL DEFAULT '' CHECK (length(description) <= 500),
    color        text NOT NULL DEFAULT '#ef4444' CHECK (length(color) <= 32),
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT lpr_watchlists_id_tenant_key UNIQUE (id, tenant_id)
);
CREATE UNIQUE INDEX lpr_watchlists_name_key ON lpr_watchlists (tenant_id, lower(name));

CREATE TABLE lpr_watchlist_items (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    watchlist_id  uuid NOT NULL,
    plate         text NOT NULL CHECK (length(btrim(plate)) >= 2),
    notes         text NOT NULL DEFAULT '' CHECK (length(notes) <= 500),
    vehicle_brand text NOT NULL DEFAULT '' CHECK (length(vehicle_brand) <= 100),
    vehicle_model text NOT NULL DEFAULT '' CHECK (length(vehicle_model) <= 100),
    vehicle_color text NOT NULL DEFAULT '' CHECK (length(vehicle_color) <= 50),
    owner_name    text NOT NULL DEFAULT '' CHECK (length(owner_name) <= 120),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT lpr_watchlist_items_list_fk FOREIGN KEY (watchlist_id, tenant_id)
        REFERENCES lpr_watchlists (id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT lpr_watchlist_items_unique_plate UNIQUE (watchlist_id, plate)
);
CREATE INDEX lpr_watchlist_items_tenant_plate_idx ON lpr_watchlist_items (tenant_id, upper(plate));
CREATE INDEX lpr_watchlist_items_list_idx ON lpr_watchlist_items (tenant_id, watchlist_id);

ALTER TABLE lpr_watchlists ENABLE ROW LEVEL SECURITY;
ALTER TABLE lpr_watchlists FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lpr_watchlists USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE lpr_watchlist_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE lpr_watchlist_items FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lpr_watchlist_items USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS lpr_watchlist_items;
DROP TABLE IF EXISTS lpr_watchlists;
