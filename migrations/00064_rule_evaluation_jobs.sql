-- Durable, event-scoped inputs for asynchronous rule evaluation.
-- +goose Up
CREATE TABLE rule_evaluation_jobs (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id      uuid NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    evaluation_kind text NOT NULL CHECK (evaluation_kind IN ('event', 'face_update')),
    face_names    text[] NOT NULL DEFAULT '{}',
    payload       jsonb NOT NULL,
    status        text NOT NULL DEFAULT 'completed' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    attempts      integer NOT NULL DEFAULT 0,
    available_at  timestamptz NOT NULL DEFAULT now(),
    lease_until   timestamptz,
    lease_token   uuid,
    last_error    text,
    enqueued_at   timestamptz NOT NULL DEFAULT now(),
    completed_at  timestamptz,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (event_id, evaluation_kind, face_names)
);

CREATE INDEX rule_evaluation_jobs_pending_idx ON rule_evaluation_jobs (available_at, enqueued_at, id)
    WHERE status IN ('pending', 'processing');

-- +goose Down
DROP TABLE IF EXISTS rule_evaluation_jobs;
