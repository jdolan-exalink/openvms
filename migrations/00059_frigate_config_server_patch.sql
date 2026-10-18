-- +goose Up
-- FACE-04: a structured, server-wide section change (e.g. face_recognition) made through the
-- Frigate config editor gets its own revision kind, next to camera_patch / raw_save / rollback.
ALTER TABLE frigate_config_revisions DROP CONSTRAINT frigate_config_revisions_kind_check;
ALTER TABLE frigate_config_revisions ADD CONSTRAINT frigate_config_revisions_kind_check
    CHECK (kind IN ('camera_patch', 'server_patch', 'raw_save', 'rollback'));

-- +goose Down
DELETE FROM frigate_config_revisions WHERE kind = 'server_patch';
ALTER TABLE frigate_config_revisions DROP CONSTRAINT frigate_config_revisions_kind_check;
ALTER TABLE frigate_config_revisions ADD CONSTRAINT frigate_config_revisions_kind_check
    CHECK (kind IN ('camera_patch', 'raw_save', 'rollback'));
