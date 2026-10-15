-- +goose Up
ALTER TABLE lpr_reads
    ADD COLUMN crop_key text NOT NULL DEFAULT '',
    ADD COLUMN crop_attempts int NOT NULL DEFAULT 0;

CREATE INDEX lpr_reads_crop_pending_idx ON lpr_reads (server_id) WHERE crop_key = '';

-- +goose Down
DROP INDEX IF EXISTS lpr_reads_crop_pending_idx;
ALTER TABLE lpr_reads
    DROP COLUMN IF EXISTS crop_attempts,
    DROP COLUMN IF EXISTS crop_key;
