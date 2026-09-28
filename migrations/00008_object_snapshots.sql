-- +goose Up
-- M3-5b: has_snapshot must converge regardless of the order a review and its tracked objects
-- arrive in (review finding on commits 7658e50..6b8a355): internal/events/syncer.go's
-- markHasSnapshot only updates an events row that already exists, so a tracked object seen
-- before its review is indexed (or before the review's detection_ids widen to include it) was
-- silently lost once the object's own re-sync window (objectOverlap) closed.
--
-- object_snapshots persists every Frigate tracked-object id ever seen with has_snapshot=true,
-- the same role lpr_reads plays for plates. upsertReview derives events.has_snapshot from it by
-- detection_ids on every review upsert, exactly as it already derives events.plates from
-- lpr_reads, so the fact converges no matter which side is upserted first or again later.
CREATE TABLE object_snapshots (
    server_id         uuid NOT NULL REFERENCES frigate_servers (id),
    tenant_id         uuid NOT NULL REFERENCES tenants (id),
    remote_object_id  text NOT NULL,
    seen_at           timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, remote_object_id)
);

ALTER TABLE object_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE object_snapshots FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON object_snapshots USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS object_snapshots;
