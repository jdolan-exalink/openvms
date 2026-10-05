package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

var (
	// ErrInvalidCredentials hides whether the user or the password was wrong.
	ErrInvalidCredentials = errors.New("invalid username or password")
	// ErrLocked means too many failed attempts; the account unlocks by itself.
	ErrLocked = errors.New("account temporarily locked after repeated failed logins")
	// ErrMFAInvalid means the TOTP code was wrong or missing.
	ErrMFAInvalid = errors.New("invalid verification code")
)

// Audit actions (PRD §66).
const (
	ActionLoginSuccess   = "LOGIN_SUCCESS"
	ActionLoginFailed    = "LOGIN_FAILED"
	ActionLogout         = "LOGOUT"
	ActionPasswordChange = "PASSWORD_CHANGED"
	ActionMFAEnabled     = "MFA_ENABLED"
	ActionMFADisabled    = "MFA_DISABLED"
	ActionUserCreated    = "USER_CREATED"
	ActionUserUpdated    = "USER_UPDATED"
	ActionUserRemoved    = "USER_REMOVED"
	ActionGroupChanged   = "USER_GROUP_CHANGED"
)

type Service struct {
	Store  *store.Store
	Sealer *secrets.Sealer
	Log    *slog.Logger
	// SessionTTL is the absolute lifetime of a session; IdleTimeout ends it earlier
	// without activity.
	SessionTTL  time.Duration
	IdleTimeout time.Duration
	MaxFailures int
	LockFor     time.Duration
	Issuer      string

	now func() time.Time
}

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Service) tx(ctx context.Context, actor authz.Actor, fn func(q *db.Queries, c *access.Checker) error) error {
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		c, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		return fn(q, c)
	})
}

func invalid(format string, args ...any) error {
	return &inventory.ValidationError{Msg: fmt.Sprintf(format, args...)}
}

func audit(ctx context.Context, q *db.Queries, actorID *uuid.UUID, actorName string, tenantID *uuid.UUID, action, targetType string, targetID *uuid.UUID, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return q.InsertAudit(ctx, db.InsertAuditParams{
		TenantID: tenantID, ActorID: actorID, ActorName: actorName, Action: action,
		TargetType: targetType, TargetID: targetID, RequestID: logging.RequestID(ctx),
		Ip: httpx.ClientIP(ctx), Details: b,
	})
}

// HashToken is how session tokens are stored.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ovs_" + base64.RawURLEncoding.EncodeToString(b), nil
}

type LoginInput struct {
	Username  string
	Password  string
	TOTP      string
	IP        *netip.Addr
	UserAgent string
}

type LoginResult struct {
	// MFARequired means the password was right and a TOTP code is needed; no session yet.
	MFARequired bool
	Token       string
	ExpiresAt   time.Time
	User        db.User
}

