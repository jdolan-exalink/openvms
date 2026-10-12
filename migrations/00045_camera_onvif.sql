-- +goose Up
ALTER TABLE cameras
    ADD COLUMN onvif_endpoint text,
    ADD COLUMN onvif_profile text,
    ADD COLUMN onvif_version text,
    ADD COLUMN onvif_capabilities jsonb NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE cameras
    DROP COLUMN onvif_capabilities,
    DROP COLUMN onvif_version,
    DROP COLUMN onvif_profile,
    DROP COLUMN onvif_endpoint;
