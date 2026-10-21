package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

var recoveryCodeSpace = big.NewInt(1_000_000)

// newRecoveryCode draws a uniformly random 6-digit code.
func newRecoveryCode() (string, error) {
	n, err := rand.Int(rand.Reader, recoveryCodeSpace)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// hashRecoveryCode stores a code as salted SHA-256. The code space is small, so the real
// protection is online: few attempts and a short lifetime, not the hash.
func hashRecoveryCode(code string) (salt, hash []byte, err error) {
	salt = make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, err
	}
	return salt, recoveryDigest(code, salt), nil
}

func recoveryCodeMatches(code string, salt, hash []byte) bool {
	return subtle.ConstantTimeCompare(recoveryDigest(code, salt), hash) == 1
}

func recoveryDigest(code string, salt []byte) []byte {
	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(code))
	return h.Sum(nil)
}

const (
	// RecoveryCodeTTL is how long a logged recovery code stays valid.
	RecoveryCodeTTL = 10 * time.Minute
	// RecoveryRequestCooldown is the minimum time between two codes for one account.
	RecoveryRequestCooldown = time.Minute
	// MaxRecoveryAttempts wrong codes invalidate the active code.
	MaxRecoveryAttempts = 5
	// MaxRecoveryFailuresPerDay failed confirmations block recovery of an account for 24 hours.
	// It bounds slow guessing across codes and source IPs (about 3650 guesses a year against a
	// 10^6 code space); a blocked admin can still use tools/reset_admin.sh on the host.
	MaxRecoveryFailuresPerDay = 10
)

// Audit actions for log-code password recovery. Codes never reach the audit log.
const (
	ActionPasswordRecoveryRequested = "PASSWORD_RECOVERY_REQUESTED"
	ActionPasswordRecoveryFailed    = "PASSWORD_RECOVERY_FAILED"
	ActionPasswordRecovered         = "PASSWORD_RECOVERED"
)

var (
	// ErrRecoveryDisabled means PASSWORD_RECOVERY_LOG_CODES is off.
	ErrRecoveryDisabled = errors.New("password recovery is disabled")
	// ErrInvalidRecoveryCode hides whether the account, the code or its lifetime was wrong.
	ErrInvalidRecoveryCode = errors.New("invalid or expired recovery code")
)

// recoverable is a platform administrator that may recover access through the log code.
func recoverable(u db.User) bool { return u.TenantID == nil && u.Status == "active" }

// RequestPasswordRecovery writes a fresh 6-digit code to the API log for an active platform
// admin. It answers the same for any username so callers cannot probe accounts; within the
// cooldown the active code is kept and nothing new is logged.
func (s *Service) RequestPasswordRecovery(ctx context.Context, username string) error {
	if !s.RecoveryLogCodes {
		return ErrRecoveryDisabled
	}
	username = strings.TrimSpace(username)
	now := s.clock()
	var code string
	var user db.User
	err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		u, err := q.GetUserByUsername(ctx, username)
		if errors.Is(store.Classify(err), store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !recoverable(u) {
			return nil
		}
		// Concurrent requests (a double click) must see each other's code: lock the account row,
		// since the code row may not exist yet for FOR UPDATE to lock.
		if err := q.LockUserForRecovery(ctx, u.ID); err != nil {
			return err
		}
		if blocked, err := recoveryBlocked(ctx, q, u.ID); err != nil || blocked {
			return err
		}
		active, err := q.GetPasswordRecoveryCode(ctx, u.ID)
		switch {
		case err == nil && now.Sub(active.CreatedAt) < RecoveryRequestCooldown:
			return nil
		case err != nil && !errors.Is(store.Classify(err), store.ErrNotFound):
			return err
		}
		if code, err = newRecoveryCode(); err != nil {
			return err
		}
		salt, hash, err := hashRecoveryCode(code)
		if err != nil {
			return err
		}
		if err := q.UpsertPasswordRecoveryCode(ctx, db.UpsertPasswordRecoveryCodeParams{
			UserID: u.ID, CodeSalt: salt, CodeHash: hash, CreatedAt: now, ExpiresAt: now.Add(RecoveryCodeTTL),
		}); err != nil {
			return err
		}
		user = u
		return audit(ctx, q, &u.ID, u.Username, nil, ActionPasswordRecoveryRequested, "user", &u.ID, nil)
	})
	if err != nil {
		return err
	}
	if code != "" {
		// The log is the delivery channel: only whoever can read the API log can use the code.
		s.logger().Warn("platform admin password recovery code", "user", user.Username, "code", code,
			"expires_in", RecoveryCodeTTL.String(), "ip", ipString(httpx.ClientIP(ctx)))
	}
	return nil
}

