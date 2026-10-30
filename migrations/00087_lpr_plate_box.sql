-- +goose Up
ALTER TABLE lpr_reads
    ADD COLUMN plate_box real[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE lpr_reads
    DROP COLUMN IF EXISTS plate_box;
