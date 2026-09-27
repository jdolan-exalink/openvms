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
}

// AuthOptions configure Authenticate.
type AuthOptions struct {
	// IdleTimeout ends a session after this long without requests.
	IdleTimeout time.Duration
}

// Authenticate resolves either "Authorization: Bearer <api token>" (scripts, integrations)
// or the session cookie (browser) for every /api/v1 and /media route.
func Authenticate(q *db.Queries, opts AuthOptions) func(http.Handler) http.Handler {
	idle := opts.IdleTimeout
	if idle <= 0 {
		idle = 2 * time.Hour
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			protected := strings.HasPrefix(r.URL.Path, "/api/v1/") || strings.HasPrefix(r.URL.Path, "/media/")
			if !protected || publicPaths[r.URL.Path] {
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
				next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), actor)))
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
			row, err := q.GetActorBySessionHash(r.Context(), db.GetActorBySessionHashParams{
				TokenHash: identity.HashToken(c.Value), IdleSeconds: idle.Seconds(),
			})
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
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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
