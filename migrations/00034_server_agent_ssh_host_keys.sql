-- +goose Up
-- Keep a host key per managed Frigate server, SSH target IPv4 and port. This is
-- intentionally distinct from server_agent_tls, which anchors agent HTTPS only.
-- +goose StatementBegin
DO $$
DECLARE
    parent_constraint text;
BEGIN
    IF to_regclass('public.frigate_servers') IS NULL THEN
        RAISE EXCEPTION 'cannot add SSH host-key trust: frigate_servers is missing';
    END IF;

    SELECT pg_get_constraintdef(oid) INTO parent_constraint
      FROM pg_catalog.pg_constraint
     WHERE conrelid = 'public.frigate_servers'::regclass
       AND conname = 'frigate_servers_id_tenant_unique';
    IF parent_constraint IS NULL THEN
        ALTER TABLE public.frigate_servers
            ADD CONSTRAINT frigate_servers_id_tenant_unique UNIQUE (id, tenant_id);
    ELSIF lower(regexp_replace(parent_constraint, '[[:space:]]+', '', 'g'))
          <> 'unique(id,tenant_id)' THEN
        RAISE EXCEPTION 'incompatible frigate_servers tenant binding';
    END IF;
END $$;
-- +goose StatementEnd

CREATE TABLE public.server_agent_ssh_host_keys (
    server_id uuid NOT NULL,
    tenant_id uuid NOT NULL REFERENCES public.tenants (id) ON DELETE CASCADE,
    host text NOT NULL CHECK (family(host::inet) = 4 AND host::inet::text = host),
    ssh_port integer NOT NULL CHECK (ssh_port BETWEEN 1 AND 65535),
    fingerprint text NOT NULL CHECK (fingerprint ~ '^SHA256:[A-Za-z0-9+/]{43}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT server_agent_ssh_host_keys_pkey PRIMARY KEY (server_id, host, ssh_port),
    CONSTRAINT server_agent_ssh_host_keys_server_tenant_fk
        FOREIGN KEY (server_id, tenant_id)
        REFERENCES public.frigate_servers (id, tenant_id) ON DELETE CASCADE
);

ALTER TABLE public.server_agent_ssh_host_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.server_agent_ssh_host_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON public.server_agent_ssh_host_keys
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
-- This migration is intentionally forward-only. Forced RLS can hide rows from
-- the migration role, so a conditional emptiness check cannot safely authorize
-- deleting remembered host keys.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'migration 34 is forward-only; SSH host-key trust cannot be safely downgraded';
END $$;
-- +goose StatementEnd
