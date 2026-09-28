-- +goose Up
-- M3-5b SECURITY finding: sub_label redaction/filter gating (commit 7658e50) keys on
-- cameras.lpr, the camera's CURRENT LPR flag. If a camera is switched from LPR to non-LPR after
-- events were ingested, those historical events' plate text in sub_labels becomes visible and
-- searchable to actors without lpr.view/lpr.search, since the (NOT c.lpr OR ...) / "if cam.Lpr"
-- checks in internal/events/service.go stop gating them.
--
-- events.lpr records the camera's LPR capability at ingestion time (set in
-- internal/events/syncer.go upsertReview, once true never reset, mirroring has_snapshot/plates).
-- Gating now requires EITHER the event's own ingest-time flag OR the camera's current flag
-- (conservative: a camera can only ever gain gating by being reconfigured, never lose it).
ALTER TABLE events ADD COLUMN lpr boolean NOT NULL DEFAULT false;

-- Backfill: conservatively treat an existing event as LPR-ingested when its camera is currently
-- LPR-capable, or when it already carries a plate (proof it was recognized on an LPR camera at
-- some point, even if the camera's flag changed since).
UPDATE events e SET lpr = true
FROM cameras c
WHERE c.id = e.camera_id AND e.lpr = false AND (c.lpr OR cardinality(e.plates) > 0);

-- +goose Down
ALTER TABLE events DROP COLUMN IF EXISTS lpr;
