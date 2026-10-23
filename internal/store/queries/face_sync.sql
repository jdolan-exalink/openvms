-- name: LockFacePersonSync :exec
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 0));

-- name: BumpFacePersonRevision :one
UPDATE face_persons SET sync_revision = sync_revision + 1
WHERE tenant_id = @tenant_id AND id = @id
RETURNING sync_revision;

-- name: PropagateFaceRevision :exec
-- A new desired state makes every non-disabled job due again; disabled jobs keep waiting for
-- the capability to flip.
UPDATE face_server_sync
SET desired_revision = @desired_revision,
    status = CASE WHEN status = 'disabled' THEN 'disabled' ELSE 'pending' END,
    attempts = 0, hard_failures = 0, next_attempt_at = now(), lease_until = NULL, last_error = NULL, updated_at = now()
WHERE tenant_id = @tenant_id AND person_id = @person_id;

-- name: EnqueueFaceSyncRows :execrows
-- Every live person gets one job per server of its tenant; servers without face recognition
-- start as 'disabled'.
INSERT INTO face_server_sync (server_id, person_id, tenant_id, desired_revision, status, last_error)
SELECT s.id, p.id, p.tenant_id, p.sync_revision,
       CASE WHEN s.capabilities @> '{"face_recognition": true}'::jsonb THEN 'pending' ELSE 'disabled' END,
       CASE WHEN s.capabilities @> '{"face_recognition": true}'::jsonb THEN NULL ELSE 'face recognition is not enabled on this server' END
FROM face_persons p
JOIN frigate_servers s ON s.tenant_id = p.tenant_id AND s.deleted_at IS NULL
WHERE p.deleted_at IS NULL
ON CONFLICT (server_id, person_id) DO NOTHING;

-- name: DisableFaceSyncWithoutCapability :execrows
UPDATE face_server_sync AS f
SET status = 'disabled', last_error = 'face recognition is not enabled on this server', next_attempt_at = now(), updated_at = now()
FROM frigate_servers s
WHERE s.id = f.server_id AND f.status = 'pending'
  AND NOT (s.capabilities @> '{"face_recognition": true}'::jsonb);

-- name: EnableFaceSyncWithCapability :execrows
-- Disabled jobs become due again once the server reports the capability and any remote
-- "disabled" back-off has elapsed.
UPDATE face_server_sync AS f
SET status = 'pending', last_error = NULL, attempts = 0, next_attempt_at = now(), updated_at = now()
FROM frigate_servers s
WHERE s.id = f.server_id AND f.status = 'disabled' AND f.next_attempt_at <= now()
  AND s.capabilities @> '{"face_recognition": true}'::jsonb;

-- name: ClaimFaceSyncJobs :many
UPDATE face_server_sync AS job
SET status = 'processing', attempts = job.attempts + 1,
    lease_until = now() + make_interval(secs => @lease_seconds::float8), updated_at = now()
WHERE (job.server_id, job.person_id) IN (
    SELECT queued.server_id, queued.person_id FROM face_server_sync AS queued
    JOIN frigate_servers s ON s.id = queued.server_id AND s.deleted_at IS NULL
    WHERE s.capabilities @> '{"face_recognition": true}'::jsonb
      AND ((queued.status = 'pending' AND queued.next_attempt_at <= now())
        OR (queued.status = 'processing' AND queued.lease_until <= now()))
    ORDER BY queued.next_attempt_at, queued.created_at
    LIMIT @max_rows
    FOR UPDATE OF queued SKIP LOCKED
)
RETURNING job.*;

-- name: GetFaceServerSync :one
SELECT * FROM face_server_sync WHERE server_id = @server_id AND person_id = @person_id;

-- name: FinishFaceServerSync :execrows
-- Guarded by the claimed revision and the processing state: a newer desired revision (or a
-- retry/reset) makes this a no-op.
UPDATE face_server_sync SET
    status = @status,
    last_error = sqlc.narg('last_error'),
    next_attempt_at = @next_attempt_at,
    lease_until = NULL,
    synced_revision = CASE WHEN @status::text = 'synced' THEN desired_revision ELSE synced_revision END,
    last_synced_at = CASE WHEN @status::text = 'synced' THEN now() ELSE last_synced_at END,
    attempts = CASE WHEN @status::text = 'synced' THEN 0 ELSE attempts END,
    hard_failures = CASE WHEN @status::text = 'synced' THEN 0 WHEN @hard_failure::bool THEN hard_failures + 1 ELSE hard_failures END,
    updated_at = now()