// Login checks the password (and TOTP when enabled) and opens a session.
func (s *Service) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	if s.MaxFailures < 0 || uint64(s.MaxFailures) > uint64(^uint32(0)>>1) {
		return LoginResult{}, invalid("max failures is out of range")
	}
	var res LoginResult
	username := strings.TrimSpace(in.Username)
	var failure error
	err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		u, err := q.GetUserByUsername(ctx, username)
		if err != nil {
			if errors.Is(store.Classify(err), store.ErrNotFound) {
				VerifyPassword(dummyHash, in.Password)
				failure = ErrInvalidCredentials
				return audit(ctx, q, nil, username, nil, ActionLoginFailed, "user", nil, map[string]any{"reason": "unknown_user"})
			}
			return err
		}
		if u.LockedUntil != nil && u.LockedUntil.After(s.clock()) {
			failure = ErrLocked
			return audit(ctx, q, &u.ID, u.Username, u.TenantID, ActionLoginFailed, "user", &u.ID, map[string]any{"reason": "locked"})
		}
		if u.Status != "active" || !VerifyPassword(u.PasswordHash, in.Password) {
			failure = ErrInvalidCredentials
			reason := "bad_password"
			if u.Status != "active" {
				reason = "status_" + u.Status
			}
			if s.MaxFailures < 0 || s.MaxFailures > 1<<31-1 {
				return invalid("max failures is out of range")
			}
			if _, err := q.RecordLoginFailure(ctx, db.RecordLoginFailureParams{ID: u.ID, MaxFailures: int32(s.MaxFailures), LockSeconds: s.LockFor.Seconds()}); err != nil {
				return err
			}
			return audit(ctx, q, &u.ID, u.Username, u.TenantID, ActionLoginFailed, "user", &u.ID, map[string]any{"reason": reason})
		}
		if u.MfaEnabled {
			if in.TOTP == "" {
				res.MFARequired = true
				return nil
			}
			secret, err := s.Sealer.Open(u.MfaSecretSealed, u.ID[:])
			if err != nil {
				return err
			}
			if !VerifyTOTP(string(secret), in.TOTP, s.clock()) {
				failure = ErrMFAInvalid
				if s.MaxFailures < 0 || s.MaxFailures > 1<<31-1 {
					return invalid("max failures is out of range")
				}
				if _, err := q.RecordLoginFailure(ctx, db.RecordLoginFailureParams{ID: u.ID, MaxFailures: int32(s.MaxFailures), LockSeconds: s.LockFor.Seconds()}); err != nil {
					return err
				}
				return audit(ctx, q, &u.ID, u.Username, u.TenantID, ActionLoginFailed, "user", &u.ID, map[string]any{"reason": "bad_totp"})
			}
		}
		token, err := newSessionToken()
		if err != nil {
			return err
		}
		expires := s.clock().Add(s.SessionTTL)
		sid, err := q.CreateSession(ctx, db.CreateSessionParams{UserID: u.ID, TokenHash: HashToken(token), ExpiresAt: expires, Ip: in.IP, UserAgent: truncate(in.UserAgent, 300)})
		if err != nil {
			return err
		}
		if err := q.RecordLoginSuccess(ctx, u.ID); err != nil {
			return err
		}
		res = LoginResult{Token: token, ExpiresAt: expires, User: u}
		return audit(ctx, q, &u.ID, u.Username, u.TenantID, ActionLoginSuccess, "session", &sid, map[string]any{"mfa": u.MfaEnabled})
	})
	if err != nil {
		return LoginResult{}, err
	}
	if failure != nil {
		return LoginResult{}, failure
	}
	return res, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Logout revokes the session behind token.
func (s *Service) Logout(ctx context.Context, actor authz.Actor, token string) error {
	return s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		if err := q.RevokeSession(ctx, HashToken(token)); err != nil {
			return err
		}
		return audit(ctx, q, &actor.UserID, actor.Username, actor.TenantID, ActionLogout, "session", actor.SessionID, nil)
	})
}

// ChangePassword lets a user change their own password. Other sessions are closed.
func (s *Service) ChangePassword(ctx context.Context, actor authz.Actor, current, next string) error {
	if err := CheckPolicy(next, actor.Username); err != nil {
		return invalid("%s", err.Error())
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	var failure error
	err = s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		u, err := q.GetUser(ctx, actor.UserID)
		if err != nil {
			return store.Classify(err)
		}
		// Users created without a password (API-token only) may set one without a current one.
		if u.PasswordHash != "" && !VerifyPassword(u.PasswordHash, current) {
			failure = invalid("the current password is not correct")
			return nil
		}
		if err := q.SetPassword(ctx, db.SetPasswordParams{ID: u.ID, PasswordHash: hash, MustChangePassword: false}); err != nil {
			return err
		}
		if err := q.RevokeUserSessions(ctx, db.RevokeUserSessionsParams{UserID: u.ID, Keep: actor.SessionID}); err != nil {
			return err
		}
		return audit(ctx, q, &u.ID, u.Username, u.TenantID, ActionPasswordChange, "user", &u.ID, map[string]any{"self": true})
	})
	if err != nil {
		return err
	}
	return failure
}

