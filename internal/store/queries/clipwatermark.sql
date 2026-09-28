-- name: InsertClipWatermarkJob :one
INSERT INTO clip_watermark_jobs
    (tenant_id, site_id, server_id, camera_id, lpr_read_id, remote_event_id, requested_by, watermark_text, logo_key)
VALUES (@tenant_id, @site_id, @server_id, @camera_id, @lpr_read_id, @remote_event_id, @requested_by, @watermark_text, @logo_key)
RETURNING *;

-- name: GetClipWatermarkJob :one
SELECT * FROM clip_watermark_jobs WHERE id = @id;

-- name: ClaimNextClipWatermarkJob :one
UPDATE clip_watermark_jobs SET status = 'running', updated_at = now()
WHERE id = (
    SELECT id FROM clip_watermark_jobs
    WHERE status = 'queued'
    ORDER BY created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: MarkClipWatermarkJobDone :exec
UPDATE clip_watermark_jobs SET status = 'done', output_key = @output_key, updated_at = now() WHERE id = @id;

-- name: MarkClipWatermarkJobFailed :exec
UPDATE clip_watermark_jobs SET status = 'failed', error = @error, updated_at = now() WHERE id = @id;

-- name: SetClipWatermarkJobLogoKey :exec
UPDATE clip_watermark_jobs SET logo_key = @logo_key, updated_at = now() WHERE id = @id;

-- name: FailStuckClipWatermarkJobs :many
-- PDW-6: a worker process killed between claiming a job (status -> 'running') and marking it
-- done/failed leaves the row stuck "running" forever, since nothing else ever revisits it.
-- Fails every job that has been "running" since before @cutoff (the caller computes
-- now() - stuckTimeout), mirroring internal/media/exports.go's exportTimeout sweep.
UPDATE clip_watermark_jobs SET status = 'failed', error = @error, updated_at = now()
WHERE status = 'running' AND updated_at < @cutoff
RETURNING id;
