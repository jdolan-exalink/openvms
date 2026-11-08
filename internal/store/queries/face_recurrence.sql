-- name: GetFaceRecurrenceSettings :one
SELECT * FROM face_recurrence_settings WHERE singleton;

-- name: UpdateFaceRecurrenceSettings :one
UPDATE face_recurrence_settings
SET min_appearances = @min_appearances,
    window_days = @window_days,
    updated_at = now(),
    updated_by = @updated_by
WHERE singleton
RETURNING *;
