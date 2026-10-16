-- P5-02-02-R1: retain the old remote key until a rename is fully applied.
-- +goose Up
ALTER TABLE camera_zone_sync_jobs ADD COLUMN previous_remote_name text;

-- +goose Down
ALTER TABLE camera_zone_sync_jobs DROP COLUMN previous_remote_name;
