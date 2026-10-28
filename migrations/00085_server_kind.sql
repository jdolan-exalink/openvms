-- A server is a Frigate NVR (the only kind until now) or a Provision-ISR recorder. The kind decides
-- which client talks to it; existing rows are Frigate and stay as they are. Adding a kind is a new
-- migration that widens the CHECK.
-- +goose Up
ALTER TABLE frigate_servers
    ADD COLUMN kind text NOT NULL DEFAULT 'frigate' CHECK (kind IN ('frigate', 'provision_nvr'));

-- +goose Down
ALTER TABLE frigate_servers DROP COLUMN IF EXISTS kind;
