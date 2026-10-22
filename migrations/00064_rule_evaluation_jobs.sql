-- Durable, event-scoped inputs for asynchronous rule evaluation.
-- +goose Up
CREATE TABLE rule_evaluation_jobs (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id      uuid NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    evaluation_kind text NOT NULL CHECK (evaluation_kind IN ('event', 'face_update')),
    face_names    text[] NOT NULL DEFAULT '{}',
    payload       jsonb NOT NULL,
    enqueued_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (event_id, evaluation_kind, face_names)
);

CREATE INDEX rule_evaluation_jobs_pending_idx ON rule_evaluation_jobs (enqueued_at, id);

-- +goose Down
DROP TABLE IF EXISTS rule_evaluation_jobs;
