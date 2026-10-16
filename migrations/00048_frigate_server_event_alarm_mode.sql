-- +goose Up
-- Existing Frigate servers keep opening event-source alarms on every Frigate alert until changed
-- in server settings; servers registered after this migration (and fresh installs) default to
-- rules_only so only rule-based create_alarm actions open alarms.
ALTER TABLE frigate_servers
    ADD COLUMN event_alarm_mode text NOT NULL DEFAULT 'all_alerts'
    CHECK (event_alarm_mode IN ('all_alerts', 'rules_only'));

ALTER TABLE frigate_servers
    ALTER COLUMN event_alarm_mode SET DEFAULT 'rules_only';

-- +goose Down
ALTER TABLE frigate_servers DROP COLUMN IF EXISTS event_alarm_mode;