// SetupMFA generates a new TOTP secret for the user. It is not enforced until
// EnableMFA proves the user's app produces valid codes.
func (s *Service) SetupMFA(ctx context.Context, actor authz.Actor) (secret, otpURL string, err error) {
	secret, err = NewTOTPSecret()
	if err != nil {
		return "", "", err
	}
	sealed, err := s.Sealer.Seal([]byte(secret), actor.UserID[:])
	if err != nil {
		return "", "", err
	}
	err = s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		u, err := q.GetUser(ctx, actor.UserID)
		if err != nil {
			return store.Classify(err)
		}
		if u.MfaEnabled {
			return invalid("MFA is already enabled; disable it first")
		}
		return q.SetMFASecret(ctx, db.SetMFASecretParams{ID: u.ID, MfaSecretSealed: sealed, MfaEnabled: false})
	})
	if err != nil {
		return "", "", err
	}
	return secret, TOTPURL(s.Issuer, actor.Username, secret), nil
}

func (s *Service) EnableMFA(ctx context.Context, actor authz.Actor, code string) error {
	var failure error
	err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		u, err := q.GetUser(ctx, actor.UserID)
		if err != nil {
			return store.Classify(err)
		}
		if len(u.MfaSecretSealed) == 0 {
			return invalid("start the MFA setup first")
		}
		secret, err := s.Sealer.Open(u.MfaSecretSealed, u.ID[:])
		if err != nil {
			return err
		}
		if !VerifyTOTP(string(secret), code, s.clock()) {
			failure = invalid("the code is not valid; check the time on your phone")
			return nil
		}
		if err := q.SetMFASecret(ctx, db.SetMFASecretParams{ID: u.ID, MfaSecretSealed: u.MfaSecretSealed, MfaEnabled: true}); err != nil {
			return err
		}
		return audit(ctx, q, &u.ID, u.Username, u.TenantID, ActionMFAEnabled, "user", &u.ID, nil)
	})
	if err != nil {
		return err
	}
	return failure
}

// DisableMFA needs the current password so a stolen session cannot remove the second factor.
func (s *Service) DisableMFA(ctx context.Context, actor authz.Actor, password string) error {
	var failure error
	err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		u, err := q.GetUser(ctx, actor.UserID)
		if err != nil {
			return store.Classify(err)
		}
		if !VerifyPassword(u.PasswordHash, password) {
			failure = invalid("the password is not correct")
			return nil
		}
		if err := q.SetMFASecret(ctx, db.SetMFASecretParams{ID: u.ID, MfaSecretSealed: nil, MfaEnabled: false}); err != nil {
			return err
		}
		return audit(ctx, q, &u.ID, u.Username, u.TenantID, ActionMFADisabled, "user", &u.ID, map[string]any{"self": true})
	})
	if err != nil {
		return err
	}
	return failure
}

// scopeOf is the resource user administration is checked against: the user's tenant,
// or the platform for platform users and groups.
func scopeOf(tenantID *uuid.UUID) authz.Resource {
	if tenantID == nil {
		return authz.Resource{Kind: authz.ScopePlatform}
	}
	return access.Tenant(*tenantID)
}

// User is a user as administrators see it.
type User struct {
	db.User
	GroupIDs []uuid.UUID
}

func (s *Service) loadUser(ctx context.Context, q *db.Queries, id uuid.UUID) (User, error) {
	u, err := q.GetUser(ctx, id)
	if err != nil {
		return User{}, store.Classify(err)
	}
	groups, err := q.ListUserGroupIDs(ctx, id)
	if err != nil {
		return User{}, err
	}
	return User{User: u, GroupIDs: groups}, nil
}

