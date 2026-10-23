-- Add tenant-scoped ordering metadata for publications that belong to one aggregate.
-- +goose Up
CREATE TABLE realtime_publication_aggregate_sequences (
    tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    aggregate_key text NOT NULL CHECK (length(btrim(aggregate_key)) > 0),
    last_sequence bigint NOT NULL CHECK (last_sequence > 0),
    PRIMARY KEY (tenant_id, aggregate_key)
);

ALTER TABLE realtime_publication_aggregate_sequences ENABLE ROW LEVEL SECURITY;
ALTER TABLE realtime_publication_aggregate_sequences FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON realtime_publication_aggregate_sequences
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE realtime_publication_outbox
    ADD COLUMN aggregate_key text,
    ADD COLUMN aggregate_sequence bigint,
    ADD CONSTRAINT realtime_publication_outbox_aggregate_pair_check
        CHECK ((aggregate_key IS NULL) = (aggregate_sequence IS NULL)),
    ADD CONSTRAINT realtime_publication_outbox_aggregate_sequence_check
        CHECK (aggregate_sequence IS NULL OR aggregate_sequence > 0);

CREATE UNIQUE INDEX realtime_publication_outbox_aggregate_sequence_idx
    ON realtime_publication_outbox (tenant_id, aggregate_key, aggregate_sequence)
    WHERE aggregate_key IS NOT NULL;

CREATE INDEX realtime_publication_outbox_aggregate_pending_idx
    ON realtime_publication_outbox (tenant_id, aggregate_key, aggregate_sequence)
    WHERE aggregate_key IS NOT NULL AND status IN ('pending', 'processing');

-- +goose Down
DROP INDEX IF EXISTS realtime_publication_outbox_aggregate_pending_idx;
DROP INDEX IF EXISTS realtime_publication_outbox_aggregate_sequence_idx;
ALTER TABLE realtime_publication_outbox
    DROP CONSTRAINT IF EXISTS realtime_publication_outbox_aggregate_sequence_check,
    DROP CONSTRAINT IF EXISTS realtime_publication_outbox_aggregate_pair_check,
    DROP COLUMN IF EXISTS aggregate_sequence,
    DROP COLUMN IF EXISTS aggregate_key;
DROP TABLE IF EXISTS realtime_publication_aggregate_sequences;
