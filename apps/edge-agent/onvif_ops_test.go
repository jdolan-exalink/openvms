package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/agent/backup"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
)

// opsRoutes are the camera operations the control plane drives through the agent. The control plane
// reaches an installed agent over the TLS listener, and a route missing there answers 404, which it
// reads as "the agent is too old" and then refuses, for one, to configure a camera's video.
var opsRoutes = []string{
	"/v1/onvif/sync-time", "/v1/onvif/ptz", "/v1/onvif/video-config", "/v1/onvif/video-encoding",
	"/v1/onvif/video-device", "/v1/onvif/reboot", "/v1/onvif/imaging", "/v1/onvif/snapshot",
	"/v1/onvif/pull-events", "/v1/provision-lpr/configure",
}

func newTestOps(t *testing.T) *onvifdiscover.OpsHandler {
	t.Helper()
	cfg, _, err := onvifdiscover.LoadConfig("eth0", "192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	ops, err := onvifdiscover.NewOpsHandler("agent-secret", cfg)
	if err != nil {
		t.Fatal(err)
	}
	return ops
}

func TestTLSMuxServesEveryCameraOperationTheControlPlaneUses(t *testing.T) {
	mux := buildTLSMux(&authenticator{secret: "agent-secret"}, nil, nil, nil, nil, nil, newTestOps(t), nil)
	for _, path := range opsRoutes {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s on the TLS listener without a token: %d, want 401 (a 404 reads as an outdated agent)", path, rec.Code)
		}
	}
}

// Both listeners must register the same operations, so adding one to a listener and forgetting the
// other fails here instead of showing up as "update the agent" in the camera settings.
func TestPlainAndTLSMuxesRegisterTheSameCameraOperations(t *testing.T) {
	ops := newTestOps(t)
	plain := buildMux("agent-secret", "test", agent.NewSampler(agent.Paths{}), nil, nil, ops, nil, func(*http.Request) error { return nil }, func() {})
	var restarted bool
	core := newTestCore(t, func(*http.Request) error { return nil }, &restarted)
	mountBackup(plain, &authenticator{secret: "agent-secret"}, core.backup) // the plain listener mounts it from main
	tls := buildTLSMux(&authenticator{secret: "agent-secret"}, nil, nil, nil, nil, nil, ops, core)
	routes := []struct{ method, path string }{{http.MethodGet, "/v1/metrics"}, {http.MethodPost, "/v1/update"}, {http.MethodGet, "/v1/backup/original"}}
	for _, p := range opsRoutes {
		routes = append(routes, struct{ method, path string }{http.MethodPost, p})
	}
	for _, r := range routes {
		req := httptest.NewRequest(r.method, r.path, nil)
		_, onPlain := plain.Handler(req)
		_, onTLS := tls.Handler(req)
		if onPlain == "" || onTLS == "" {
			t.Errorf("%s %s: plain=%q tls=%q, want it registered on both", r.method, r.path, onPlain, onTLS)
		}
	}
}

func TestTLSMuxWithoutOpsDoesNotRegisterThem(t *testing.T) {
	mux := buildTLSMux(&authenticator{secret: "agent-secret"}, nil, nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/onvif/video-device", nil)
	if _, pattern := mux.Handler(req); pattern != "" {
		t.Fatalf("video-device registered without an ops handler: %q", pattern)
	}
}

func newTestCore(t *testing.T, replace func(*http.Request) error, restarted *bool) *agentCore {
	t.Helper()
	return &agentCore{
		variant: "test",
		sampler: agent.NewSampler(agent.Paths{}),
		replace: replace,
		restart: func() { *restarted = true },
		backup:  backup.New(t.TempDir()),
	}
}

func agentCall(mux http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// The control plane asks an installed agent for its status, replaces its binary and reads its
// configuration backup over the TLS listener; none of these may answer 404 there.
func TestTLSMuxServesStatusUpdateAndBackup(t *testing.T) {
	var restarted, replaced bool
	core := newTestCore(t, func(*http.Request) error { replaced = true; return nil }, &restarted)
	mux := buildTLSMux(&authenticator{secret: "agent-secret"}, nil, nil, nil, nil, nil, nil, core)

	for _, c := range []struct{ method, path string }{{http.MethodGet, "/v1/metrics"}, {http.MethodPost, "/v1/update"}, {http.MethodGet, "/v1/backup/original"}} {
		if got := agentCall(mux, c.method, c.path, "").Code; got != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d, want 401", c.method, c.path, got)
		}
	}
	if rec := agentCall(mux, http.MethodGet, "/v1/metrics", "agent-secret"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "{") {
		t.Errorf("metrics: %d %q", rec.Code, rec.Body.String())
	}
	if got := agentCall(mux, http.MethodGet, "/v1/backup/original", "agent-secret").Code; got != http.StatusNotFound {
		t.Errorf("backup with nothing stored: %d, want 404 from the handler itself", got)
	}
	if got := agentCall(mux, http.MethodPost, "/v1/update", "agent-secret").Code; got != http.StatusAccepted || !replaced || !restarted {
		t.Errorf("update: status=%d replaced=%v restarted=%v, want 202 and both", got, replaced, restarted)
	}
}

func TestTLSMuxUpdateRefusalKeepsItsStatusAndDoesNotRestart(t *testing.T) {
	var restarted bool
	core := newTestCore(t, func(*http.Request) error { return errors.New("agent updates are unavailable on this host") }, &restarted)
	mux := buildTLSMux(&authenticator{secret: "agent-secret"}, nil, nil, nil, nil, nil, nil, core)
	rec := agentCall(mux, http.MethodPost, "/v1/update", "agent-secret")
	if rec.Code < 400 || restarted {
		t.Fatalf("a refused update must fail and must not restart: %d restarted=%v", rec.Code, restarted)
	}
}

func TestTLSMuxWithoutACoreDoesNotRegisterIt(t *testing.T) {
	mux := buildTLSMux(&authenticator{secret: "agent-secret"}, nil, nil, nil, nil, nil, nil, nil)
	for _, path := range []string{"/v1/metrics", "/v1/update", "/v1/backup/original"} {
		if _, pattern := mux.Handler(httptest.NewRequest(http.MethodPost, path, nil)); pattern != "" {
			t.Errorf("%s registered without a core: %q", path, pattern)
		}
	}
}

func TestStoragePruningIsOptIn(t *testing.T) {
	for in, want := range map[string]bool{"": false, "0": false, "false": false, "no": false, "garbage": false, "1": true, "true": true, " TRUE ": true, "yes": true, "on": true} {
		if got := storagePruneEnabled(in); got != want {
			t.Errorf("storagePruneEnabled(%q) = %v, want %v", in, got, want)
		}
	}
}
