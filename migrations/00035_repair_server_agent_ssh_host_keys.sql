-- +goose Up
-- The original host check constraint in migration 34 compared host::inet::text = host,
-- but PostgreSQL inet::text appends /32 for single addresses. Replace it with
-- host(host::inet) = host so valid IPv4 addresses satisfy the check.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'public.server_agent_ssh_host_keys'::regclass
          AND conname = 'server_agent_ssh_host_keys_host_check'
    ) THEN
        ALTER TABLE public.server_agent_ssh_host_keys
            DROP CONSTRAINT server_agent_ssh_host_keys_host_check;
    END IF;

    ALTER TABLE public.server_agent_ssh_host_keys
        ADD CONSTRAINT server_agent_ssh_host_keys_host_check
        CHECK (family(host::inet) = 4 AND host(host::inet) = host);
END $$;
-- +goose StatementEnd

-- +goose Down
-- This migration is forward-only.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'migration 35 is forward-only; SSH host-key trust cannot be safely downgraded';
END $$;
-- +goose StatementEnd
