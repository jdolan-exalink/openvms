-- config_revision_id points at the Frigate config revision made when a camera was removed from
-- Frigate; its before_yaml holds the camera's block, which restoring puts back. Null when the camera
-- was already absent from Frigate's config, or when the revision is gone.
-- +goose Up
ALTER TABLE camera_tombstones
    ADD COLUMN config_revision_id uuid REFERENCES frigate_config_revisions (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE camera_tombstones DROP COLUMN IF EXISTS config_revision_id;
