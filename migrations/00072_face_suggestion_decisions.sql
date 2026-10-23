-- FACE-07.6: operator verdicts on suggested pairs of sightings. A suggestion is between identities
-- (a sighting or a group), but identity IDs change when groups merge, so the verdict is keyed by
-- one sighting from each side, which never changes. "different" hides the two identities that
-- those sightings belong to; both verdicts keep the similarity the operator saw, so the suggestion
-- threshold can be calibrated on real decisions. Rows hold sighting IDs and a number, never
-- biometric data, and outlive the crops they refer to.
-- +goose Up
CREATE TABLE face_suggestion_decisions (
    tenant_id   uuid NOT NULL REFERENCES tenants (id),
    sighting_lo uuid NOT NULL,
    sighting_hi uuid NOT NULL,
    decision    text NOT NULL CHECK (decision IN ('same', 'different')),
    similarity  real NOT NULL CHECK (similarity >= 0 AND similarity <= 1),
    decided_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    decided_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, sighting_lo, sighting_hi),
    CHECK (sighting_lo < sighting_hi)
);

ALTER TABLE face_suggestion_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE face_suggestion_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON face_suggestion_decisions
    USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS face_suggestion_decisions;
