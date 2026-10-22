package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// authenticated reports whether Authenticate lets a request carrying only these cookies through.
func authenticated(t *testing.T, https bool, cookies ...string) bool {
	t.Helper()
	passed := false
	h := Authenticate(db.New(&revokableDB{}), AuthOptions{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { passed = true }))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	if https {
		req.Header.Set("X-Forwarded-Proto", "https")
	}
	for _, name := range cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: "sess", Secure: https, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return passed
}

func TestSessionCookieNameFollowsTheScheme(t *testing.T) {
	if SecureSessionCookie == HTTPSessionCookie || SecureSessionCookie == SessionCookie || HTTPSessionCookie == SessionCookie {
		t.Fatal("the HTTPS, HTTP and legacy session cookies must have distinct names")
	}
	if !strings.HasPrefix(SecureSessionCookie, "__Secure-") {
		t.Fatalf("the HTTPS cookie must carry the __Secure- prefix, got %q", SecureSessionCookie)
	}
}

func TestSessionCookieIsSetUnderTheNameOfItsScheme(t *testing.T) {
	for _, tc := range []struct {
		secure bool
		want   string
	}{{true, SecureSessionCookie}, {false, HTTPSessionCookie}} {
		ctx := context.WithValue(context.Background(), requestInfoKey{}, requestInfo{secure: tc.secure})
		c := sessionCookie(ctx, "tok", time.Now().Add(time.Hour))
		if !strings.HasPrefix(c, tc.want+"=tok") {
			t.Errorf("secure=%v: cookie %q, want name %q", tc.secure, c, tc.want)
		}
		if got := strings.Contains(c, "Secure"); got != tc.secure {
			t.Errorf("secure=%v: Secure attribute = %v", tc.secure, got)
		}
		if gone := sessionCookie(ctx, "", time.Time{}); !strings.HasPrefix(gone, tc.want+"=;") && !strings.HasPrefix(gone, tc.want+"=") {
			t.Errorf("secure=%v: logout cookie %q must clear %q", tc.secure, gone, tc.want)
		}
	}
}

func TestAuthenticateReadsTheCookieOfTheRequestScheme(t *testing.T) {
	for _, tc := range []struct {
		name    string
		https   bool
		cookies []string
		want    bool
	}{
		{"https with its own cookie", true, []string{SecureSessionCookie}, true},
		{"http with its own cookie", false, []string{HTTPSessionCookie}, true},
		{"https ignores the http cookie", true, []string{HTTPSessionCookie}, false},
		{"http ignores the https cookie", false, []string{SecureSessionCookie}, false},
		{"legacy cookie still works over https", true, []string{SessionCookie}, true},
		{"legacy cookie still works over http", false, []string{SessionCookie}, true},
		{"no cookie", true, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := authenticated(t, tc.https, tc.cookies...); got != tc.want {
				t.Fatalf("authenticated = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClearSessionCookieUsesTheNameOfTheScheme(t *testing.T) {
	for _, tc := range []struct {
		https bool
		want  string
	}{{true, SecureSessionCookie}, {false, HTTPSessionCookie}} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if tc.https {
			req.Header.Set("X-Forwarded-Proto", "https")
		}
		rec := httptest.NewRecorder()
		clearSessionCookie(rec, req)
		if got := rec.Header().Get("Set-Cookie"); !strings.HasPrefix(got, tc.want+"=") {
			t.Errorf("https=%v: %q, want name %q", tc.https, got, tc.want)
		}
	}
}
