-- When the history of a deleted camera was purged, after its recording window passed. The tombstone
-- and the camera row stay, so the Frigate sync still skips the name and the camera can be restored.
-- +goose Up
ALTER TABLE camera_tombstones ADD COLUMN purged_at timestamptz;
CREATE INDEX camera_tombstones_due_idx ON camera_tombstones (deleted_at) WHERE purged_at IS NULL AND retain_days IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS camera_tombstones_due_idx;
ALTER TABLE camera_tombstones DROP COLUMN IF EXISTS purged_at;