WHERE server_id = @server_id AND person_id = @person_id
  AND desired_revision = @desired_revision AND status = 'processing';

-- name: SetFaceServerOwned :exec
UPDATE face_server_sync SET owned = true, updated_at = now() WHERE server_id = @server_id AND person_id = @person_id;

-- name: RetryFaceServerSync :execrows
UPDATE face_server_sync
SET status = 'pending', attempts = 0, hard_failures = 0, next_attempt_at = now(), lease_until = NULL, last_error = NULL, updated_at = now()
WHERE tenant_id = @tenant_id AND person_id = @person_id AND status IN ('failed', 'conflict');

-- name: ListFaceSyncStatus :many
SELECT f.server_id, s.name AS server_name, f.status, f.desired_revision, f.synced_revision, f.owned,
       f.last_error, f.last_synced_at, f.updated_at
FROM face_server_sync f
JOIN frigate_servers s ON s.id = f.server_id
WHERE f.tenant_id = @tenant_id AND f.person_id = @person_id
ORDER BY lower(s.name), f.server_id;

-- name: GetFacePersonAny :one
SELECT * FROM face_persons WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetFaceSyncServer :one
SELECT * FROM frigate_servers WHERE id = @id AND deleted_at IS NULL;

-- name: ListFaceServerImages :many
SELECT * FROM face_server_images WHERE server_id = @server_id AND person_id = @person_id ORDER BY created_at, image_id;

-- name: InsertFaceServerImage :exec
INSERT INTO face_server_images (server_id, image_id, person_id, tenant_id, frigate_image_id)
VALUES (@server_id, @image_id, @person_id, @tenant_id, @frigate_image_id)
ON CONFLICT (server_id, image_id) DO UPDATE SET frigate_image_id = EXCLUDED.frigate_image_id;

-- name: DeleteFaceServerImage :exec
DELETE FROM face_server_images WHERE server_id = @server_id AND image_id = @image_id;

-- name: DeleteFaceServerImagesForPerson :exec
DELETE FROM face_server_images WHERE server_id = @server_id AND person_id = @person_id;

-- name: ListPurgeableFaceTombstones :many
-- A tombstone is purgeable once no live, face-capable server still holds a face OpenVMS owns
-- for it. Soft-deleted servers have nothing to delete; face-disabled ones get a cleanup row.
SELECT p.* FROM face_persons p
WHERE p.deleted_at IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM face_server_sync f
      JOIN frigate_servers s ON s.id = f.server_id AND s.deleted_at IS NULL
        AND s.capabilities @> '{"face_recognition": true}'::jsonb
      WHERE f.person_id = p.id AND f.owned
        AND NOT (f.status = 'synced' AND f.synced_revision = f.desired_revision))
ORDER BY p.deleted_at, p.id
LIMIT @max_rows;

-- name: ListPendingFaceTombstones :many
-- Show managers only deleted persons that still have an owned face on a live server.
SELECT p.id, p.display_name, p.deleted_at, count(*)::int AS pending_server_count
FROM face_persons p
JOIN face_server_sync f ON f.person_id = p.id AND f.tenant_id = p.tenant_id AND f.owned
JOIN frigate_servers s ON s.id = f.server_id AND s.deleted_at IS NULL
WHERE p.tenant_id = @tenant_id AND p.deleted_at IS NOT NULL
  AND NOT (f.status = 'synced' AND f.synced_revision = f.desired_revision)
GROUP BY p.id
ORDER BY p.deleted_at, p.id;

-- name: FaceTombstonePurgeable :one
SELECT EXISTS (
    SELECT 1 FROM face_persons p
    WHERE p.tenant_id = @tenant_id AND p.id = @id AND p.deleted_at IS NOT NULL
      AND NOT EXISTS (
          SELECT 1 FROM face_server_sync f
          JOIN frigate_servers s ON s.id = f.server_id AND s.deleted_at IS NULL
            AND s.capabilities @> '{"face_recognition": true}'::jsonb
          WHERE f.person_id = p.id AND f.owned
            AND NOT (f.status = 'synced' AND f.synced_revision = f.desired_revision)));