// visibleTenant reports whether a tenant user may see a row of tenantID at all. Rows of
// other tenants answer 404, like the rest of the API.
func visibleTenant(actor authz.Actor, tenantID *uuid.UUID) bool {
	if actor.IsPlatform() {
		return true
	}
	return tenantID != nil && *tenantID == *actor.TenantID
}

func (s *Service) ListUsers(ctx context.Context, actor authz.Actor, tenantID *uuid.UUID) ([]User, error) {
	var out []User
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		filter := tenantID
		if !actor.IsPlatform() {
			filter = actor.TenantID
		}
		rows, err := q.ListUsers(ctx, db.ListUsersParams{TenantID: filter, IncludePlatform: actor.IsPlatform() && tenantID == nil})
		if err != nil {
			return err
		}
		for _, u := range rows {
			if !c.Can(authz.UsersView, scopeOf(u.TenantID)) {
				continue
			}
			groups, err := q.ListUserGroupIDs(ctx, u.ID)
			if err != nil {
				return err
			}
			out = append(out, User{User: u, GroupIDs: groups})
		}
		return nil
	})
	if out == nil {
		out = []User{}
	}
	return out, err
}

func (s *Service) GetUser(ctx context.Context, actor authz.Actor, id uuid.UUID) (User, error) {
	var out User
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		u, err := s.loadUser(ctx, q, id)
		if err != nil {
			return err
		}
		if !visibleTenant(actor, u.TenantID) {
			return store.ErrNotFound
		}
		if u.ID != actor.UserID {
			if err := c.Require(authz.UsersView, scopeOf(u.TenantID)); err != nil {
				return err
			}
		}
		out = u
		return nil
	})
	return out, err
}

type CreateUserInput struct {
	TenantID    *uuid.UUID
	Username    string
	DisplayName string
	Email       *string
	Password    string
	// MustChange forces a new password at first login (the admin chose a temporary one).
	MustChange bool
	GroupIDs   []uuid.UUID
}

func validUsername(u string) bool {
	if len(u) < 3 || len(u) > 64 {
		return false
	}
	for _, r := range u {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("._-@", r) {
			return false
		}
	}
	return true
}

func (s *Service) CreateUser(ctx context.Context, actor authz.Actor, in CreateUserInput) (User, error) {
	in.Username = strings.TrimSpace(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if !validUsername(in.Username) {
		return User{}, invalid("username must be 3-64 letters, digits or . _ - @")
	}
	if in.DisplayName == "" {
		in.DisplayName = in.Username
	}
	if !actor.IsPlatform() {
		if in.TenantID == nil {
			in.TenantID = actor.TenantID
		} else if *in.TenantID != *actor.TenantID {
			return User{}, store.ErrNotFound
		}
	}
	hash := ""
	if in.Password != "" {
		if err := CheckPolicy(in.Password, in.Username); err != nil {
			return User{}, invalid("%s", err.Error())
		}
		var err error
		if hash, err = HashPassword(in.Password); err != nil {
			return User{}, err
		}
	}
	var id uuid.UUID
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if err := c.Require(authz.UsersManage, scopeOf(in.TenantID)); err != nil {
			return err
		}
		u, err := q.CreateUser(ctx, db.CreateUserParams{TenantID: in.TenantID, Username: in.Username, Email: in.Email, DisplayName: in.DisplayName})
		if err != nil {
			return store.Classify(err)
		}
		id = u.ID
		if hash != "" {
			if err := q.SetPassword(ctx, db.SetPasswordParams{ID: u.ID, PasswordHash: hash, MustChangePassword: in.MustChange}); err != nil {
				return err
			}
		}
		if err := s.setMemberships(ctx, q, c, u.TenantID, u.ID, in.GroupIDs); err != nil {
			return err
		}
		return audit(ctx, q, &actor.UserID, actor.Username, in.TenantID, ActionUserCreated, "user", &u.ID,
			map[string]any{"username": in.Username, "groups": len(in.GroupIDs), "password_set": hash != ""})
	})
	if err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, actor, id)
}

