-- +goose Up
-- Version 32 was recorded by older deployments before its TLS DDL shipped.
-- Repair the missing objects without accepting a same-named incompatible schema.
-- +goose StatementBegin
DO $$
DECLARE
    parent_constraint text;
    table_columns integer;
    parent_index_count integer;
    table_index_count integer;
    table_constraint_count integer;
    all_constraint_count integer;
    policy_count integer;
    policy_using text;
    policy_check text;
    policy_roles oid[];
    policy_command "char";
BEGIN
    IF to_regclass('public.server_agents') IS NULL THEN
        RAISE EXCEPTION 'cannot repair server_agent_tls: server_agents is missing';
    END IF;

    SELECT pg_get_constraintdef(oid) INTO parent_constraint
      FROM pg_catalog.pg_constraint
     WHERE conrelid = 'public.server_agents'::regclass
       AND conname = 'server_agents_server_tenant_unique';
    IF parent_constraint IS NULL THEN
        ALTER TABLE public.server_agents
            ADD CONSTRAINT server_agents_server_tenant_unique UNIQUE (server_id, tenant_id);
    ELSIF lower(regexp_replace(parent_constraint, '[[:space:]]+', '', 'g'))
          <> 'unique(server_id,tenant_id)' THEN
        RAISE EXCEPTION 'incompatible server_agents composite constraint';
    END IF;
    SELECT count(*) INTO parent_index_count
      FROM pg_catalog.pg_constraint c
      JOIN pg_catalog.pg_index i ON i.indexrelid = c.conindid
     WHERE c.conrelid = 'public.server_agents'::regclass
       AND c.conname = 'server_agents_server_tenant_unique'
       AND c.contype = 'u' AND i.indisunique AND i.indisvalid AND i.indisready;
    IF parent_index_count <> 1 THEN
        RAISE EXCEPTION 'invalid server_agents composite constraint index';
    END IF;

    IF to_regclass('public.server_agent_tls') IS NULL THEN
        CREATE TABLE public.server_agent_tls (
            server_id uuid PRIMARY KEY,
            tenant_id uuid NOT NULL REFERENCES public.tenants (id) ON DELETE CASCADE,
            secure_port integer NOT NULL CONSTRAINT server_agent_tls_secure_port_check
                CHECK (secure_port BETWEEN 1 AND 65535),
            trust_mode text NOT NULL CONSTRAINT server_agent_tls_trust_mode_check
                CHECK (trust_mode IN ('system', 'custom')),
            ca_pem text,
            CONSTRAINT server_agent_tls_check CHECK (
                (trust_mode = 'system' AND ca_pem IS NULL) OR
                (trust_mode = 'custom' AND ca_pem IS NOT NULL AND length(ca_pem) > 0)
            ),
            CONSTRAINT server_agent_tls_server_tenant_fk
                FOREIGN KEY (server_id, tenant_id)
                REFERENCES public.server_agents (server_id, tenant_id) ON DELETE CASCADE
        );
    ELSE
        SELECT count(*) INTO table_columns
          FROM pg_catalog.pg_attribute a
         WHERE a.attrelid = 'public.server_agent_tls'::regclass
           AND a.attnum > 0 AND NOT a.attisdropped;
        IF table_columns <> 5 OR NOT EXISTS (
            SELECT 1 FROM pg_catalog.pg_attribute
             WHERE attrelid = 'public.server_agent_tls'::regclass AND attname = 'server_id'
               AND atttypid = 'uuid'::regtype AND attnotnull AND attnum > 0 AND NOT attisdropped
        ) OR NOT EXISTS (
            SELECT 1 FROM pg_catalog.pg_attribute
             WHERE attrelid = 'public.server_agent_tls'::regclass AND attname = 'tenant_id'
               AND atttypid = 'uuid'::regtype AND attnotnull AND attnum > 0 AND NOT attisdropped
        ) OR NOT EXISTS (
            SELECT 1 FROM pg_catalog.pg_attribute
             WHERE attrelid = 'public.server_agent_tls'::regclass AND attname = 'secure_port'
               AND atttypid = 'integer'::regtype AND attnotnull AND attnum > 0 AND NOT attisdropped
        ) OR NOT EXISTS (
            SELECT 1 FROM pg_catalog.pg_attribute
             WHERE attrelid = 'public.server_agent_tls'::regclass AND attname = 'trust_mode'
               AND atttypid = 'text'::regtype AND attnotnull AND attnum > 0 AND NOT attisdropped
        ) OR NOT EXISTS (
            SELECT 1 FROM pg_catalog.pg_attribute
             WHERE attrelid = 'public.server_agent_tls'::regclass AND attname = 'ca_pem'
               AND atttypid = 'text'::regtype AND NOT attnotnull AND attnum > 0 AND NOT attisdropped
        ) THEN
            RAISE EXCEPTION 'incompatible server_agent_tls columns';
        END IF;

        SELECT count(*) INTO all_constraint_count
          FROM pg_catalog.pg_constraint c
         WHERE c.conrelid = 'public.server_agent_tls'::regclass;
        SELECT count(*) INTO table_constraint_count
          FROM pg_catalog.pg_constraint c
         WHERE c.conrelid = 'public.server_agent_tls'::regclass
           AND ((c.conname = 'server_agent_tls_pkey' AND c.contype = 'p' AND c.convalidated
                 AND lower(regexp_replace(pg_get_constraintdef(c.oid), '[[:space:]]+', '', 'g')) = 'primarykey(server_id)')
             OR (c.conname = 'server_agent_tls_server_tenant_fk' AND c.contype = 'f' AND c.convalidated
                 AND lower(regexp_replace(pg_get_constraintdef(c.oid), '[[:space:]]+', '', 'g')) = 'foreignkey(server_id,tenant_id)referencesserver_agents(server_id,tenant_id)ondeletecascade')
             OR (c.conname = 'server_agent_tls_tenant_id_fkey' AND c.contype = 'f' AND c.convalidated
                 AND lower(regexp_replace(pg_get_constraintdef(c.oid), '[[:space:]]+', '', 'g')) = 'foreignkey(tenant_id)referencestenants(id)ondeletecascade')
             OR (c.conname = 'server_agent_tls_secure_port_check' AND c.contype = 'c' AND c.convalidated
                 AND lower(regexp_replace(replace(pg_get_constraintdef(c.oid), '::integer', ''), '[[:space:]()]', '', 'g')) = 'checksecure_port>=1andsecure_port<=65535')
             OR (c.conname = 'server_agent_tls_trust_mode_check' AND c.contype = 'c' AND c.convalidated
                 AND lower(regexp_replace(replace(pg_get_constraintdef(c.oid), '::text', ''), '[[:space:]()]', '', 'g')) = 'checktrust_mode=anyarray[''system'',''custom'']')
             OR (c.conname = 'server_agent_tls_check' AND c.contype = 'c' AND c.convalidated
                 AND lower(regexp_replace(replace(pg_get_constraintdef(c.oid), '::text', ''), '[[:space:]()]', '', 'g')) = 'checktrust_mode=''system''andca_pemisnullortrust_mode=''custom''andca_pemisnotnullandlengthca_pem>0'));
        SELECT count(*) INTO table_index_count
          FROM pg_catalog.pg_constraint c
          JOIN pg_catalog.pg_index i ON i.indexrelid = c.conindid
         WHERE c.conrelid = 'public.server_agent_tls'::regclass
           AND c.conname = 'server_agent_tls_pkey' AND c.contype = 'p'
           AND i.indisunique AND i.indisvalid AND i.indisready;
        IF table_constraint_count <> 6 OR all_constraint_count <> 6 OR table_index_count <> 1 THEN
            RAISE EXCEPTION 'incompatible server_agent_tls constraints';
        END IF;
    END IF;

    -- Both fresh installs (where v32 already created these) and repaired installs
    -- receive the same policies. Existing incompatible definitions fail closed.
    IF NOT (SELECT relrowsecurity AND relforcerowsecurity FROM pg_catalog.pg_class
             WHERE oid = 'public.server_agent_tls'::regclass) THEN
        ALTER TABLE public.server_agent_tls ENABLE ROW LEVEL SECURITY;
        ALTER TABLE public.server_agent_tls FORCE ROW LEVEL SECURITY;
    END IF;

    SELECT count(*) INTO policy_count FROM pg_catalog.pg_policy
     WHERE polrelid = 'public.server_agent_tls'::regclass;
    IF policy_count > 1 THEN
        RAISE EXCEPTION 'incompatible server_agent_tls policy count';
    END IF;

    IF policy_count = 0 THEN
        CREATE POLICY tenant_isolation ON public.server_agent_tls
            USING (app_tenant_visible(tenant_id))
            WITH CHECK (app_tenant_visible(tenant_id));
    ELSE
        SELECT pg_get_expr(pol.polqual, pol.polrelid), pg_get_expr(pol.polwithcheck, pol.polrelid), pol.polroles, pol.polcmd
          INTO policy_using, policy_check, policy_roles, policy_command
          FROM pg_catalog.pg_policy pol
         WHERE pol.polrelid = 'public.server_agent_tls'::regclass
           AND pol.polname = 'tenant_isolation';
        IF NOT FOUND OR policy_using <> 'app_tenant_visible(tenant_id)'
           OR policy_check <> 'app_tenant_visible(tenant_id)'
           OR policy_roles <> ARRAY[0::oid] OR policy_command <> '*' THEN
            RAISE EXCEPTION 'incompatible server_agent_tls tenant policy';
        END IF;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- The repair may have adopted an existing v32 schema. Deleting it would destroy
-- operator trust configuration; rollback is intentionally refused.
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'migration 33 is forward-only; do not drop server_agent_tls data';
END $$;
-- +goose StatementEnd
