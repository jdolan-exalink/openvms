//go:build integration

package rules_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/migrations"
)

// migrationSection returns the SQL between two markers of an embedded migration. The harness has
// already applied every migration to an empty database, so the tests re-run the seeding statements
// against grants they insert themselves; the seeding is idempotent by design.
func migrationSection(t *testing.T, file, from, to string) string {
	t.Helper()
	b, err := migrations.FS.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, from)
	if i < 0 {
		t.Fatalf("%s: marker %q not found", file, from)
	}
	s = s[i:]
	if to != "" {
		j := strings.Index(s, to)
		if j < 0 {
			t.Fatalf("%s: marker %q not found", file, to)
		}
		s = s[:j]
	}
	return s
}

func (f *fixture) seedRuleGrants() {
	f.t.Helper()
	sql := migrationSection(f.t, "00050_rules_versioning.sql", "-- Dedicated permissions", "-- +goose Down") +
		migrationSection(f.t, "00051_rules_alarm_deny_mirror.sql", "-- +goose Up", "-- +goose Down")
	if err := f.exec(sql); err != nil {
		f.t.Fatal(err)
	}
}

func TestRuleGrantSeedingMirrorsAlarmGrantsSemantically(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tenantID := f.env.Demo.TenantID

	newUser := func(tx pgx.Tx, name string) uuid.UUID {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO users (tenant_id, username, display_name, status)
VALUES ($1, $2, $2, 'active') RETURNING id`, tenantID, name+"-"+uuid.NewString()[:8]).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	grant := func(tx pgx.Tx, subjectType string, subject uuid.UUID, perm, effect, scopeType string, scopeID *uuid.UUID) {
		if _, err := tx.Exec(ctx, `INSERT INTO permission_grants
(tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)`, tenantID, subjectType, subject, perm, effect, scopeType, scopeID); err != nil {
			t.Fatal(err)
		}
	}
	group := func(tx pgx.Tx, member uuid.UUID) uuid.UUID {
		var gid uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO user_groups (tenant_id, name) VALUES ($1, $2) RETURNING id`,
			tenantID, "rol-"+uuid.NewString()[:8]).Scan(&gid); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_group_members (group_id, user_id) VALUES ($1, $2)`, gid, member); err != nil {
			t.Fatal(err)
		}
		return gid
	}

	type want struct{ manage, read bool }
	users := map[string]uuid.UUID{}
	err := f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		cam := f.env.Cameras["frigate-c01/muelle"].ID
		for _, n := range []string{"roleAllowDirectDeny", "viewRoleAllowDirectDeny", "manageAllowOnly", "viewAllowOnly",
			"cameraScopedOnly", "manageAllowViewDeny"} {
			users[n] = newUser(tx, n)
		}
		// Role-level alarms.manage allow + direct alarms.manage deny.
		grant(tx, "group", group(tx, users["roleAllowDirectDeny"]), "alarms.manage", "allow", "tenant", &tenantID)
		grant(tx, "user", users["roleAllowDirectDeny"], "alarms.manage", "deny", "tenant", &tenantID)
		// Role-level alarms.view allow + direct alarms.view deny.
		grant(tx, "group", group(tx, users["viewRoleAllowDirectDeny"]), "alarms.view", "allow", "tenant", &tenantID)
		grant(tx, "user", users["viewRoleAllowDirectDeny"], "alarms.view", "deny", "tenant", &tenantID)
		grant(tx, "user", users["manageAllowOnly"], "alarms.manage", "allow", "tenant", &tenantID)
		grant(tx, "user", users["viewAllowOnly"], "alarms.view", "allow", "tenant", &tenantID)
		// Camera-scoped alarm grants have no rules equivalent and must not be carried over.
		grant(tx, "user", users["cameraScopedOnly"], "alarms.manage", "allow", "camera", &cam)
		// A view deny must not take away rules.manage.
		grant(tx, "user", users["manageAllowViewDeny"], "alarms.manage", "allow", "tenant", &tenantID)
		grant(tx, "user", users["manageAllowViewDeny"], "alarms.view", "deny", "tenant", &tenantID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	f.seedRuleGrants()
	f.seedRuleGrants() // idempotent

	expect := map[string]want{
		"roleAllowDirectDeny":     {manage: false, read: false},
		"viewRoleAllowDirectDeny": {manage: false, read: false},
		"manageAllowOnly":         {manage: true, read: true},
		"viewAllowOnly":           {manage: false, read: true},
		"cameraScopedOnly":        {manage: false, read: false},
		"manageAllowViewDeny":     {manage: true, read: false},
	}
	for name, w := range expect {
		actor := authz.Actor{UserID: users[name], Username: name, TenantID: &tenantID}
		var gotManage, gotRead bool
		err := f.env.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
			chk, err := access.Load(ctx, q, actor)
			if err != nil {
				return err
			}
			gotManage = chk.Can(authz.RulesManage, access.Tenant(tenantID))
			gotRead = chk.Can(authz.RulesRead, access.Tenant(tenantID))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if gotManage != w.manage || gotRead != w.read {
			t.Errorf("%s: rules.manage=%v rules.read=%v, want %v/%v", name, gotManage, gotRead, w.manage, w.read)
		}
	}
}
