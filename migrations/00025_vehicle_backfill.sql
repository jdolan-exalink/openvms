-- +goose Up
-- Jobs created while catching up must fill the cards and must not raise alarms
-- for footage that already passed through the rules.
ALTER TABLE vehicle_attribute_jobs
    ADD COLUMN reevaluate boolean NOT NULL DEFAULT true;

UPDATE vehicle_attribute_jobs
SET status = 'pending', attempts = 0, last_error = '', reevaluate = false
WHERE status <> 'completed';

INSERT INTO vehicle_attribute_jobs (event_id, tenant_id, reevaluate)
SELECT e.id, e.tenant_id, false
FROM events e
WHERE e.labels && ARRAY['car', 'truck', 'bus', 'motorcycle']
ON CONFLICT (event_id) DO NOTHING;

-- +goose Down
ALTER TABLE vehicle_attribute_jobs DROP COLUMN IF EXISTS reevaluate;
