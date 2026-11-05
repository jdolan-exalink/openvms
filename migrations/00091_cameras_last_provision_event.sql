-- +goose Up
-- When central last received an analytic that the camera itself (Provision) reported through the edge agent.
-- Written by the agent ingestion; read by the reception LED. NULL until the first one arrives after this
-- migration: there is no backfill, a scan of events by origin has no index and would be slow on a big table.
ALTER TABLE cameras ADD COLUMN last_provision_event_at timestamptz;

-- +goose Down
ALTER TABLE cameras DROP COLUMN last_provision_event_at;
