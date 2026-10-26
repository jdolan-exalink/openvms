//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
	"github.com/jdolan-exalink/openvms/migrations"
)

func TestCameraConfigPermissionsMigrationMirrorsLegacyGrantsOnly(t *testing.T) {
	url := pgtest.Start(t)
	ctx := context.Background()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 76); err != nil {
		t.Fatal(err)
	}
	var tenantA, tenantB string
	if err := db.QueryRowContext(ctx, `INSERT INTO tenants (slug, name) VALUES ('a', 'a') RETURNING id`).Scan(&tenantA); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO tenants (slug, name) VALUES ('b', 'b') RETURNING id`).Scan(&tenantB); err != nil {
		t.Fatal(err)
	}
	const (
		uView   = "00000000-0000-0000-0000-000000000001"
		uManage = "00000000-0000-0000-0000-000000000002"
		uConfig = "00000000-0000-0000-0000-000000000003"
		uSecret = "00000000-0000-0000-0000-000000000004"
		uPair   = "00000000-0000-0000-0000-000000000005"
		uMixed  = "00000000-0000-0000-0000-000000000006"
		uDeny   = "00000000-0000-0000-0000-000000000007"
		uOther  = "00000000-0000-0000-0000-000000000008"
	)
	grant := func(tenant any, subject, perm, effect, scope string) {
		t.Helper()
		var scopeID any
		if scope != "platform" {
			scopeID = "00000000-0000-0000-0000-0000000000aa"
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id)
VALUES ($1, 'user', $2, $3, $4, $5, $6)`, tenant, subject, perm, effect, scope, scopeID); err != nil {
			t.Fatal(err)
		}
	}
	grant(tenantA, uView, "cameras.view", "allow", "tenant")
	grant(tenantA, uManage, "cameras.manage", "allow", "tenant")
	grant(tenantA, uConfig, "servers.config", "allow", "server")
	grant(tenantA, uSecret, "servers.config.secrets", "allow", "server") // half a pair: no web, no yaml
	grant(tenantA, uPair, "servers.manage", "allow", "server")
	grant(tenantA, uPair, "servers.config.secrets", "allow", "server")
	grant(tenantA, uMixed, "servers.manage", "allow", "tenant") // pair at different scopes: runtime pair rule covers it
	grant(tenantA, uMixed, "servers.config.secrets", "allow", "server")
	grant(tenantA, uDeny, "servers.config", "deny", "server")
	grant(tenantA, uDeny, "servers.config.secrets", "deny", "server")
	grant(tenantB, uOther, "cameras.view", "allow", "tenant")
	grant(nil, uOther, "servers.restart", "allow", "platform")

	snapshot := func() map[string]bool {
		rows, err := db.QueryContext(ctx, `SELECT subject_id::text, permission, effect, scope_type, coalesce(tenant_id::text, '') FROM permission_grants`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]bool{}
		for rows.Next() {
			var s, p, e, sc, tn string
			if err := rows.Scan(&s, &p, &e, &sc, &tn); err != nil {
				t.Fatal(err)
			}
			out[strings.Join([]string{s[len(s)-1:], p, e, sc, tn}, "|")] = true
		}
		return out
	}
	before := snapshot()

	if _, err := provider.UpTo(ctx, 78); err != nil {
		t.Fatal(err)
	}
	after := snapshot()
	for k := range before {
		if !after[k] {
			t.Errorf("legacy grant changed or removed: %s", k)
		}
	}
	key := func(subject, perm, effect, scope, tenant string) string {
		return strings.Join([]string{subject, perm, effect, scope, tenant}, "|")
	}
	want := []string{
		key("1", "cameras.config.view", "allow", "tenant", tenantA),
		key("2", "cameras.config.ptz", "allow", "tenant", tenantA),
		key("3", "cameras.config.edit", "allow", "server", tenantA),
		key("3", "cameras.config.video", "allow", "server", tenantA),
		key("3", "cameras.config.recording", "allow", "server", tenantA),
		key("3", "cameras.config.analytics", "allow", "server", tenantA),
		key("3", "cameras.config.advanced", "allow", "server", tenantA),
		key("5", "cameras.web.access", "allow", "server", tenantA),
		key("7", "cameras.config.edit", "deny", "server", tenantA),
		key("7", "cameras.config.video", "deny", "server", tenantA),
		key("7", "cameras.config.recording", "deny", "server", tenantA),
		key("7", "cameras.config.analytics", "deny", "server", tenantA),
		key("7", "cameras.config.advanced", "deny", "server", tenantA),
		key("7", "cameras.web.access", "deny", "server", tenantA),
		key("7", "cameras.config.yaml", "deny", "server", tenantA),
		key("8", "cameras.config.view", "allow", "tenant", tenantB),
		key("8", "cameras.restart", "allow", "platform", ""),
	}
	var added, wantSorted []string
	for k := range after {
		if !before[k] {
			added = append(added, k)
		}
	}
	wantSorted = append(wantSorted, want...)
	sort.Strings(added)
	sort.Strings(wantSorted)
	if strings.Join(added, "\n") != strings.Join(wantSorted, "\n") {
		t.Fatalf("mirrored grants differ\n got: %v\nwant: %v", added, wantSorted)
	}

	// Down removes only the mirrored grants and a second Up reproduces the same set.
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(); len(got) != len(before) {
		t.Fatalf("down left %d grants, want %d", len(got), len(before))
	}
	for k := range before {
		if !snapshot()[k] {
			t.Errorf("down removed legacy grant %s", k)
		}
	}
	if _, err := provider.UpTo(ctx, 78); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(); len(got) != len(after) {
		t.Fatalf("second up produced %d grants, want %d", len(got), len(after))
	}
}
