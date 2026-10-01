//go:build integration

package bootstrap_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func TestSyncPlatformAdminGrantsOnly(t *testing.T) {
	ctx := context.Background()
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	var userID db.User
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		userID, err = q.CreateUser(ctx, db.CreateUserParams{Username: "existing-admin", DisplayName: "Existing Admin"})
		if err != nil {
			return err
		}
		_, err = q.CreateGrant(ctx, db.CreateGrantParams{
			SubjectType: "user", SubjectID: userID.ID, Permission: string(authz.Catalog[0].Permission),
			Effect: string(authz.Deny), ScopeType: string(authz.ScopePlatform), CreatedBy: &userID.ID,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	added, err := bootstrap.SyncPlatformAdmin(ctx, st, "existing-admin")
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	var grants, tokens int
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		gs, err := q.ListGrantsForUser(ctx, userID.ID)
		if err != nil {
			return err
		}
		grants = len(gs)
		return pool.QueryRow(ctx, "SELECT count(*) FROM api_tokens WHERE user_id = $1", userID.ID).Scan(&tokens)
	}); err != nil {
		t.Fatal(err)
	}
	if added != len(authz.Catalog) {
		t.Fatalf("first sync added %d grants, want %d", added, len(authz.Catalog))
	}
	if grants != len(authz.Catalog)+1 {
		t.Fatalf("grants = %d, want catalog ALLOWs plus preserved DENY", grants)
	}
	var preservedDeny bool
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		gs, err := q.ListGrantsForUser(ctx, userID.ID)
		for _, grant := range gs {
			if grant.Permission == string(authz.Catalog[0].Permission) && grant.ScopeType == string(authz.ScopePlatform) && grant.Effect == string(authz.Deny) {
				preservedDeny = true
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !preservedDeny {
		t.Fatal("existing platform DENY was not preserved")
	}
	if tokens != 0 {
		t.Fatalf("sync created %d API tokens", tokens)
	}
	added, err = bootstrap.SyncPlatformAdmin(ctx, st, "existing-admin")
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if added != 0 {
		t.Fatalf("second sync added %d grants, want 0", added)
	}
	var after int
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		gs, err := q.ListGrantsForUser(ctx, userID.ID)
		after = len(gs)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if after != grants {
		t.Fatalf("second sync changed grant count: %d -> %d", grants, after)
	}
}

func TestSyncPlatformAdminRejectsMissingOrTenantUser(t *testing.T) {
	ctx := context.Background()
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	if _, err := bootstrap.SyncPlatformAdmin(ctx, st, "missing-admin"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing user error = %v, want not found", err)
	}
	var tenantUser db.User
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		tenant, err := q.CreateTenant(ctx, db.CreateTenantParams{Slug: "tenant", Name: "Tenant"})
		if err != nil {
			return err
		}
		tenantUser, err = q.CreateUser(ctx, db.CreateUserParams{TenantID: &tenant.ID, Username: "tenant-admin", DisplayName: "Tenant Admin"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.SyncPlatformAdmin(ctx, st, "tenant-admin"); err == nil || !strings.Contains(err.Error(), "belongs to a tenant") {
		t.Fatalf("tenant user error = %v, want rejection", err)
	}
	var count int
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		gs, err := q.ListGrantsForUser(ctx, tenantUser.ID)
		count = len(gs)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("tenant user received %d grants", count)
	}
}
