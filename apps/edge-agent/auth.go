package main

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent/tokens"
)

// authenticator decides who may call the agent's routes. The token in /etc/openvms/agent.token keeps
// working with every scope (existing installs and the SSH installer rely on it); tokens created from
// the console carry the scopes they were given.
//
// The handlers behind the routes keep their own check against the legacy secret. For a console token
// the middleware therefore proves the token and the scope, then presents the legacy secret to the
// handler: a route added later without the middleware still refuses a console token.
type authenticator struct {
	secret string
	tokens *tokens.Store
	now    func() time.Time
}

func bearerValue(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	got := r.Header.Get("Authorization")
	if !strings.HasPrefix(got, prefix) {
		return "", false
	}
	return strings.TrimPrefix(got, prefix), true
}

// scoped lets a request through when it carries the legacy secret or a console token with the scope.
// A valid token without the scope gets 403; anything else gets 401.
func (a *authenticator) scoped(scope tokens.Scope, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented, ok := bearerValue(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if subtle.ConstantTimeCompare([]byte(presented), []byte(a.secret)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		if a.tokens != nil {
			now := time.Now
			if a.now != nil {
				now = a.now
			}
			tok, allowed := a.tokens.Authenticate(presented, scope, now())
			switch {
			case allowed:
				forwarded := r.Clone(r.Context())
				forwarded.Header = r.Header.Clone()
				forwarded.Header.Set("Authorization", "Bearer "+a.secret)
				next.ServeHTTP(w, forwarded)
				return
			case tok.ID != "":
				http.Error(w, "this token does not have the "+string(scope)+" scope", http.StatusForbidden)
				return
			}
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}
