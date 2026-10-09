package migrations

import (
	"strings"
	"testing"
)

func TestServerAgentTLSUsesMatchingAgentTenant(t *testing.T) {
	parent, err := FS.ReadFile("00031_server_agents.sql")
	if err != nil {
		t.Fatal(err)
	}
	child, err := FS.ReadFile("00032_server_agent_tls.sql")
	if err != nil {
		t.Fatal(err)
	}
	parentSQL := normalizeSQL(string(parent))
	if !strings.Contains(parentSQL, "server_id uuid primary key") || !strings.Contains(parentSQL, "tenant_id uuid not null") {
		t.Fatal("server_agents key columns must be non-null UUIDs")
	}
	childSQL := normalizeSQL(string(child))
	if !strings.Contains(childSQL, "alter table server_agents add constraint server_agents_server_tenant_unique unique (server_id, tenant_id)") {
		t.Fatal("server_agents must declare the composite key referenced by server_agent_tls")
	}
	if !strings.Contains(childSQL, "foreign key (server_id, tenant_id) references server_agents (server_id, tenant_id)") {
		t.Fatal("server_agent_tls must bind its tenant_id to the referenced server_agents tenant")
	}
	down := strings.SplitN(childSQL, "-- +goose down", 2)
	if len(down) != 2 {
		t.Fatal("server_agent_tls migration must have a Down section")
	}
	dropChild := strings.Index(down[1], "drop table if exists server_agent_tls")
	dropParentConstraint := strings.Index(down[1], "alter table server_agents drop constraint if exists server_agents_server_tenant_unique")
	if dropChild < 0 || dropParentConstraint < 0 || dropChild >= dropParentConstraint {
		t.Fatal("Down must drop server_agent_tls before removing its parent composite constraint")
	}
}

func normalizeSQL(sql string) string {
	return strings.Join(strings.Fields(strings.ToLower(sql)), " ")
}
