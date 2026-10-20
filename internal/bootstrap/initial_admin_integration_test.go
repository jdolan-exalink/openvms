//go:build integration

package bootstrap_test

import (
	"bytes"
	"context"
	"log/slog"
	"regexp"
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

var loggedPassword = regexp.MustCompile(`password=(\S+)`)

func randomAdmin(t *testing.T) (st *store.Store, password string, buf *bytes.Buffer) {
	t.Helper()
	st = adminFixture(t)
	log, buf := captureLog()
	if err := bootstrap.EnsureInitialAdmin(context.Background(), st, "admin", "", log); err != nil {
		t.Fatal(err)
	}
	m := loggedPassword.FindAllStringSubmatch(buf.String(), -1)
	if len(m) != 1 {
		t.Fatalf("want the password logged exactly once, got %q", buf.String())
	}
	return st, m[0][1], buf
}

func TestEnsureInitialAdminGeneratesRandomPassword(t *testing.T) {
	ctx := context.Background()
	st, password, buf := randomAdmin(t)
	if !strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("want a WARN, got %q", buf.String())
	}
	if err := identity.CheckPolicy(password); err != nil {
		t.Fatalf("generated password fails the policy: %v", err)
	}
	users := listUsers(t, st)
	if len(users) != 1 || users[0].Username != "admin" || users[0].TenantID != nil || !users[0].MustChangePassword {
		t.Fatalf("users = %+v, want one platform admin forced to change", users)
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
	if res, err := svc.Login(ctx, identity.LoginInput{Username: "admin", Password: password}); err != nil || res.Token == "" {
		t.Fatalf("login with the logged password: %v", err)
	}
	// A second start creates nothing and never logs a password again.
	log2, buf2 := captureLog()
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "", log2); err != nil {
		t.Fatal(err)
	}
	if len(listUsers(t, st)) != 1 || strings.Contains(buf2.String(), password) || strings.Contains(buf2.String(), "password=") {
		t.Fatalf("second start: %q", buf2.String())
	}
}

func TestEnsureInitialAdminPasswordsDiffer(t *testing.T) {
	_, a, _ := randomAdmin(t)
	_, b, _ := randomAdmin(t)
	if a == b {
		t.Fatal("two installs got the same generated password")
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
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "Clave-Inicial-2026", log); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.SetPassword(ctx, st, "admin", "Otra-Clave-Larga-2026"); err != nil {
		t.Fatal(err)
	}
	log2, buf := captureLog()
	for i := 0; i < 2; i++ {
		if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "Clave-Inicial-2026", log2); err != nil {
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
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "Clave-Inicial-2026", log); err != nil {
		t.Fatal(err)
	}
	if users := listUsers(t, st); len(users) != 1 || users[0].Username != "root" {
		t.Fatalf("users = %+v, want only root", users)
	}
}

func TestEnsureInitialAdminShortPasswordFallsBackToGenerated(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, buf := captureLog()
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "corta", log); err != nil {
		t.Fatalf("short password must not fail startup: %v", err)
	}
	users := listUsers(t, st)
	if len(users) != 1 || users[0].Username != "admin" || !users[0].MustChangePassword {
		t.Fatalf("users = %+v, want one admin forced to change its password", users)
	}
	out := buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "OPENVMS_ADMIN_PASSWORD rejected") || !strings.Contains(out, "at least 8 characters") {
		t.Fatalf("want a WARN naming the rejection and the rule, got %q", out)
	}
	m := loggedPassword.FindAllStringSubmatch(out, -1)
	if len(m) != 1 || m[0][1] == "corta" {
		t.Fatalf("want the generated password logged exactly once, got %q", out)
	}
	if !identity.VerifyPassword(users[0].PasswordHash, m[0][1]) || identity.VerifyPassword(users[0].PasswordHash, "corta") {
		t.Fatal("the stored password must be the generated one")
	}
}

func TestEnsureInitialAdminEightCharacterPasswordAccepted(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, buf := captureLog()
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "abcd1234", log); err != nil {
		t.Fatal(err)
	}
	users := listUsers(t, st)
	if len(users) != 1 || users[0].MustChangePassword || !identity.VerifyPassword(users[0].PasswordHash, "abcd1234") {
		t.Fatalf("users = %+v, want one admin with the configured password", users)
	}
	if strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("unexpected WARN: %q", buf.String())
	}
}

