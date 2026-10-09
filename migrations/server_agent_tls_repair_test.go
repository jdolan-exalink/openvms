package migrations

import (
	"strings"
	"testing"
)

func TestServerAgentTLSRepairIsForwardOnlyAndValidatesExistingSchema(t *testing.T) {
	sql, err := FS.ReadFile("00033_repair_server_agent_tls.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalized := normalizeSQL(string(sql))
	for _, required := range []string{
		"-- +goose up",
		"pg_catalog.pg_constraint",
		"pg_catalog.pg_attribute",
		"pg_catalog.pg_policy",
		"server_agents_server_tenant_unique",
		"server_agent_tls_server_tenant_fk",
		"server_agent_tls_tenant_id_fkey",
		"enable row level security",
		"force row level security",
		"tenant_isolation",
		"policy_count > 1",
		"app_tenant_visible(tenant_id)",
	} {
		if !strings.Contains(normalized, required) {
			t.Errorf("repair migration must validate/apply %q", required)
		}
	}
	if !strings.Contains(normalized, "-- +goose down") || !strings.Contains(normalized, "raise exception") || strings.Contains(normalized, "drop table") {
		t.Fatal("Down must refuse rollback rather than delete v32-owned TLS data")
	}
	for _, unsafe := range []string{"create table if not exists", "add constraint if not exists", "create policy if not exists"} {
		if strings.Contains(normalized, unsafe) {
			t.Fatalf("repair migration must not mask incompatible objects with %q", unsafe)
		}
	}
}

func TestServerAgentTLSRepairCreatesMissingSchemaAndRejectsIncompatibleObjects(t *testing.T) {
	sql, err := FS.ReadFile("00033_repair_server_agent_tls.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalized := normalizeSQL(string(sql))
	for _, required := range []string{
		"create table public.server_agent_tls",
		"add constraint server_agents_server_tenant_unique unique (server_id, tenant_id)",
		"raise exception",
		"server_agent_tls_secure_port_check",
		"server_agent_tls_trust_mode_check",
		"server_agent_tls_check",
		"incompatible server_agent_tls constraints",
		"server_agent_tls_server_tenant_fk",
	} {
		if !strings.Contains(normalized, required) {
			t.Errorf("repair migration must create or reject according to %q", required)
		}
	}
}

func TestServerAgentTLSRepairValidatesConstraintSemanticsAndIndexes(t *testing.T) {
	sql, err := FS.ReadFile("00033_repair_server_agent_tls.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalized := normalizeSQL(string(sql))
	for _, required := range []string{
		"c.convalidated",
		"pg_catalog.pg_index",
		"i.indisunique",
		"i.indisvalid",
		"i.indisready",
		"table_index_count <> 1",
		"= 'foreignkey(server_id,tenant_id)referencesserver_agents(server_id,tenant_id)ondeletecascade'",
		"= 'foreignkey(tenant_id)referencestenants(id)ondeletecascade'",
	} {
		if !strings.Contains(normalized, required) {
			t.Errorf("repair must validate exact constraint/index property %q", required)
		}
	}
	if strings.Contains(normalized, "like 'foreignkey(server_id") || strings.Contains(normalized, "like 'foreignkey(tenant_id)") {
		t.Fatal("foreign key definitions must be exact, not permissive substring matches")
	}
}
