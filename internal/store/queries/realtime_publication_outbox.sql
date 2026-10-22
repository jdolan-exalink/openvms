-- name: EnqueueRealtimePublication :one
INSERT INTO realtime_publication_outbox (tenant_id, subject, payload)
VALUES (@tenant_id, @subject, @payload)
RETURNING *;

-- name: ClaimRealtimePublications :many
WITH exhausted AS (
    UPDATE realtime_publication_outbox
    SET status = 'failed', lease_until = NULL, lease_token = NULL,
        last_error = coalesce(last_error, 'attempt limit reached'), updated_at = now()
    WHERE status = 'processing' AND lease_until <= now() AND attempts >= @max_attempts
)
UPDATE realtime_publication_outbox AS publication
SET status = 'processing', attempts = publication.attempts + 1,
    lease_until = now() + make_interval(secs => @lease_seconds::float8),
    lease_token = gen_random_uuid(), updated_at = now()
WHERE publication.id IN (
    SELECT queued.id FROM realtime_publication_outbox AS queued
    WHERE queued.attempts < @max_attempts
      AND ((queued.status = 'pending' AND queued.available_at <= now())
        OR (queued.status = 'processing' AND queued.lease_until <= now()))
    ORDER BY queued.available_at, queued.created_at, queued.id
    LIMIT @max_rows
    FOR UPDATE SKIP LOCKED
)
RETURNING publication.*;

-- name: MarkRealtimePublicationSent :execrows
UPDATE realtime_publication_outbox
SET status = 'sent', lease_until = NULL, lease_token = NULL,
    sent_at = now(), last_error = NULL, updated_at = now()
WHERE id = @id AND status = 'processing' AND lease_token = @lease_token;

-- name: RetryRealtimePublication :execrows
UPDATE realtime_publication_outbox
SET status = CASE WHEN attempts >= @max_attempts THEN 'failed' ELSE 'pending' END,
    available_at = now() + make_interval(secs => @retry_seconds::float8),
    lease_until = NULL, lease_token = NULL, last_error = @last_error, updated_at = now()
WHERE id = @id AND status = 'processing' AND lease_token = @lease_token;
