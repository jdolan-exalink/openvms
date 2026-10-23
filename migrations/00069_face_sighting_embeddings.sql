-- FACE-07.2: central embeddings for retained face crops. Vectors are only comparable when
-- embedding_profile and embedding_model match, so both are stored with the vector.
-- +goose Up
ALTER TABLE face_sightings
    ADD COLUMN embedding real[],
    ADD COLUMN embedding_profile text,
    ADD COLUMN embedding_model text,
    ADD COLUMN embedded_at timestamptz,
    ADD CONSTRAINT face_sightings_embedding_complete CHECK (
        (embedding IS NULL AND embedding_profile IS NULL AND embedding_model IS NULL AND embedded_at IS NULL)
        OR (embedding IS NOT NULL AND cardinality(embedding) = 512
            AND embedding_profile IS NOT NULL AND embedding_model IS NOT NULL AND embedded_at IS NOT NULL)
    );
CREATE INDEX face_sightings_embed_pending_idx ON face_sightings (first_seen_at)
    WHERE review_status = 'pending' AND crop_key IS NOT NULL AND embedding IS NULL;

-- +goose Down
DROP INDEX IF EXISTS face_sightings_embed_pending_idx;
ALTER TABLE face_sightings
    DROP CONSTRAINT IF EXISTS face_sightings_embedding_complete,
    DROP COLUMN IF EXISTS embedded_at,
    DROP COLUMN IF EXISTS embedding_model,
    DROP COLUMN IF EXISTS embedding_profile,
    DROP COLUMN IF EXISTS embedding;
