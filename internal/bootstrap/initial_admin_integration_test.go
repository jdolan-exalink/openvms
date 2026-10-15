//go:build integration

package bootstrap_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func captureLog() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func adminFixture(t *testing.T) *store.Store {
	t.Helper()
	pool, _ := pgtest.Migrated(t)
	return &store.Store{Pool: pool}
}

func listUsers(t *testing.T, st *store.Store) []db.User {
	t.Helper()
	var users []db.User
	if err := st.Tx(context.Background(), store.AllTenants, func(q *db.Queries) error {
		var err error
		users, err = q.ListUsers(context.Background(), db.ListUsersParams{IncludePlatform: true})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return users
}

func TestEnsureInitialAdminCreatesDefaultAdmin(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, buf := captureLog()
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", bootstrap.DefaultAdminPassword, log); err != nil {
		t.Fatal(err)
	}
	users := listUsers(t, st)
	if len(users) != 1 || users[0].Username != "admin" || users[0].TenantID != nil {
		t.Fatalf("users = %+v, want one platform admin", users)
	}
	if !users[0].MustChangePassword {
		t.Fatal("default password must force a change")
	}
	var grants []db.PermissionGrant
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		grants, err = q.ListGrantsForUser(ctx, users[0].ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(grants) != len(authz.Catalog) {
		t.Fatalf("grants = %d, want %d", len(grants), len(authz.Catalog))
	}
	svc := &identity.Service{Store: st, Log: pgtest.Discard(), SessionTTL: time.Hour, IdleTimeout: time.Hour, MaxFailures: 5, LockFor: time.Minute}
	res, err := svc.Login(ctx, identity.LoginInput{Username: "admin", Password: bootstrap.DefaultAdminPassword})
	if err != nil || res.Token == "" {
		t.Fatalf("login with default password: %v", err)
	}
	if !strings.Contains(buf.String(), "level=WARN") || strings.Contains(buf.String(), bootstrap.DefaultAdminPassword) {
		t.Fatalf("want a WARN without the password, got %q", buf.String())
	}
}

func TestEnsureInitialAdminCustomPassword(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, buf := captureLog()
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "Una-Clave-Larga-2026", log); err != nil {
		t.Fatal(err)
	}
	users := listUsers(t, st)
	if len(users) != 1 || users[0].MustChangePassword {
		t.Fatalf("users = %+v, want one admin without forced change", users)
	}
	if !identity.VerifyPassword(users[0].PasswordHash, "Una-Clave-Larga-2026") {
		t.Fatal("password was not stored")
	}
	if strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("unexpected WARN: %q", buf.String())
	}
}

func TestEnsureInitialAdminNeverResets(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, _ := captureLog()
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", bootstrap.DefaultAdminPassword, log); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.SetPassword(ctx, st, "admin", "Otra-Clave-Larga-2026"); err != nil {
		t.Fatal(err)
	}
	log2, buf := captureLog()
	for i := 0; i < 2; i++ {
		if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", bootstrap.DefaultAdminPassword, log2); err != nil {
			t.Fatal(err)
		}
	}
	users := listUsers(t, st)
	if len(users) != 1 || !identity.VerifyPassword(users[0].PasswordHash, "Otra-Clave-Larga-2026") {
		t.Fatalf("existing admin was modified: %+v", users)
	}
	if strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("WARN after password change: %q", buf.String())
	}
}

func TestEnsureInitialAdminSkipsWhenAnotherPlatformUserHasPassword(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, _ := captureLog()
	if _, _, err := bootstrap.PlatformAdmin(ctx, st, "root"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.SetPassword(ctx, st, "root", "Clave-Larga-2026"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", bootstrap.DefaultAdminPassword, log); err != nil {
		t.Fatal(err)
	}
	if users := listUsers(t, st); len(users) != 1 || users[0].Username != "root" {
		t.Fatalf("users = %+v, want only root", users)
	}
}

func TestEnsureInitialAdminWeakPasswordSkipped(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, buf := captureLog()
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "corta", log); err != nil {
		t.Fatalf("weak password must not fail startup: %v", err)
	}
	if users := listUsers(t, st); len(users) != 0 {
		t.Fatalf("users = %+v, want none", users)
	}
	if !strings.Contains(buf.String(), "level=ERROR") || strings.Contains(buf.String(), "corta") {
		t.Fatalf("want an ERROR without the password, got %q", buf.String())
	}
}

func TestEnsureInitialAdminConcurrent(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, _ := captureLog()
	errs := make(chan error, 4)
	for i := 0; i < cap(errs); i++ {
		go func() { errs <- bootstrap.EnsureInitialAdmin(ctx, st, "admin", bootstrap.DefaultAdminPassword, log) }()
	}
	for i := 0; i < cap(errs); i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if users := listUsers(t, st); len(users) != 1 {
		t.Fatalf("users = %d, want 1", len(users))
	}
}

func TestEnsureInitialAdminWarnsEvenWhenAnotherUserSortsFirst(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, _ := captureLog()
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", bootstrap.DefaultAdminPassword, log); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bootstrap.PlatformAdmin(ctx, st, "aaron"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.SetPassword(ctx, st, "aaron", "Clave-Larga-2026"); err != nil {
		t.Fatal(err)
	}
	log2, buf := captureLog()
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", bootstrap.DefaultAdminPassword, log2); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("want WARN, got %q", buf.String())
	}
}