func TestEnsureInitialAdminConcurrent(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, _ := captureLog()
	errs := make(chan error, 4)
	for i := 0; i < cap(errs); i++ {
		go func() { errs <- bootstrap.EnsureInitialAdmin(ctx, st, "admin", "Clave-Inicial-2026", log) }()
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

func TestEnsureInitialAdminSkipsWhenPasswordlessPlatformUserExists(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, buf := captureLog()
	if _, _, err := bootstrap.PlatformAdmin(ctx, st, "root"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "", log); err != nil {
		t.Fatal(err)
	}
	if users := listUsers(t, st); len(users) != 1 || users[0].Username != "root" || strings.Contains(buf.String(), "password=") {
		t.Fatalf("upgrade added an admin: %+v %q", users, buf.String())
	}
}

func TestEnsureInitialAdminSkipsWhenOnlySoftDeletedPlatformUserExists(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, _ := captureLog()
	if _, _, err := bootstrap.PlatformAdmin(ctx, st, "root"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, "UPDATE users SET deleted_at = now()"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "", log); err != nil {
		t.Fatal(err)
	}
	if users := listUsers(t, st); len(users) != 0 {
		t.Fatalf("users = %+v, want none", users)
	}
}

func TestEnsureInitialAdminExistingAdminIgnoresWeakConfiguredPassword(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, _ := captureLog()
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "Clave-Inicial-2026", log); err != nil {
		t.Fatal(err)
	}
	log2, buf := captureLog()
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "corta", log2); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "level=ERROR") {
		t.Fatalf("policy checked despite existing admin: %q", buf.String())
	}
}

func TestEnsureInitialAdminWarnsOnTenantUserNameCollision(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		tn, err := q.CreateTenant(ctx, db.CreateTenantParams{Slug: "muni", Name: "Muni"})
		if err != nil {
			return err
		}
		_, err = q.CreateUser(ctx, db.CreateUserParams{TenantID: &tn.ID, Username: "admin", DisplayName: "admin"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	log, buf := captureLog()
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "", log); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "tenant user") || !strings.Contains(out, "OPENVMS_ADMIN_USERNAME") || strings.Contains(out, "password=") {
		t.Fatalf("log = %q", out)
	}
}

func TestResetPlatformAdminPasswordForcesChangeAndClearsLockout(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, _ := captureLog()
	if err := bootstrap.EnsureInitialAdmin(ctx, st, "admin", "Clave-Inicial-2026", log); err != nil {
		t.Fatal(err)
	}
	var user db.User
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		user, err = q.GetUserByUsername(ctx, "admin")
		if err != nil {
			return err
		}
		if err := q.SetPassword(ctx, db.SetPasswordParams{ID: user.ID, PasswordHash: user.PasswordHash}); err != nil {
			return err
		}
		_, err = q.RecordLoginFailure(ctx, db.RecordLoginFailureParams{ID: user.ID, MaxFailures: 1, LockSeconds: 600})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.ResetPlatformAdminPassword(ctx, st, "admin", "New-Strong-Password-2026"); err != nil {
		t.Fatal(err)
	}
	users := listUsers(t, st)
	if len(users) != 1 {
		t.Fatalf("users = %d, want 1", len(users))
	}
	got := users[0]
	if !got.MustChangePassword || got.FailedLogins != 0 || got.LockedUntil != nil {
		t.Fatalf("reset state = must_change:%t failed:%d locked:%v", got.MustChangePassword, got.FailedLogins, got.LockedUntil)
	}
	if !identity.VerifyPassword(got.PasswordHash, "New-Strong-Password-2026") || identity.VerifyPassword(got.PasswordHash, "Clave-Inicial-2026") {
		t.Fatal("reset password hash did not replace the prior password")
	}
}

func TestResetPlatformAdminPasswordRejectsTenantUsers(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	var original db.User
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		tenant, err := q.CreateTenant(ctx, db.CreateTenantParams{Slug: "tenant-reset", Name: "Tenant Reset"})
		if err != nil {
			return err
		}
		original, err = q.CreateUser(ctx, db.CreateUserParams{TenantID: &tenant.ID, Username: "tenant-admin", DisplayName: "Tenant Admin"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err := bootstrap.ResetPlatformAdminPassword(ctx, st, "tenant-admin", "New-Strong-Password-2026")
	if err == nil || !strings.Contains(err.Error(), "tenant user") {
		t.Fatalf("ResetPlatformAdminPassword() error = %v, want tenant-user refusal", err)
	}
	users := listUsers(t, st)
	if len(users) != 1 || users[0].ID != original.ID || users[0].PasswordHash != "" {
		t.Fatalf("tenant user changed during refused reset: %+v", users)
	}
}
