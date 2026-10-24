package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/agent/provisionlpr"
	"github.com/jdolan-exalink/openvms/internal/agent/tokens"
)

type authFixture struct {
	mux   *http.ServeMux
	store *tokens.Store
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	store := tokens.Open(filepath.Join(t.TempDir(), "agent.tokens.json"))
	queue, err := provisionlpr.OpenEventQueue(t.TempDir(), 10, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	receiver := provisionlpr.NewReceiver(provisionlpr.DefaultConfig(), nil)
	receiver.SetEventQueue(queue)
	sampler := agent.NewSampler(agent.Paths{})
	auth := &authenticator{secret: "legacy-secret", tokens: store}
	updated := false
	mux := buildMuxWithAuth(auth, "test", sampler, nil, nil, nil, receiver, func(*http.Request) error { updated = true; return nil }, func() {})
	_ = updated
	return &authFixture{mux: mux, store: store}
}

func (f *authFixture) do(method, path, bearer string) int {
	req := httptest.NewRequest(method, path, strings.NewReader(`{"through":0}`))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec.Code
}

func (f *authFixture) token(t *testing.T, name string, scopes ...tokens.Scope) string {
	t.Helper()
	secret, _, err := f.store.Create(name, scopes, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

// What each scope opens, and what it does not.
func TestTokenScopesOpenOnlyTheirRoutes(t *testing.T) {
	f := newAuthFixture(t)
	read := f.token(t, "monitor", tokens.ScopeRead)
	events := f.token(t, "collector", tokens.ScopeEvents)
	control := f.token(t, "operator", tokens.ScopeControl)
	update := f.token(t, "updater", tokens.ScopeUpdate)

	type route struct{ method, path string }
	metrics := route{http.MethodGet, "/v1/metrics"}
	list := route{http.MethodGet, "/v1/provision-lpr/events"}
	ack := route{http.MethodPost, "/v1/provision-lpr/events/ack"}
	status := route{http.MethodGet, "/v1/provision-lpr/status"}
	registry := route{http.MethodGet, "/v1/provision-lpr/registry"}
	captures := route{http.MethodGet, "/v1/provision-lpr/captures"}
	upd := route{http.MethodPost, "/v1/update"}

	for _, tc := range []struct {
		name  string
		token string
		allow []route
	}{
		{"read", read, []route{metrics}},
		{"events", events, []route{list, ack, status}},
		{"control", control, []route{registry, captures}},
		{"update", update, []route{upd}},
		{"legacy secret", "legacy-secret", []route{metrics, list, ack, status, registry, captures, upd}},
	} {
		for _, r := range []route{metrics, list, ack, status, registry, captures, upd} {
			allowed := false
			for _, a := range tc.allow {
				allowed = allowed || a == r
			}
			got := f.do(r.method, r.path, tc.token)
			switch {
			case allowed && (got == http.StatusUnauthorized || got == http.StatusForbidden):
				t.Errorf("%s on %s %s = %d, want it allowed", tc.name, r.method, r.path, got)
			case !allowed && got != http.StatusForbidden && got != http.StatusUnauthorized:
				t.Errorf("%s on %s %s = %d, want 403", tc.name, r.method, r.path, got)
			case !allowed && tc.name != "legacy secret" && got != http.StatusForbidden:
				t.Errorf("%s on %s %s = %d, want 403 (a valid token without the scope)", tc.name, r.method, r.path, got)
			}
		}
	}
}

func TestUnknownRevokedAndMissingCredentialsGet401(t *testing.T) {
	f := newAuthFixture(t)
	secret := f.token(t, "monitor", tokens.ScopeRead)
	if got := f.do(http.MethodGet, "/v1/metrics", secret); got != http.StatusOK {
		t.Fatalf("setup: %d", got)
	}
	if err := f.store.Revoke("monitor"); err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string]string{"revoked": secret, "none": "", "wrong": "wrong", "malformed": "ovmsa_x_y"} {
		if got := f.do(http.MethodGet, "/v1/metrics", token); got != http.StatusUnauthorized {
			t.Errorf("%s credential = %d, want 401", name, got)
		}
	}
}

// The camera-facing routes carry no token and stay that way.
func TestCameraFacingRoutesStayOpen(t *testing.T) {
	f := newAuthFixture(t)
	if got := f.do(http.MethodPost, "/SendKeepalive", ""); got != http.StatusOK {
		t.Fatalf("keepalive = %d, want 200", got)
	}
}

// The HTTPS listener carries the same receiver as the HTTP one. Its management routes (status,
// registry, captures, readings) were reachable there without a token.
func TestTLSListenerProvisionRoutesNeedATokenToo(t *testing.T) {
	store := tokens.Open(filepath.Join(t.TempDir(), "agent.tokens.json"))
	queue, err := provisionlpr.OpenEventQueue(t.TempDir(), 10, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	receiver := provisionlpr.NewReceiver(provisionlpr.DefaultConfig(), nil)
	receiver.SetEventQueue(queue)
	auth := &authenticator{secret: "legacy-secret", tokens: store}
	mux := buildTLSMux(auth, nil, nil, nil, receiver)
	events, _, err := store.Create("collector", []tokens.Scope{tokens.ScopeEvents}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	do := func(method, path, bearer string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(`{"through":0}`))
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, path := range []string{"/v1/provision-lpr/status", "/v1/provision-lpr/events", "/v1/provision-lpr/registry", "/v1/provision-lpr/captures"} {
		if got := do(http.MethodGet, path, ""); got != http.StatusUnauthorized {
			t.Errorf("GET %s without a token on the TLS listener = %d, want 401", path, got)
		}
	}
	if got := do(http.MethodPost, "/v1/provision-lpr/registry/enforce", ""); got != http.StatusUnauthorized {
		t.Errorf("registry enforcement without a token = %d, want 401", got)
	}
	if got := do(http.MethodGet, "/v1/provision-lpr/events", events); got != http.StatusOK {
		t.Errorf("events with an events token = %d, want 200", got)
	}
	if got := do(http.MethodGet, "/v1/provision-lpr/registry", events); got != http.StatusForbidden {
		t.Errorf("registry with an events token = %d, want 403", got)
	}
	if got := do(http.MethodPost, "/SendKeepalive", ""); got != http.StatusOK {
		t.Errorf("camera keepalive = %d, want 200", got)
	}
}

// A token file that cannot be read refuses every console token. That is the safe answer, but the
// operator must be able to see why, instead of a wall of 401s.
func TestUnreadableTokenFileIsLoggedNotJustRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.tokens.json")
	store := tokens.Open(path)
	secret, _, err := store.Create("central", tokens.AllScopes, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	clock := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	auth := &authenticator{secret: "legacy", tokens: store, log: slog.New(slog.NewTextHandler(&logs, nil)), now: func() time.Time { return clock }}
	handler := auth.scoped(tokens.ScopeRead, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	call := func(bearer string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := call(secret); got != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", got)
	}
	if !strings.Contains(logs.String(), "token file") || strings.Contains(logs.String(), secret) {
		t.Fatalf("log = %q; want a warning about the token file and never the secret", logs.String())
	}
	// Once a minute, not once per request.
	logs.Reset()
	call(secret)
	call(secret)
	if logs.Len() != 0 {
		t.Fatalf("the warning repeated within a minute: %q", logs.String())
	}
	clock = clock.Add(2 * time.Minute)
	call(secret)
	if !strings.Contains(logs.String(), "token file") {
		t.Fatal("the warning should come back after a minute")
	}
	// A request that is not even shaped like a console token says nothing about the file.
	logs.Reset()
	clock = clock.Add(2 * time.Minute)
	call("wrong")
	if logs.Len() != 0 {
		t.Fatalf("a wrong legacy secret must not blame the token file: %q", logs.String())
	}
}
