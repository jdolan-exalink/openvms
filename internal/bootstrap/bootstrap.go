// Package bootstrap creates the first platform administrator and API tokens.
// It writes grants directly because, before it runs, nobody holds permissions.manage.
package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// NewToken returns a random API token. Only its hash is stored.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ovms_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// IssueToken creates an API token for userID and returns it in clear, once.
func IssueToken(ctx context.Context, q *db.Queries, userID uuid.UUID, name string, ttl time.Duration) (string, error) {
	token, err := NewToken()
	if err != nil {
		return "", err
	}
	var expires *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		expires = &t
	}
	if _, err := q.CreateAPIToken(ctx, db.CreateAPITokenParams{UserID: userID, Name: name, TokenHash: api.HashToken(token), ExpiresAt: expires}); err != nil {
		return "", fmt.Errorf("create token: %w", err)
	}
	return token, nil
}

// PlatformAdmin creates (or reuses) a platform user holding every permission at
// platform scope, and issues a fresh token for it.
func PlatformAdmin(ctx context.Context, st *store.Store, username string) (authz.Actor, string, error) {
	var actor authz.Actor
	var token string
	err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		u, err := q.GetUserByUsername(ctx, username)
		if err != nil {
			if !errors.Is(store.Classify(err), store.ErrNotFound) {
				return err
			}
			u, err = q.CreateUser(ctx, db.CreateUserParams{Username: username, DisplayName: username})
			if err != nil {
				return fmt.Errorf("create user: %w", err)
			}
		}
		if u.TenantID != nil {
			return fmt.Errorf("user %q belongs to a tenant; pick another name", username)
		}
		existing, err := q.ListGrantsForUser(ctx, u.ID)
		if err != nil {
			return err
		}
		have := map[string]bool{}
		for _, g := range existing {
			if g.ScopeType == string(authz.ScopePlatform) && g.Effect == string(authz.Allow) {
				have[g.Permission] = true
			}
		}
		for _, d := range authz.Catalog {
			if have[string(d.Permission)] {
				continue
			}
			if _, err := q.CreateGrant(ctx, db.CreateGrantParams{
				SubjectType: "user", SubjectID: u.ID, Permission: string(d.Permission),
				Effect: string(authz.Allow), ScopeType: string(authz.ScopePlatform), CreatedBy: &u.ID,
			}); err != nil {
				return fmt.Errorf("grant %s: %w", d.Permission, err)
			}
		}
		token, err = IssueToken(ctx, q, u.ID, "bootstrap", 0)
		actor = authz.Actor{UserID: u.ID, Username: u.Username}
		return err
	})
	return actor, token, err
}

// TenantUser creates a user in tenantID and issues a token for it.
func TenantUser(ctx context.Context, st *store.Store, tenantID uuid.UUID, username string) (authz.Actor, string, error) {
	var actor authz.Actor
	var token string
	err := st.Tx(ctx, store.TenantScope{TenantID: tenantID}, func(q *db.Queries) error {
		u, err := q.CreateUser(ctx, db.CreateUserParams{TenantID: &tenantID, Username: username, DisplayName: username})
		if err != nil {
			return fmt.Errorf("create user: %w", store.Classify(err))
		}
		token, err = IssueToken(ctx, q, u.ID, "cli", 0)
		actor = authz.Actor{UserID: u.ID, Username: u.Username, TenantID: &tenantID}
		return err
	})
	return actor, token, err
}

// SetPassword sets the login password of an existing user (vmsctl passwd), enforcing the
// password policy, and closes the user's open sessions.
func SetPassword(ctx context.Context, st *store.Store, username, password string) error {
	return st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		u, err := q.GetUserByUsername(ctx, username)
		if err != nil {
			return fmt.Errorf("user %q: %w", username, store.Classify(err))
		}
		if err := identity.CheckPolicy(password, u.Username); err != nil {
			return err
		}
		hash, err := identity.HashPassword(password)
		if err != nil {
			return err
		}
		if err := q.SetPassword(ctx, db.SetPasswordParams{ID: u.ID, PasswordHash: hash}); err != nil {
			return err
		}
		return q.RevokeUserSessions(ctx, db.RevokeUserSessionsParams{UserID: u.ID})
	})
}
