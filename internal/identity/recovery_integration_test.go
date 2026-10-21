//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type recoveryEnv struct {
	t    *testing.T
	ctx  context.Context
	st   *store.Store
	svc  *Service
	logs *bytes.Buffer
	now  time.Time
	// ownerURL connects as the database owner, for fixtures the app role may not write
	// (audit_log is append-only for it).
	ownerURL string
}

var loggedCode = regexp.MustCompile(`code=([0-9]{6})`)

func newRecoveryEnv(t *testing.T) *recoveryEnv {
	t.Helper()
	pool, url := pgtest.Migrated(t)
	e := &recoveryEnv{t: t, ctx: context.Background(), st: &store.Store{Pool: pool}, logs: &bytes.Buffer{}, now: time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC), ownerURL: url}
	e.svc = &Service{
		Store: e.st, Log: slog.New(slog.NewTextHandler(e.logs, nil)),
		SessionTTL: time.Hour, IdleTimeout: time.Hour, MaxFailures: 5, LockFor: 15 * time.Minute,
		RecoveryLogCodes: true,
	}
	e.svc.now = func() time.Time { return e.now }
	return e
}

// user creates an account with password "Old-Password-1"; tenant users get a fresh tenant.
func (e *recoveryEnv) user(username string, platform bool, status string) db.User {
	e.t.Helper()
	var u db.User
	if err := e.st.Tx(e.ctx, store.AllTenants, func(q *db.Queries) error {
		params := db.CreateUserParams{Username: username, DisplayName: username}
		if !platform {
			tenant, err := q.CreateTenant(e.ctx, db.CreateTenantParams{Name: "T " + username, Slug: "t-" + username})
			if err != nil {
				return err
			}
			params.TenantID = &tenant.ID
		}
		var err error
		if u, err = q.CreateUser(e.ctx, params); err != nil {
			return err
		}
		hash, err := HashPassword("Old-Password-1")
		if err != nil {
			return err
		}
		return q.SetPassword(e.ctx, db.SetPasswordParams{ID: u.ID, PasswordHash: hash, MustChangePassword: true})
	}); err != nil {
		e.t.Fatal(err)
	}
	if status != "active" {
		if _, err := e.st.Pool.Exec(e.ctx, "UPDATE users SET status = $2 WHERE id = $1", u.ID, status); err != nil {
			e.t.Fatal(err)
		}
	}
	return u
}

func (e *recoveryEnv) request(username string) string {
	e.t.Helper()
	e.logs.Reset()
	if err := e.svc.RequestPasswordRecovery(e.ctx, username); err != nil {
		e.t.Fatalf("request %q: %v", username, err)
	}
	if m := loggedCode.FindStringSubmatch(e.logs.String()); m != nil {
		return m[1]
	}
	return ""
}

