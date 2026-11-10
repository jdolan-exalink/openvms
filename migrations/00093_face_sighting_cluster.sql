-- +goose Up
-- The group an unknown face belongs to, written by the worker so the faces page reads groups instead of
-- recomputing them on every request. It is the oldest member's sighting id. NULL means the face has not
-- been grouped yet and counts as a group of one.
ALTER TABLE face_sightings ADD COLUMN cluster_id uuid;
CREATE INDEX face_sightings_pending_cluster_idx ON face_sightings (tenant_id, cluster_id)
    WHERE review_status = 'pending';

-- +goose Down
DROP INDEX IF EXISTS face_sightings_pending_cluster_idx;
ALTER TABLE face_sightings DROP COLUMN IF EXISTS cluster_id;
