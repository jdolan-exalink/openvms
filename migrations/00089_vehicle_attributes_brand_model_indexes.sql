-- +goose Up
CREATE INDEX IF NOT EXISTS vehicle_attributes_brand_trgm_idx ON vehicle_attributes USING gin (brand gin_trgm_ops) WHERE brand <> '';
CREATE INDEX IF NOT EXISTS vehicle_attributes_model_trgm_idx ON vehicle_attributes USING gin (model gin_trgm_ops) WHERE model <> '';

-- +goose Down
DROP INDEX IF EXISTS vehicle_attributes_model_trgm_idx;
DROP INDEX IF EXISTS vehicle_attributes_brand_trgm_idx;
