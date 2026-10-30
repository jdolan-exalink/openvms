-- +goose Up
ALTER TABLE vehicle_attributes
    ADD COLUMN brand text NOT NULL DEFAULT '',
    ADD COLUMN model text NOT NULL DEFAULT '';

ALTER TABLE lpr_reads
    ADD COLUMN snapshot_key text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE lpr_reads
    DROP COLUMN IF EXISTS snapshot_key;

ALTER TABLE vehicle_attributes
    DROP COLUMN IF EXISTS model,
    DROP COLUMN IF EXISTS brand;
