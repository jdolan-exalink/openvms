package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type actorKey struct{}

type sessionTokenKey struct{}

type revalidateKey struct{}

// Revalidator re-runs the credential lookup Authenticate performed for a request, without
// side effects (no last-seen or last-used touch). valid=false means the credential no longer
// authenticates; a non-nil error means the store could not answer.
type Revalidator func(ctx context.Context) (valid bool, err error)

// RevalidatorFrom returns the Revalidator of the request's credential, for long-lived
// connections (the /ws feed) that must notice logout, revocation, expiry or a disabled user.
func RevalidatorFrom(ctx context.Context) func(context.Context) (bool, error) {
	r, _ := ctx.Value(revalidateKey{}).(Revalidator)
	if r == nil {
		return nil
	}
	return r
}

// ActorFrom returns the authenticated actor. Handlers behind Authenticate always have one.
func ActorFrom(ctx context.Context) (authz.Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(authz.Actor)
	return a, ok
}

func WithActor(ctx context.Context, a authz.Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// sessionToken returns the session cookie value of the request, if it used one.
func sessionToken(ctx context.Context) string {
	s, _ := ctx.Value(sessionTokenKey{}).(string)
	return s
}

// HashToken is how API tokens are stored: only their SHA-256 ever reaches the database.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// SessionCookie is the name of the browser session cookie.
const SessionCookie = "openvms_session"

// CSRFHeader must accompany every state-changing request authenticated by the session
// cookie. Browsers only send custom headers from same-origin scripts (or after a CORS
// preflight the API never grants), so a cross-site form cannot forge it.
const CSRFHeader = "X-OpenVMS-Request"

// publicPaths never require authentication.
var publicPaths = map[string]bool{
	"/api/v1/system/info": true,
	"/api/v1/auth/login":  true,
	// The agent holds no session; its one-time enrollment token is the credential.
	"/api/v1/agent/enroll": true,
}

// AuthOptions configure Authenticate.
type AuthOptions struct {
	// IdleTimeout ends a session after this long without requests.
	IdleTimeout time.Duration
}

// Authenticate resolves either "Authorization: Bearer <api token>" (scripts, integrations)
// or the session cookie (browser) for every /api/v1 and /media route and the /ws push feed.
func Authenticate(q *db.Queries, opts AuthOptions) func(http.Handler) http.Handler {
	idle := opts.IdleTimeout
	if idle <= 0 {
		idle = 2 * time.Hour
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			protected := strings.HasPrefix(r.URL.Path, "/api/v1/") || strings.HasPrefix(r.URL.Path, "/media/") || r.URL.Path == "/ws"
			if !protected || publicPaths[r.URL.Path] || strings.HasPrefix(r.URL.Path, "/media/v1/public/") || strings.HasPrefix(r.URL.Path, "/api/v1/public/") {
				next.ServeHTTP(w, r)
				return
			}
			if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && token != "" {
				row, err := q.GetActorByTokenHash(r.Context(), HashToken(token))
				if errors.Is(err, pgx.ErrNoRows) {
					writeError(w, r, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
					return
				}
				if err != nil {
					writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
					return
				}
				_ = q.TouchAPIToken(r.Context(), row.TokenID)
				actor := authz.Actor{UserID: row.ID, Username: row.Username, TenantID: row.TenantID}
				hash := HashToken(token)
				revalidate := Revalidator(func(ctx context.Context) (bool, error) {
					_, err := q.GetActorByTokenHash(ctx, hash)
					return lookupValid(err)
				})
				ctx := context.WithValue(WithActor(r.Context(), actor), revalidateKey{}, revalidate)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			c, err := r.Cookie(SessionCookie)
			if err != nil || c.Value == "" {
				writeError(w, r, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(CSRFHeader) == "" {
				writeError(w, r, http.StatusForbidden, "csrf", "missing "+CSRFHeader+" header")
				return
			}
			sessionParams := db.GetActorBySessionHashParams{TokenHash: identity.HashToken(c.Value), IdleSeconds: idle.Seconds()}
			row, err := q.GetActorBySessionHash(r.Context(), sessionParams)
			if errors.Is(err, pgx.ErrNoRows) {
				clearSessionCookie(w, r)
				writeError(w, r, http.StatusUnauthorized, "unauthorized", "session expired")
				return
			}
			if err != nil {
				writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
				return
			}
			_ = q.TouchSession(r.Context(), row.SessionID)
			sid := row.SessionID
			actor := authz.Actor{UserID: row.ID, Username: row.Username, TenantID: row.TenantID, SessionID: &sid}
			ctx := context.WithValue(WithActor(r.Context(), actor), sessionTokenKey{}, c.Value)
			ctx = context.WithValue(ctx, revalidateKey{}, Revalidator(func(ctx context.Context) (bool, error) {
				_, err := q.GetActorBySessionHash(ctx, sessionParams)
				return lookupValid(err)
			}))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// lookupValid maps a credential lookup result: no row means the credential ended.
func lookupValid(err error) (bool, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// secureRequest reports whether the browser reached us over HTTPS (directly or via Caddy).
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// #nosec G124 -- Secure intentionally follows HTTPS/TLS detection; forcing it on breaks HTTP deployments.
func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteStrictMode,
	})
}
