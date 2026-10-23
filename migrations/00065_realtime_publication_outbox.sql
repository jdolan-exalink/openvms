-- Durable tenant-scoped records for realtime JetStream publications.
-- +goose Up
CREATE TABLE realtime_publication_outbox (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    subject        text NOT NULL CHECK (length(btrim(subject)) > 0),
    payload        jsonb NOT NULL,
    status         text NOT NULL DEFAULT 'pending'
                   CHECK (status IN ('pending', 'processing', 'sent', 'failed')),
    attempts       integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at   timestamptz NOT NULL DEFAULT now(),
    lease_until    timestamptz,
    lease_token    uuid,
    last_error     text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    sent_at        timestamptz,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CHECK ((status = 'processing') = (lease_until IS NOT NULL AND lease_token IS NOT NULL)),
    CHECK ((status = 'sent') = (sent_at IS NOT NULL))
);

CREATE INDEX realtime_publication_outbox_due_idx
    ON realtime_publication_outbox (available_at, created_at, id)
    WHERE status IN ('pending', 'processing');

ALTER TABLE realtime_publication_outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE realtime_publication_outbox FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON realtime_publication_outbox
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS realtime_publication_outbox;