func (e *recoveryEnv) codeRows(id uuid.UUID) int {
	e.t.Helper()
	var n int
	if err := e.st.Pool.QueryRow(e.ctx, "SELECT count(*) FROM password_recovery_codes WHERE user_id = $1", id).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *recoveryEnv) auditCount(action string) int {
	e.t.Helper()
	var n int
	if err := e.st.Pool.QueryRow(e.ctx, "SELECT count(*) FROM audit_log WHERE action = $1", action).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestPasswordRecoveryRecoversPlatformAdmin(t *testing.T) {
	e := newRecoveryEnv(t)
	admin := e.user("admin", true, "active")
	if _, err := e.st.Pool.Exec(e.ctx, "UPDATE users SET failed_logins = 5, locked_until = $2 WHERE id = $1", admin.ID, e.now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	code := e.request("admin")
	if code == "" {
		t.Fatalf("no 6-digit code in the log: %s", e.logs.String())
	}
	if err := e.svc.ConfirmPasswordRecovery(e.ctx, "admin", code, "New-Password-22"); err != nil {
		t.Fatal(err)
	}

	res, err := e.svc.Login(e.ctx, LoginInput{Username: "admin", Password: "New-Password-22"})
	if err != nil || res.Token == "" {
		t.Fatalf("login with the recovered password: %v", err)
	}
	if res.User.MustChangePassword {
		t.Fatal("the operator chose the new password: no forced change")
	}
	if e.codeRows(admin.ID) != 0 {
		t.Fatal("a used code must be deleted")
	}
	if err := e.svc.ConfirmPasswordRecovery(e.ctx, "admin", code, "Another-Pass-33"); !errors.Is(err, ErrInvalidRecoveryCode) {
		t.Fatalf("reusing a code: %v, want ErrInvalidRecoveryCode", err)
	}
	if e.auditCount(ActionPasswordRecoveryRequested) != 1 || e.auditCount(ActionPasswordRecovered) != 1 {
		t.Fatal("request and recovery must be audited")
	}
	var details string
	if err := e.st.Pool.QueryRow(e.ctx, "SELECT string_agg(details::text, ' ') FROM audit_log WHERE action LIKE 'PASSWORD_RECOVER%'").Scan(&details); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains([]byte(details), []byte(code)) {
		t.Fatal("the code must never reach the audit log")
	}
}

func TestPasswordRecoveryIgnoresIneligibleAccounts(t *testing.T) {
	e := newRecoveryEnv(t)
	tenantUser := e.user("operador", false, "active")
	disabled := e.user("old-admin", true, "disabled")
	for _, name := range []string{"operador", "old-admin", "nobody"} {
		if code := e.request(name); code != "" {
			t.Fatalf("%s: a code was logged for an ineligible account", name)
		}
	}
	if e.codeRows(tenantUser.ID)+e.codeRows(disabled.ID) != 0 {
		t.Fatal("no code may be stored for ineligible accounts")
	}
	if err := e.svc.ConfirmPasswordRecovery(e.ctx, "nobody", "123456", "New-Password-22"); !errors.Is(err, ErrInvalidRecoveryCode) {
		t.Fatalf("unknown user confirm: %v, want the same ErrInvalidRecoveryCode", err)
	}
}

func TestPasswordRecoveryLimitsAttempts(t *testing.T) {
	e := newRecoveryEnv(t)
	admin := e.user("admin", true, "active")
	code := e.request("admin")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := range MaxRecoveryAttempts {
		if err := e.svc.ConfirmPasswordRecovery(e.ctx, "admin", wrong, "New-Password-22"); !errors.Is(err, ErrInvalidRecoveryCode) {
			t.Fatalf("wrong attempt %d: %v", i+1, err)
		}
	}
	if e.codeRows(admin.ID) != 1 {
		t.Fatal("an exhausted code stays stored (unusable) so the cooldown keeps applying")
	}
	if err := e.svc.ConfirmPasswordRecovery(e.ctx, "admin", code, "New-Password-22"); !errors.Is(err, ErrInvalidRecoveryCode) {
		t.Fatalf("right code after exhaustion: %v, want ErrInvalidRecoveryCode", err)
	}
	if e.auditCount(ActionPasswordRecoveryFailed) != MaxRecoveryAttempts+1 {
		t.Fatalf("every failed confirmation must be audited, got %d", e.auditCount(ActionPasswordRecoveryFailed))
	}
}

func TestPasswordRecoveryCodeExpires(t *testing.T) {
	e := newRecoveryEnv(t)
	e.user("admin", true, "active")
	code := e.request("admin")
	e.now = e.now.Add(RecoveryCodeTTL + time.Second)
	if err := e.svc.ConfirmPasswordRecovery(e.ctx, "admin", code, "New-Password-22"); !errors.Is(err, ErrInvalidRecoveryCode) {
		t.Fatalf("expired code: %v, want ErrInvalidRecoveryCode", err)
	}
}

func TestPasswordRecoveryCooldownKeepsTheActiveCode(t *testing.T) {
	e := newRecoveryEnv(t)
	e.user("admin", true, "active")
	first := e.request("admin")
	e.now = e.now.Add(10 * time.Second)
	if again := e.request("admin"); again != "" {
		t.Fatal("a request inside the cooldown must not log a new code")
	}
	e.now = e.now.Add(RecoveryRequestCooldown)
	second := e.request("admin")
	if second == "" {
		t.Fatal("after the cooldown a new code must be logged")
	}
	if first != second {
		if err := e.svc.ConfirmPasswordRecovery(e.ctx, "admin", first, "New-Password-22"); !errors.Is(err, ErrInvalidRecoveryCode) {
			t.Fatalf("a replaced code must stop working: %v", err)
		}
	}
	if err := e.svc.ConfirmPasswordRecovery(e.ctx, "admin", second, "New-Password-22"); err != nil {
		t.Fatalf("the latest code must work: %v", err)
	}
}

func TestPasswordRecoveryWeakPasswordKeepsTheCode(t *testing.T) {
	e := newRecoveryEnv(t)
	admin := e.user("admin", true, "active")
	code := e.request("admin")
	if err := e.svc.ConfirmPasswordRecovery(e.ctx, "admin", code, "short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak password: %v, want ErrWeakPassword", err)
	}
	if e.codeRows(admin.ID) != 1 {
		t.Fatal("a policy error must not consume the code")
	}
	if err := e.svc.ConfirmPasswordRecovery(e.ctx, "admin", code, "New-Password-22"); err != nil {
		t.Fatalf("retry with a valid password: %v", err)
	}
}

func TestPasswordRecoveryDisabled(t *testing.T) {
	e := newRecoveryEnv(t)
	e.user("admin", true, "active")
	e.svc.RecoveryLogCodes = false
	if err := e.svc.RequestPasswordRecovery(e.ctx, "admin"); !errors.Is(err, ErrRecoveryDisabled) {
		t.Fatalf("request while disabled: %v", err)
	}
	if err := e.svc.ConfirmPasswordRecovery(e.ctx, "admin", "123456", "New-Password-22"); !errors.Is(err, ErrRecoveryDisabled) {
		t.Fatalf("confirm while disabled: %v", err)
	}
	if loggedCode.MatchString(e.logs.String()) {
		t.Fatal("no code may be logged while disabled")
	}
}

// TestPasswordRecoveryCooldownSurvivesExhaustion closes the brute-force loop: burning a code's
// attempts must not allow an immediate new code, so guesses stay bounded by the cooldown.
func TestPasswordRecoveryCooldownSurvivesExhaustion(t *testing.T) {
	e := newRecoveryEnv(t)
	e.user("admin", true, "active")
	code := e.request("admin")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for range MaxRecoveryAttempts {
		_ = e.svc.ConfirmPasswordRecovery(e.ctx, "admin", wrong, "New-Password-22")
	}
	e.now = e.now.Add(time.Second)
	if again := e.request("admin"); again != "" {
		t.Fatal("exhausting a code must not skip the cooldown")
	}
	// An expired code keeps the cooldown too (it is measured from the last code).
	e.now = e.now.Add(RecoveryRequestCooldown)
	if next := e.request("admin"); next == "" {
		t.Fatal("after the cooldown a new code must be issued")
	}
}

// TestPasswordRecoveryConcurrentRequestsLogOneCode: a double click must not log two codes (the
// first one already invalid) nor bypass the cooldown.
func TestPasswordRecoveryConcurrentRequestsLogOneCode(t *testing.T) {
	e := newRecoveryEnv(t)
	for round := range 8 {
		name := fmt.Sprintf("admin%d", round)
		e.user(name, true, "active")
		e.logs.Reset()
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := e.svc.RequestPasswordRecovery(e.ctx, name); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if got := len(loggedCode.FindAllString(e.logs.String(), -1)); got != 1 {
			t.Fatalf("round %d: %d codes logged for concurrent requests, want 1", round, got)
		}
	}
}

// TestPasswordRecoveryDailyFailureCap bounds slow guessing across many codes and IPs: after
// MaxRecoveryFailuresPerDay wrong codes the account cannot recover for 24 hours, even with the
// right code; attempts rejected by the cap do not extend it, and the block is visible in the log.
func TestPasswordRecoveryDailyFailureCap(t *testing.T) {
	e := newRecoveryEnv(t)
	admin := e.user("admin", true, "active")
	failures := 0
	var lastCode string
	for failures < MaxRecoveryFailuresPerDay {
		lastCode = e.request("admin")
		if lastCode == "" {
			t.Fatalf("after %d failures a new code was expected", failures)
		}
		wrong := "000000"
		if lastCode == wrong {
			wrong = "111111"
		}
		for range MaxRecoveryAttempts {
			if failures == MaxRecoveryFailuresPerDay {
				break
			}
			_ = e.svc.ConfirmPasswordRecovery(e.ctx, "admin", wrong, "New-Password-22")
			failures++
		}
		e.now = e.now.Add(RecoveryRequestCooldown + time.Second)
	}

	if code := e.request("admin"); code != "" {
		t.Fatal("no code may be logged once the daily failure cap is reached")
	}
	if !strings.Contains(e.logs.String(), "password recovery blocked") {
		t.Fatalf("a blocked request must be visible in the log: %s", e.logs.String())
	}

	// The right, unexpired, unexhausted code is refused while blocked.
	if _, err := e.st.Pool.Exec(e.ctx, "UPDATE password_recovery_codes SET attempts = 0, expires_at = $2 WHERE user_id = $1", admin.ID, e.now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// As many refused attempts as the cap: if they counted, the block would renew itself.
	for range MaxRecoveryFailuresPerDay {
		if err := e.svc.ConfirmPasswordRecovery(e.ctx, "admin", lastCode, "New-Password-22"); !errors.Is(err, ErrInvalidRecoveryCode) {
			t.Fatalf("right code past the cap: %v, want ErrInvalidRecoveryCode", err)
		}
	}
	if _, err := e.svc.Login(e.ctx, LoginInput{Username: "admin", Password: "New-Password-22"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("the password must be unchanged while blocked: %v", err)
	}

	// Once the real wrong guesses age out, recovery reopens: rejections caused by the cap itself
	// must not have extended it.
	owner, err := pgx.Connect(e.ctx, e.ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(e.ctx)
	// Simulate the day passing: the append-only trigger is skipped for this fixture session only.
	if _, err := owner.Exec(e.ctx, "SET session_replication_role = replica"); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(e.ctx, "UPDATE audit_log SET occurred_at = occurred_at - interval '25 hours' WHERE action = $1 AND details->>'reason' = 'wrong_code'", ActionPasswordRecoveryFailed); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(RecoveryRequestCooldown + time.Second)
	if code := e.request("admin"); code == "" {
		t.Fatal("recovery must reopen once the wrong guesses are older than 24 hours")
	}
}
