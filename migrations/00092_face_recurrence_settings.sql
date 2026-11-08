-- +goose Up
-- Platform-wide rule that marks a group of unknown faces as recurrent: min_appearances sightings inside
-- the last window_days days. One row, not per tenant, so no RLS. The defaults match the rule the faces
-- page has always used (three appearances in a week).
CREATE TABLE face_recurrence_settings (
    singleton       boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    min_appearances integer NOT NULL DEFAULT 3 CHECK (min_appearances BETWEEN 1 AND 100),
    window_days     integer NOT NULL DEFAULT 7 CHECK (window_days BETWEEN 1 AND 365),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    updated_by      uuid REFERENCES users (id) ON DELETE SET NULL
);
INSERT INTO face_recurrence_settings (singleton) VALUES (true);

-- +goose Down
DROP TABLE IF EXISTS face_recurrence_settings;
