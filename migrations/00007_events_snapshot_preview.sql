-- +goose Up
-- M3-5: has_snapshot / has_preview event flags and filters (PRD §44).
--
-- has_snapshot mirrors a real Frigate fact (Event.has_snapshot from GET /api/events), set true
-- once any tracked object linked to this review (events.detection_ids) reports it (see
-- internal/events/syncer.go).
--
-- preview_key mirrors thumb_key (PRD §20): the object-storage key of this event's preview clip,
-- once copied centrally. Nothing populates it yet (no preview-copy job exists, unlike
-- copyThumbnails for thumb_key); has_preview is computed as `preview_key <> ''`, so it reports
-- false for every event until a future task adds that copy job. This is an OpenVMS-internal
-- signal, not a probe of Frigate's own preview availability (Frigate has no per-event "has
-- preview" fact — its preview feature is a per-camera timelapse over a time range).
ALTER TABLE events
    ADD COLUMN has_snapshot boolean NOT NULL DEFAULT false,
    ADD COLUMN preview_key  text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE events
    DROP COLUMN IF EXISTS has_snapshot,
    DROP COLUMN IF EXISTS preview_key;
