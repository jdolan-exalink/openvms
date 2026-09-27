-- +goose Up
-- M4: interactive login (PRD §21-23): passwords (argon2id), sessions, TOTP MFA and
-- lockout after repeated failures.

ALTER TABLE users
    ADD COLUMN password_hash        text NOT NULL DEFAULT '',
    ADD COLUMN password_changed_at  timestamptz,
    ADD COLUMN must_change_password boolean NOT NULL DEFAULT false,
    -- TOTP secret sealed with the master key, bound to the user id. mfa_enabled only turns
    -- on after the user proved a code from it.
    ADD COLUMN mfa_secret_sealed    bytea,
    ADD COLUMN mfa_enabled          boolean NOT NULL DEFAULT false,
    ADD COLUMN failed_logins        int NOT NULL DEFAULT 0,
    ADD COLUMN locked_until         timestamptz,
    ADD COLUMN last_login_at        timestamptz;

-- Browser sessions. Only the SHA-256 of the cookie value is stored.
CREATE TABLE sessions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users (id),
    token_hash    bytea NOT NULL UNIQUE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    -- Absolute end; idle expiry is checked against last_seen_at.
    expires_at    timestamptz NOT NULL,
    revoked_at    timestamptz,
    ip            inet,
    user_agent    text NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_idx ON sessions (user_id) WHERE revoked_at IS NULL;

CREATE INDEX audit_log_action_idx ON audit_log (action, occurred_at DESC);
CREATE INDEX audit_log_time_idx ON audit_log (occurred_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS audit_log_time_idx;
DROP INDEX IF EXISTS audit_log_action_idx;
DROP TABLE IF EXISTS sessions;
ALTER TABLE users
    DROP COLUMN IF EXISTS password_hash,
    DROP COLUMN IF EXISTS password_changed_at,
    DROP COLUMN IF EXISTS must_change_password,
    DROP COLUMN IF EXISTS mfa_secret_sealed,
    DROP COLUMN IF EXISTS mfa_enabled,
    DROP COLUMN IF EXISTS failed_logins,
    DROP COLUMN IF EXISTS locked_until,
    DROP COLUMN IF EXISTS last_login_at;
