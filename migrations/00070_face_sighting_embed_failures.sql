-- FACE-07.2: a crop that cannot be embedded must not starve the queue behind it. Failures are
-- counted so the embedder retries a bounded number of times and tries fresh crops first.
-- +goose Up
ALTER TABLE face_sightings ADD COLUMN embed_failures smallint NOT NULL DEFAULT 0 CHECK (embed_failures >= 0);
DROP INDEX IF EXISTS face_sightings_embed_pending_idx;
CREATE INDEX face_sightings_embed_pending_idx ON face_sightings (embed_failures, first_seen_at)
    WHERE review_status = 'pending' AND crop_key IS NOT NULL AND embedding IS NULL;

-- +goose Down
DROP INDEX IF EXISTS face_sightings_embed_pending_idx;
CREATE INDEX face_sightings_embed_pending_idx ON face_sightings (first_seen_at)
    WHERE review_status = 'pending' AND crop_key IS NOT NULL AND embedding IS NULL;
ALTER TABLE face_sightings DROP COLUMN IF EXISTS embed_failures;
