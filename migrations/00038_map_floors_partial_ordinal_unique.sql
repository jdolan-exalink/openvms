-- +goose Up
ALTER TABLE map_floors DROP CONSTRAINT IF EXISTS map_floors_building_id_ordinal_key;
CREATE UNIQUE INDEX IF NOT EXISTS map_floors_building_ordinal_active_idx
ON map_floors (building_id, ordinal) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS map_floors_building_ordinal_active_idx;
ALTER TABLE map_floors ADD CONSTRAINT map_floors_building_id_ordinal_key UNIQUE (building_id, ordinal);
