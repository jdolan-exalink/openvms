//go:build integration

package identity

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func TestLoginLockoutDisabledIgnoresPriorLockAndTracksFailures(t *testing.T) {
	ctx := context.Background()
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	const username, password = "lockout-test", "Correct-Horse-9"
	var user db.User
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		user, err = q.CreateUser(ctx, db.CreateUserParams{Username: username, DisplayName: username})
		if err != nil {
			return err
		}
		hash, err := HashPassword(password)
		if err != nil {
			return err
		}
		if err := q.SetPassword(ctx, db.SetPasswordParams{ID: user.ID, PasswordHash: hash}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, "UPDATE users SET failed_logins = 4, locked_until = now() + interval '1 hour' WHERE id = $1", user.ID); err != nil {
		t.Fatal(err)
	}

	svc := &Service{Store: st, Log: slog.New(slog.DiscardHandler), SessionTTL: time.Hour, IdleTimeout: time.Hour, MaxFailures: 0, LockFor: 15 * time.Minute}
	if _, err := svc.Login(ctx, LoginInput{Username: username, Password: "wrong-password"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("bad password while locked: got %v, want invalid credentials, not a lock response", err)
	}
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		got, err := q.GetUserByUsername(ctx, username)
		if err != nil {
			return err
		}
		if got.FailedLogins != 5 || got.LockedUntil == nil || !got.LockedUntil.After(time.Now()) {
			t.Fatalf("after failed login: failures=%d locked_until=%v; want count 5 and prior lock unchanged", got.FailedLogins, got.LockedUntil)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if result, err := svc.Login(ctx, LoginInput{Username: username, Password: password}); err != nil || result.Token == "" {
		t.Fatalf("valid password during prior lock: token=%q err=%v", result.Token, err)
	}
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		got, err := q.GetUserByUsername(ctx, username)
		if err != nil {
			return err
		}
		if got.FailedLogins != 0 || got.LockedUntil != nil {
			t.Fatalf("after successful login: failures=%d locked_until=%v, want 0/nil", got.FailedLogins, got.LockedUntil)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