// setMemberships replaces a user's groups. Every group must be in the user's tenant and
// the actor needs groups.manage there: joining a group grants its permissions.
func (s *Service) setMemberships(ctx context.Context, q *db.Queries, c *access.Checker, tenantID *uuid.UUID, userID uuid.UUID, groupIDs []uuid.UUID) error {
	if groupIDs == nil {
		return nil
	}
	for _, gid := range groupIDs {
		g, err := q.GetUserGroup(ctx, gid)
		if err != nil {
			return invalid("group %s does not exist", gid)
		}
		if (g.TenantID == nil) != (tenantID == nil) || (g.TenantID != nil && *g.TenantID != *tenantID) {
			return invalid("group %s belongs to another tenant", gid)
		}
	}
	if err := c.Require(authz.GroupsManage, scopeOf(tenantID)); err != nil {
		return err
	}
	if err := q.RemoveUserFromGroups(ctx, userID); err != nil {
		return err
	}
	for _, gid := range groupIDs {
		if err := q.AddUserToGroup(ctx, db.AddUserToGroupParams{GroupID: gid, UserID: userID}); err != nil {
			return err
		}
	}
	return nil
}

type UpdateUserInput struct {
	DisplayName *string
	Email       *string
	Status      *string
	// Password resets the password (administrators); MustChange applies with it.
	Password   *string
	MustChange bool
	GroupIDs   *[]uuid.UUID
	// DisableMFA removes the user's second factor (lost phone).
	DisableMFA bool
}

func (s *Service) UpdateUser(ctx context.Context, actor authz.Actor, id uuid.UUID, in UpdateUserInput) (User, error) {
	if in.Status != nil {
		switch *in.Status {
		case "active", "disabled", "locked":
		default:
			return User{}, invalid("status must be active, disabled or locked")
		}
	}
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		u, err := q.GetUser(ctx, id)
		if err != nil {
			return store.Classify(err)
		}
		if !visibleTenant(actor, u.TenantID) {
			return store.ErrNotFound
		}
		if err := c.Require(authz.UsersManage, scopeOf(u.TenantID)); err != nil {
			return err
		}
		if u.ID == actor.UserID && in.Status != nil && *in.Status != "active" {
			return invalid("you cannot disable your own account")
		}
		if err := q.UpdateUser(ctx, db.UpdateUserParams{ID: id, DisplayName: in.DisplayName, Email: in.Email, Status: in.Status}); err != nil {
			return err
		}
		changed := map[string]any{}
		if in.Password != nil {
			if err := CheckPolicy(*in.Password, u.Username); err != nil {
				return invalid("%s", err.Error())
			}
			hash, err := HashPassword(*in.Password)
			if err != nil {
				return err
			}
			if err := q.SetPassword(ctx, db.SetPasswordParams{ID: id, PasswordHash: hash, MustChangePassword: in.MustChange}); err != nil {
				return err
			}
			if err := q.RevokeUserSessions(ctx, db.RevokeUserSessionsParams{UserID: id}); err != nil {
				return err
			}
			changed["password_reset"] = true
		}
		if in.Status != nil && *in.Status != "active" {
			if err := q.RevokeUserSessions(ctx, db.RevokeUserSessionsParams{UserID: id}); err != nil {
				return err
			}
			changed["status"] = *in.Status
		}
		if in.DisableMFA {
			if err := q.SetMFASecret(ctx, db.SetMFASecretParams{ID: id, MfaSecretSealed: nil, MfaEnabled: false}); err != nil {
				return err
			}
			changed["mfa_disabled"] = true
		}
		if in.GroupIDs != nil {
			if err := s.setMemberships(ctx, q, c, u.TenantID, id, *in.GroupIDs); err != nil {
				return err
			}
			changed["groups"] = len(*in.GroupIDs)
		}
		return audit(ctx, q, &actor.UserID, actor.Username, u.TenantID, ActionUserUpdated, "user", &id, changed)
	})
	if err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, actor, id)
}

