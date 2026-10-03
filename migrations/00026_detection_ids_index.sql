-- +goose Up
-- Plate search looks up the review that contains a tracked-object id. Without this
-- index that lookup scans every event for every plate read.
CREATE INDEX IF NOT EXISTS events_detection_ids_idx ON events USING gin (detection_ids);

-- A car that also carried a motorcycle label was stored as a motorcycle.
UPDATE vehicle_attributes va
SET vehicle_type = 'car', vehicle_type_confidence = 0.75
FROM events e
WHERE e.id = va.event_id
  AND va.vehicle_type = 'motorcycle'
  AND 'car' = ANY(e.labels);

-- +goose Down
DROP INDEX IF EXISTS events_detection_ids_idx;
