-- Whether a Frigate config revision was kept for the camera when it was deleted. config_revision_id
-- becomes null if that revision is ever removed, so this tells "there was never a block to restore"
-- apart from "the block was lost", and the restore refuses the second instead of reporting success
-- for a camera that would not record.
-- +goose Up
ALTER TABLE camera_tombstones ADD COLUMN had_frigate_config boolean NOT NULL DEFAULT false;
UPDATE camera_tombstones SET had_frigate_config = true WHERE config_revision_id IS NOT NULL;

-- +goose Down
ALTER TABLE camera_tombstones DROP COLUMN IF EXISTS had_frigate_config;
