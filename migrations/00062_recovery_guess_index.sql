-- Bounds the per-account daily count of wrong recovery codes: without it the count scans every
-- historical PASSWORD_RECOVERY_FAILED row, which anonymous callers can grow.
-- +goose Up
CREATE INDEX audit_log_recovery_guesses_idx ON audit_log (target_id, occurred_at DESC)
WHERE action = 'PASSWORD_RECOVERY_FAILED' AND details->>'reason' = 'wrong_code';

-- +goose Down
DROP INDEX IF EXISTS audit_log_recovery_guesses_idx;
