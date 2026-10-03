-- +goose Up
-- Plate lists order by seen_at and, on the map, also by site. These let the page
-- be taken before the vehicle lookup instead of walking every historical read.
CREATE INDEX IF NOT EXISTS lpr_reads_seen_at_idx ON lpr_reads (seen_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS lpr_reads_site_seen_at_idx ON lpr_reads (site_id, seen_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS lpr_reads_site_seen_at_idx;
DROP INDEX IF EXISTS lpr_reads_seen_at_idx;