-- name: ScrubEventSubLabel :execrows
-- Removes a recognized name from stored event sub_labels in batches. Exact element match, so it
-- is idempotent and never touches other labels. Rows locked by a concurrent writer are waited
-- for, never skipped: a skipped row would keep the name after the person row is purged.
WITH batch AS (
    SELECT e.id FROM events e
    WHERE e.tenant_id = @tenant_id AND e.sub_labels @> ARRAY[@name::text]
    LIMIT @max_rows
    FOR UPDATE
)
UPDATE events AS ev SET sub_labels = array_remove(ev.sub_labels, @name::text)
FROM batch WHERE ev.id = batch.id;

-- name: CountEventsWithSubLabel :one
SELECT count(*) FROM events WHERE tenant_id = @tenant_id AND sub_labels @> ARRAY[@name::text];

-- name: RecordFaceErasedName :exec
INSERT INTO face_erased_names (tenant_id, frigate_name) VALUES (@tenant_id, @name)
ON CONFLICT (tenant_id, frigate_name) DO UPDATE SET erased_at = now();

-- name: ClearFaceErasedName :exec
DELETE FROM face_erased_names WHERE tenant_id = @tenant_id AND frigate_name = @name;

-- name: PruneFaceErasedNames :execrows
DELETE FROM face_erased_names WHERE erased_at < now() - make_interval(secs => @retention_seconds::float8);

-- name: RecordFaceRemoteCleanup :exec
-- Automatic tombstone purge (Syncer.purgeOne): record cleanup only for owned, non-converged
-- faces on servers that currently lack face recognition. A face-capable server is deleted
-- from by the normal sync before the purge, so it never needs a deferred obligation.
-- RecordFaceForcedRemoteCleanup is the operator variant without the capability condition.
INSERT INTO face_remote_cleanup (tenant_id, server_id, frigate_name)
SELECT f.tenant_id, f.server_id, @name::text
FROM face_server_sync f
JOIN frigate_servers s ON s.id = f.server_id AND s.deleted_at IS NULL
WHERE f.tenant_id = @tenant_id AND f.person_id = @id AND f.owned
  AND NOT (s.capabilities @> '{"face_recognition": true}'::jsonb)
  AND NOT (f.status = 'synced' AND f.synced_revision = f.desired_revision)
ON CONFLICT DO NOTHING;

-- name: RecordFaceForcedRemoteCleanup :exec
-- Operator force purge (Service.ForcePurgeTombstone). The only difference from
-- RecordFaceRemoteCleanup is that it drops the "server lacks face recognition" condition: an
-- operator may purge while a live server is unreachable or face-disabled, so keep a durable
-- cleanup obligation for every owned face that has not converged, regardless of current
-- capability; the worker only attempts it after the server reports face recognition enabled.
INSERT INTO face_remote_cleanup (tenant_id, server_id, frigate_name)
SELECT f.tenant_id, f.server_id, @name::text
FROM face_server_sync f
JOIN frigate_servers s ON s.id = f.server_id AND s.deleted_at IS NULL
WHERE f.tenant_id = @tenant_id AND f.person_id = @id AND f.owned
  AND NOT (f.status = 'synced' AND f.synced_revision = f.desired_revision)
ON CONFLICT DO NOTHING;

-- name: ListDueFaceRemoteCleanups :many
SELECT c.* FROM face_remote_cleanup c
JOIN frigate_servers s ON s.id = c.server_id AND s.deleted_at IS NULL
  AND s.capabilities @> '{"face_recognition": true}'::jsonb
ORDER BY c.created_at
LIMIT @max_rows;

-- name: DeleteFaceRemoteCleanup :exec
DELETE FROM face_remote_cleanup WHERE server_id = @server_id AND frigate_name = @frigate_name;

-- name: PurgeFacePerson :execrows
DELETE FROM face_persons WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NOT NULL;
