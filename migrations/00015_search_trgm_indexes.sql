-- +goose Up
-- S2-4: Trigram indexes for fast global search (ILIKE / similarity) on cameras, sites, and servers.
CREATE INDEX cameras_display_name_trgm_idx ON cameras USING gin (display_name gin_trgm_ops);
CREATE INDEX cameras_location_trgm_idx ON cameras USING gin (location gin_trgm_ops);
CREATE INDEX sites_name_trgm_idx ON sites USING gin (name gin_trgm_ops);
CREATE INDEX frigate_servers_name_trgm_idx ON frigate_servers USING gin (name gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS frigate_servers_name_trgm_idx;
DROP INDEX IF EXISTS sites_name_trgm_idx;
DROP INDEX IF EXISTS cameras_location_trgm_idx;
DROP INDEX IF EXISTS cameras_display_name_trgm_idx;