func (s *Service) DeleteUser(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	if id == actor.UserID {
		return invalid("you cannot delete your own account")
	}
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		u, err := q.GetUser(ctx, id)
		if err != nil {
			return store.Classify(err)
		}
		if !visibleTenant(actor, u.TenantID) {
			return store.ErrNotFound
		}
		if err := c.Require(authz.UsersManage, scopeOf(u.TenantID)); err != nil {
			return err
		}
		if err := q.RevokeUserSessions(ctx, db.RevokeUserSessionsParams{UserID: id}); err != nil {
			return err
		}
		if err := q.SoftDeleteUser(ctx, db.SoftDeleteUserParams{ID: id, DeletedBy: &actor.UserID}); err != nil {
			return err
		}
		return audit(ctx, q, &actor.UserID, actor.Username, u.TenantID, ActionUserRemoved, "user", &id, map[string]any{"username": u.Username})
	})
}

// Group is a user group with its members.
type Group struct {
	db.UserGroup
	MemberIDs []uuid.UUID
}

func (s *Service) ListGroups(ctx context.Context, actor authz.Actor, tenantID *uuid.UUID) ([]Group, error) {
	var out []Group
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		filter := tenantID
		if !actor.IsPlatform() {
			filter = actor.TenantID
		}
		rows, err := q.ListUserGroups(ctx, db.ListUserGroupsParams{TenantID: filter, IncludePlatform: actor.IsPlatform() && tenantID == nil})
		if err != nil {
			return err
		}
		for _, g := range rows {
			if !c.Can(authz.GroupsView, scopeOf(g.TenantID)) {
				continue
			}
			members, err := q.ListGroupMembers(ctx, g.ID)
			if err != nil {
				return err
			}
			out = append(out, Group{UserGroup: db.UserGroup{ID: g.ID, TenantID: g.TenantID, Name: g.Name, Description: g.Description,
				CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}, MemberIDs: members})
		}
		return nil
	})
	if out == nil {
		out = []Group{}
	}
	return out, err
}

func (s *Service) GetGroup(ctx context.Context, actor authz.Actor, id uuid.UUID) (Group, error) {
	var out Group
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		g, err := q.GetUserGroup(ctx, id)
		if err != nil {
			return store.Classify(err)
		}
		if !visibleTenant(actor, g.TenantID) {
			return store.ErrNotFound
		}
		if err := c.Require(authz.GroupsView, scopeOf(g.TenantID)); err != nil {
			return err
		}
		members, err := q.ListGroupMembers(ctx, id)
		if err != nil {
			return err
		}
		out = Group{UserGroup: g, MemberIDs: members}
		return nil
	})
	return out, err
}

type GroupInput struct {
	TenantID    *uuid.UUID
	Name        string
	Description string
	MemberIDs   []uuid.UUID
}

