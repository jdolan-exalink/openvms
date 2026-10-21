-- Platform admin password recovery: a short-lived 6-digit code written to the API log. One
-- active code per user (a new request replaces it); the code is stored salted and hashed, and
-- the row is deleted once used and replaced by the next request; an expired or exhausted code
-- stays (unusable) so the per-account request cooldown still applies.
-- +goose Up
CREATE TABLE password_recovery_codes (
    user_id      uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    code_salt    bytea NOT NULL,
    code_hash    bytea NOT NULL,
    attempts     integer NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS password_recovery_codes;