// ConfirmPasswordRecovery sets newPassword when code is the account's active, unexpired code.
// Every wrong answer is ErrInvalidRecoveryCode; after MaxRecoveryAttempts the code is unusable.
// The account's lock and sessions are cleared; MFA is left as it was.
func (s *Service) ConfirmPasswordRecovery(ctx context.Context, username, code, newPassword string) error {
	if !s.RecoveryLogCodes {
		return ErrRecoveryDisabled
	}
	// The policy is the same for everyone, so checking it first leaks nothing and spends no attempt.
	if err := CheckPolicy(newPassword); err != nil {
		return err
	}
	username, code = strings.TrimSpace(username), strings.TrimSpace(code)
	now := s.clock()
	var failure error
	err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		fail := func(u *db.User, reason string) error {
			failure = ErrInvalidRecoveryCode
			if u == nil {
				return audit(ctx, q, nil, username, nil, ActionPasswordRecoveryFailed, "user", nil, map[string]any{"reason": reason})
			}
			return audit(ctx, q, &u.ID, u.Username, u.TenantID, ActionPasswordRecoveryFailed, "user", &u.ID, map[string]any{"reason": reason})
		}
		u, err := q.GetUserByUsername(ctx, username)
		if errors.Is(store.Classify(err), store.ErrNotFound) {
			return fail(nil, "unknown_user")
		}
		if err != nil {
			return err
		}
		if !recoverable(u) {
			return fail(&u, "not_recoverable")
		}
		if blocked, err := recoveryBlocked(ctx, q, u.ID); err != nil {
			return err
		} else if blocked {
			return fail(&u, "daily_limit")
		}
		active, err := q.GetPasswordRecoveryCode(ctx, u.ID)
		if errors.Is(store.Classify(err), store.ErrNotFound) {
			return fail(&u, "no_code")
		}
		if err != nil {
			return err
		}
		// Expired and exhausted codes stay stored (unusable) so the request cooldown, measured from
		// the last code, still bounds how fast new codes, and so guesses, can be obtained.
		if now.After(active.ExpiresAt) {
			return fail(&u, "expired")
		}
		if active.Attempts >= MaxRecoveryAttempts {
			return fail(&u, "exhausted")
		}
		if !recoveryCodeMatches(code, active.CodeSalt, active.CodeHash) {
			if _, err := q.IncrementPasswordRecoveryAttempts(ctx, u.ID); err != nil {
				return err
			}
			return fail(&u, "wrong_code")
		}
		hash, err := HashPassword(newPassword)
		if err != nil {
			return err
		}
		if err := q.SetPassword(ctx, db.SetPasswordParams{ID: u.ID, PasswordHash: hash}); err != nil {
			return err
		}
		if err := q.RevokeUserSessions(ctx, db.RevokeUserSessionsParams{UserID: u.ID}); err != nil {
			return err
		}
		if err := q.DeletePasswordRecoveryCode(ctx, u.ID); err != nil {
			return err
		}
		return audit(ctx, q, &u.ID, u.Username, nil, ActionPasswordRecovered, "user", &u.ID, nil)
	})
	if err != nil {
		return err
	}
	return failure
}

func ipString(ip *netip.Addr) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}

func (s *Service) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// recoveryBlocked reports an account that reached MaxRecoveryFailuresPerDay in the last 24 hours.
func recoveryBlocked(ctx context.Context, q *db.Queries, userID uuid.UUID) (bool, error) {
	n, err := q.CountRecentRecoveryFailures(ctx, &userID)
	return n >= MaxRecoveryFailuresPerDay, err
}