func (s *Service) replaceMembers(ctx context.Context, q *db.Queries, g db.UserGroup, members []uuid.UUID) error {
	for _, uid := range members {
		u, err := q.GetUser(ctx, uid)
		if err != nil {
			return invalid("user %s does not exist", uid)
		}
		if (u.TenantID == nil) != (g.TenantID == nil) || (u.TenantID != nil && *u.TenantID != *g.TenantID) {
			return invalid("user %s belongs to another tenant", uid)
		}
	}
	if err := q.ClearGroupMembers(ctx, g.ID); err != nil {
		return err
	}
	for _, uid := range members {
		if err := q.AddUserToGroup(ctx, db.AddUserToGroupParams{GroupID: g.ID, UserID: uid}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) CreateGroup(ctx context.Context, actor authz.Actor, in GroupInput) (Group, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return Group{}, invalid("name is required")
	}
	if !actor.IsPlatform() {
		if in.TenantID == nil {
			in.TenantID = actor.TenantID
		} else if *in.TenantID != *actor.TenantID {
			return Group{}, store.ErrNotFound
		}
	}
	var id uuid.UUID
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if err := c.Require(authz.GroupsManage, scopeOf(in.TenantID)); err != nil {
			return err
		}
		if in.TenantID != nil {
			if _, err := q.GetTenant(ctx, *in.TenantID); err != nil {
				return store.Classify(err)
			}
		}
		g, err := q.CreateUserGroup(ctx, db.CreateUserGroupParams{TenantID: in.TenantID, Name: in.Name, Description: in.Description})
		if err != nil {
			return store.Classify(err)
		}
		id = g.ID
		if err := s.replaceMembers(ctx, q, g, in.MemberIDs); err != nil {
			return err
		}
		return audit(ctx, q, &actor.UserID, actor.Username, in.TenantID, ActionGroupChanged, "user_group", &g.ID,
			map[string]any{"created": true, "name": in.Name, "members": len(in.MemberIDs)})
	})
	if err != nil {
		return Group{}, err
	}
	return s.GetGroup(ctx, actor, id)
}

func (s *Service) ReplaceGroup(ctx context.Context, actor authz.Actor, id uuid.UUID, in GroupInput) (Group, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return Group{}, invalid("name is required")
	}
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		g, err := q.GetUserGroup(ctx, id)
		if err != nil {
			return store.Classify(err)
		}
		if !visibleTenant(actor, g.TenantID) {
			return store.ErrNotFound
		}
		if err := c.Require(authz.GroupsManage, scopeOf(g.TenantID)); err != nil {
			return err
		}
		if err := q.UpdateUserGroup(ctx, db.UpdateUserGroupParams{ID: id, Name: in.Name, Description: in.Description}); err != nil {
			return store.Classify(err)
		}
		if err := s.replaceMembers(ctx, q, g, in.MemberIDs); err != nil {
			return err
		}
		return audit(ctx, q, &actor.UserID, actor.Username, g.TenantID, ActionGroupChanged, "user_group", &id,
			map[string]any{"name": in.Name, "members": len(in.MemberIDs)})
	})
	if err != nil {
		return Group{}, err
	}
	return s.GetGroup(ctx, actor, id)
}

func (s *Service) DeleteGroup(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		g, err := q.GetUserGroup(ctx, id)
		if err != nil {
			return store.Classify(err)
		}
		if !visibleTenant(actor, g.TenantID) {
			return store.ErrNotFound
		}
		if err := c.Require(authz.GroupsManage, scopeOf(g.TenantID)); err != nil {
			return err
		}
		if err := q.SoftDeleteUserGroup(ctx, db.SoftDeleteUserGroupParams{ID: id, DeletedBy: &actor.UserID}); err != nil {
			return err
		}
		return audit(ctx, q, &actor.UserID, actor.Username, g.TenantID, ActionGroupChanged, "user_group", &id, map[string]any{"deleted": true, "name": g.Name})
	})
}

type AuditFilter struct {
	TenantID *uuid.UUID
	Action   *string
	ActorID  *uuid.UUID
	From     *time.Time
	To       *time.Time
	BeforeID *int64
	Limit    int
}

