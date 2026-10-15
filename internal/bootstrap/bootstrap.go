// Package bootstrap creates the first platform administrator and API tokens.
// It writes grants directly because, before it runs, nobody holds permissions.manage.
package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
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
		u, err := ensurePlatformAdmin(ctx, q, username)
		if err != nil {
			return err
		}
		token, err = IssueToken(ctx, q, u.ID, "bootstrap", 0)
		actor = authz.Actor{UserID: u.ID, Username: u.Username}
		return err
	})
	return actor, token, err
}

// ensurePlatformAdmin creates (or reuses) a platform user and reconciles every
// platform catalog grant. It issues no token.
func ensurePlatformAdmin(ctx context.Context, q *db.Queries, username string) (db.User, error) {
	u, err := q.GetUserByUsername(ctx, username)
	if err != nil {
		if !errors.Is(store.Classify(err), store.ErrNotFound) {
			return db.User{}, err
		}
		u, err = q.CreateUser(ctx, db.CreateUserParams{Username: username, DisplayName: username})
		if err != nil {
			return db.User{}, fmt.Errorf("create user: %w", err)
		}
	}
	if u.TenantID != nil {
		return db.User{}, fmt.Errorf("user %q belongs to a tenant; pick another name", username)
	}
	if _, err := reconcileCatalogGrants(ctx, q, u.ID); err != nil {
		return db.User{}, err
	}
	return u, nil
}

// DefaultAdminPassword is the initial admin password when OPENVMS_ADMIN_PASSWORD is unset.
// It is public: an install still using it is warned about on every start.
const DefaultAdminPassword = "openvms123"

// EnsureInitialAdmin creates the platform admin on a fresh install so the first login needs
// no vmsctl step. It creates or modifies nothing when a user with that name exists or any
// platform user already has a password, so an existing install is never reset. A default
// password forces a change at first login; a password failing the policy is logged and
// skipped. It warns while the named admin still uses the default password.
func EnsureInitialAdmin(ctx context.Context, st *store.Store, username, password string, log *slog.Logger) error {
	if err := identity.CheckPolicy(password, username); err != nil {
		log.Error("initial admin not created: OPENVMS_ADMIN_PASSWORD rejected", "username", username, "error", err)
		return nil
	}
	created, defaultInUse := false, false
	err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		users, err := q.ListUsers(ctx, db.ListUsersParams{IncludePlatform: true})
		if err != nil {
			return err
		}
		hasPassword, exists := false, false
		for _, u := range users {
			if strings.EqualFold(u.Username, username) {
				exists = true
				defaultInUse = u.TenantID == nil && identity.VerifyPassword(u.PasswordHash, DefaultAdminPassword)
			}
			hasPassword = hasPassword || (u.TenantID == nil && u.PasswordHash != "")
		}
		if exists || hasPassword {
			return nil
		}
		u, err := ensurePlatformAdmin(ctx, q, username)
		if err != nil {
			return err
		}
		hash, err := identity.HashPassword(password)
		if err != nil {
			return err
		}
		mustChange := password == DefaultAdminPassword
		if err := q.SetPassword(ctx, db.SetPasswordParams{ID: u.ID, PasswordHash: hash, MustChangePassword: mustChange}); err != nil {
			return err
		}
		created, defaultInUse = true, mustChange
		return nil
	})
	if errors.Is(store.Classify(err), store.ErrConflict) {
		return nil // a concurrent start created it first
	}
	if err != nil {
		return err
	}
	if created {
		log.Info("initial admin created", "username", username)
	}
	if defaultInUse {
		log.Warn("initial admin still uses the default password; change it", "username", username)
	}
	return nil
}

var (
	nonSlugRe     = regexp.MustCompile(`[^a-z0-9]+`)
	tenantSlugRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
	defaultSiteTZ = "America/Argentina/Cordoba"
)

// EnsureDefaultTenant creates one tenant and one site inside it on an install that has no
// tenant at all, so the first admin has somewhere to register servers. Any existing tenant
// makes it a no-op: nothing is renamed, recreated or added, so renames made from the web
// survive restarts. Names that are empty or yield no valid slug are logged and skipped.
// Startup creation has no acting user, so it writes no audit entry.
func EnsureDefaultTenant(ctx context.Context, st *store.Store, tenantName, siteName string, log *slog.Logger) error {
	tenantName, siteName = strings.TrimSpace(tenantName), strings.TrimSpace(siteName)
	slug := strings.Trim(nonSlugRe.ReplaceAllString(strings.ToLower(tenantName), "-"), "-")
	if siteName == "" || !tenantSlugRe.MatchString(slug) {
		log.Error("default tenant not created: OPENVMS_DEFAULT_TENANT and OPENVMS_DEFAULT_SITE need a usable name",
			"tenant", tenantName, "site", siteName)
		return nil
	}
	created := false
	err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		existing, err := q.ListTenants(ctx, nil)
		if err != nil || len(existing) > 0 {
			return err
		}
		t, err := q.CreateTenant(ctx, db.CreateTenantParams{Slug: slug, Name: tenantName})
		if err != nil {
			return err
		}
		if _, err := q.CreateSite(ctx, db.CreateSiteParams{TenantID: t.ID, Name: siteName, Timezone: defaultSiteTZ}); err != nil {
			return err
		}
		created = true
		return nil
	})
	if errors.Is(store.Classify(err), store.ErrConflict) {
		return nil // a concurrent start created it first
	}
	if err != nil {
		return err
	}
	if created {
		log.Info("default tenant created", "tenant", tenantName, "site", siteName)
	}
	return nil
}

// SyncPlatformAdmin adds missing platform-scope catalog ALLOW grants to an
// existing platform user. It does not create users or API tokens.
func SyncPlatformAdmin(ctx context.Context, st *store.Store, username string) (int, error) {
	added := 0
	err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		u, err := q.GetUserByUsername(ctx, username)
		if err != nil {
			return fmt.Errorf("user %q: %w", username, store.Classify(err))
		}
		if u.TenantID != nil {
			return fmt.Errorf("user %q belongs to a tenant; platform permission sync requires a platform user", username)
		}
		added, err = reconcileCatalogGrants(ctx, q, u.ID)
		return err
	})
	return added, err
}

func reconcileCatalogGrants(ctx context.Context, q *db.Queries, userID uuid.UUID) (int, error) {
	existing, err := q.ListGrantsForUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	have := map[string]bool{}
	for _, g := range existing {
		if g.ScopeType == string(authz.ScopePlatform) && g.Effect == string(authz.Allow) {
			have[g.Permission] = true
		}
	}
	added := 0
	for _, d := range authz.Catalog {
		if have[string(d.Permission)] {
			continue
		}
		if _, err := q.CreateGrant(ctx, db.CreateGrantParams{
			SubjectType: "user", SubjectID: userID, Permission: string(d.Permission),
			Effect: string(authz.Allow), ScopeType: string(authz.ScopePlatform), CreatedBy: &userID,
		}); err != nil {
			return 0, fmt.Errorf("grant %s: %w", d.Permission, err)
		}
		added++
	}
	return added, nil
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
