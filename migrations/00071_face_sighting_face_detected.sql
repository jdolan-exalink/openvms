-- FACE-07.5: the face detector's verdict on the retained crop. NULL means never checked, false means
-- the crop is not a face and stays out of embeddings and suggestions (it remains in the review inbox).
-- +goose Up
ALTER TABLE face_sightings ADD COLUMN face_detected boolean;
CREATE INDEX face_sightings_detect_pending_idx ON face_sightings (first_seen_at)
    WHERE review_status = 'pending' AND crop_key IS NOT NULL AND face_detected IS NULL;

-- +goose Down
DROP INDEX IF EXISTS face_sightings_detect_pending_idx;
ALTER TABLE face_sightings DROP COLUMN face_detected;