// ListAudit reads the audit trail (audit.view). Tenant users only see their tenant;
// platform entries (tenant NULL) need audit.view at platform scope.
func (s *Service) ListAudit(ctx context.Context, actor authz.Actor, f AuditFilter) ([]db.AuditLog, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	var out []db.AuditLog
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		tenant := f.TenantID
		allTenants := false
		if actor.IsPlatform() {
			allTenants = tenant == nil && c.Can(authz.AuditView, authz.Resource{Kind: authz.ScopePlatform})
		} else {
			tenant = actor.TenantID
		}
		if tenant != nil {
			if err := c.Require(authz.AuditView, access.Tenant(*tenant)); err != nil {
				return err
			}
		} else if !allTenants {
			return access.ErrForbidden
		}
		if f.Limit < 0 || f.Limit > 500 {
			return invalid("audit limit is out of range")
		}
		rows, err := q.ListAudit(ctx, db.ListAuditParams{
			TenantID: tenant, AllTenants: allTenants, Action: f.Action, ActorID: f.ActorID,
			FromTime: f.From, ToTime: f.To, BeforeID: f.BeforeID, MaxRows: int32(f.Limit),
		})
		out = rows
		return err
	})
	if out == nil {
		out = []db.AuditLog{}
	}
	return out, err
}

// ResolveSession verifies a session token or API token hash and returns the Actor.
func (s *Service) ResolveSession(ctx context.Context, tokenHash []byte) (authz.Actor, error) {
	var actor authz.Actor
	idle := s.IdleTimeout
	if idle <= 0 {
		idle = 2 * time.Hour
	}

	err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		// 1. Try session hash first
		sessParams := db.GetActorBySessionHashParams{
			TokenHash:   tokenHash,
			IdleSeconds: idle.Seconds(),
		}
		row, err := q.GetActorBySessionHash(ctx, sessParams)
		if err == nil {
			_ = q.TouchSession(ctx, row.SessionID)
			sid := row.SessionID
			actor = authz.Actor{
				UserID:    row.ID,
				Username:  row.Username,
				TenantID:  row.TenantID,
				SessionID: &sid,
			}
			return nil
		}

		// 2. Try API token hash next
		apiRow, apiErr := q.GetActorByTokenHash(ctx, tokenHash)
		if apiErr == nil {
			_ = q.TouchAPIToken(ctx, apiRow.TokenID)
			actor = authz.Actor{
				UserID:   apiRow.ID,
				Username: apiRow.Username,
				TenantID: apiRow.TenantID,
			}
			return nil
		}

		return store.ErrNotFound
	})

	return actor, err
}

// RevokeSession revokes a session by its token hash.
func (s *Service) RevokeSession(ctx context.Context, tokenHash []byte) error {
	return s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		return q.RevokeSession(ctx, tokenHash)
	})
}

// RemoteClientCounts holds recent active client counts from database sessions and API tokens.
type RemoteClientCounts struct {
	WebSessions     int
	DesktopSessions int
	APITokens       int
}

// CountActiveClients returns active distinct users across web sessions, desktop sessions, and API tokens.
func (s *Service) CountActiveClients(ctx context.Context, window time.Duration) (RemoteClientCounts, error) {
	if s == nil || s.Store == nil || s.Store.Pool == nil {
		return RemoteClientCounts{}, nil
	}
	if window <= 0 {
		window = 5 * time.Minute
	}
	since := time.Now().Add(-window)

	var counts RemoteClientCounts
	_ = s.Store.Pool.QueryRow(ctx, `
		SELECT
			COALESCE(COUNT(DISTINCT CASE WHEN user_agent IS NULL OR user_agent NOT LIKE 'OpenVMS-Desktop/%' THEN user_id END), 0) AS web_count,
			COALESCE(COUNT(DISTINCT CASE WHEN user_agent LIKE 'OpenVMS-Desktop/%' THEN user_id END), 0) AS desktop_count
		FROM sessions
		WHERE revoked_at IS NULL
		  AND expires_at > now()
		  AND last_seen_at >= $1
	`, since).Scan(&counts.WebSessions, &counts.DesktopSessions)

	_ = s.Store.Pool.QueryRow(ctx, `
		SELECT COALESCE(COUNT(DISTINCT user_id), 0)
		FROM api_tokens
		WHERE revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > now())
		  AND last_used_at >= $1
	`, since).Scan(&counts.APITokens)

	return counts, nil
}

