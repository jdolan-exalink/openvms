-- name: ListRules :many
SELECT id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at, version
FROM rules
WHERE tenant_id = @tenant_id
ORDER BY name ASC, created_at DESC;

-- name: GetRule :one
SELECT id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at, version
FROM rules
WHERE id = @id AND tenant_id = @tenant_id;

-- name: CreateRule :one
INSERT INTO rules (tenant_id, name, trigger_type, conditions, actions, enabled)
VALUES (@tenant_id, @name, @trigger_type, @conditions, @actions, @enabled)
RETURNING id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at, version;

-- name: UpdateRule :one
UPDATE rules
SET
  name = coalesce(sqlc.narg('name'), name),
  trigger_type = coalesce(sqlc.narg('trigger_type'), trigger_type),
  conditions = coalesce(sqlc.narg('conditions'), conditions),
  actions = coalesce(sqlc.narg('actions'), actions),
  enabled = coalesce(sqlc.narg('enabled'), enabled),
  version = version + 1,
  updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND version = @expected_version
RETURNING id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at, version;

-- name: DeleteRule :exec
DELETE FROM rules
WHERE id = @id AND tenant_id = @tenant_id;

-- name: ListActiveRulesByTrigger :many
SELECT id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at, version
FROM rules
WHERE tenant_id = @tenant_id AND trigger_type = @trigger_type AND enabled = true
ORDER BY created_at ASC;

-- name: InsertRuleVersion :exec
INSERT INTO rule_versions (rule_id, tenant_id, version, snapshot, changed_by)
VALUES (@rule_id, @tenant_id, @version, @snapshot, @changed_by);

-- name: ListRuleVersions :many
SELECT version, snapshot, changed_by, created_at
FROM rule_versions
WHERE tenant_id = @tenant_id AND rule_id = @rule_id
ORDER BY version DESC;

-- name: ListCameraGroupsForCamera :many
SELECT group_id FROM camera_group_members
WHERE tenant_id = @tenant_id AND camera_id = @camera_id
ORDER BY group_id;

-- name: CountCamerasByIDs :one
SELECT count(*) FROM cameras
WHERE tenant_id = @tenant_id AND deleted_at IS NULL AND id = ANY(@ids::uuid[]);

-- name: CountCameraGroupsByIDs :one
SELECT count(*) FROM camera_groups
WHERE tenant_id = @tenant_id AND deleted_at IS NULL AND id = ANY(@ids::uuid[]);

-- name: CountSitesByIDs :one
SELECT count(*) FROM sites
WHERE tenant_id = @tenant_id AND deleted_at IS NULL AND id = ANY(@ids::uuid[]);

-- name: CountServersByIDs :one
SELECT count(*) FROM frigate_servers
WHERE tenant_id = @tenant_id AND deleted_at IS NULL AND id = ANY(@ids::uuid[]);

-- name: CountMapZonesByIDs :one
SELECT count(*) FROM map_zones
WHERE tenant_id = @tenant_id AND deleted_at IS NULL AND id = ANY(@ids::uuid[]);

-- name: EnqueueRuleEvaluation :exec
INSERT INTO rule_evaluation_jobs (event_id, evaluation_kind, face_names, payload, status)
VALUES (@event_id, @evaluation_kind, @face_names, @payload, 'completed')
ON CONFLICT (event_id, evaluation_kind, face_names) DO NOTHING;

-- name: ClaimRuleEvaluationJobs :many
WITH exhausted AS (
    UPDATE rule_evaluation_jobs
    SET status = 'failed', lease_until = NULL,
        last_error = coalesce(last_error, 'attempt limit reached'), updated_at = now()
    WHERE status = 'processing' AND lease_until <= now() AND attempts >= @max_attempts
)
UPDATE rule_evaluation_jobs AS job
SET status = 'processing', attempts = job.attempts + 1,
    lease_until = now() + make_interval(secs => @lease_seconds::float8),
    lease_token = gen_random_uuid(), updated_at = now()
WHERE job.id IN (
    SELECT queued.id FROM rule_evaluation_jobs AS queued
    WHERE queued.attempts < @max_attempts
      AND ((queued.status = 'pending' AND queued.available_at <= now())
        OR (queued.status = 'processing' AND queued.lease_until <= now()))
    ORDER BY queued.available_at, queued.enqueued_at, queued.id
    LIMIT @max_rows
    FOR UPDATE SKIP LOCKED
)
RETURNING job.*;

-- name: CompleteRuleEvaluationJob :execrows
UPDATE rule_evaluation_jobs
SET status = 'completed', lease_until = NULL, lease_token = NULL, last_error = NULL,
    completed_at = now(), updated_at = now()
WHERE id = @id AND status = 'processing' AND lease_token = @lease_token;

-- name: RetryRuleEvaluationJob :execrows
UPDATE rule_evaluation_jobs
SET status = CASE WHEN attempts >= @max_attempts THEN 'failed' ELSE 'pending' END,
    available_at = now() + make_interval(secs => @retry_seconds::float8),
    lease_until = NULL, lease_token = NULL, last_error = @last_error, updated_at = now()
WHERE id = @id AND status = 'processing' AND lease_token = @lease_token;
