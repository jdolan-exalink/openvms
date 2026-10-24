package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/agent/provisionlpr"
)

func TestParseCaptureMode(t *testing.T) {
	for in, want := range map[string]captureMode{"": captureUnparsed, " ALL ": captureEverything, "off": captureOff, "unparsed": captureUnparsed} {
		got, err := parseCaptureMode(in)
		if err != nil || got != want {
			t.Fatalf("parseCaptureMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := parseCaptureMode("everything"); err == nil || !strings.Contains(err.Error(), "OPENVMS_AGENT_PROVISION_CAPTURE") {
		t.Fatalf("a typo was accepted or the error does not name the variable: %v", err)
	}
}

func TestCaptureDirLivesInsideTheStateDirectory(t *testing.T) {
	if got := captureDir("/var/lib/openvms-agent"); got != "/var/lib/openvms-agent/provision-captures" {
		t.Fatalf("captureDir = %q", got)
	}
}

func TestProvisionCapturesNeedTheAgentToken(t *testing.T) {
	store, err := provisionlpr.NewCaptureStore(filepath.Join(t.TempDir(), "c"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	receiver := provisionlpr.NewReceiver(provisionlpr.DefaultConfig(), nil)
	receiver.SetCaptures(store, false)
	mux := buildMux("agent-secret", "test", nil, nil, nil, nil, receiver, func(*http.Request) error { return nil }, func() {})

	get := func(path, token string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, path := range []string{"/v1/provision-lpr/captures", "/v1/provision-lpr/captures/20261023T180005123456789Z-aaaaaaaaaaaa", "/v1/provision-lpr/captures/20261023T180005123456789Z-aaaaaaaaaaaa/body"} {
		if got := get(path, ""); got != http.StatusUnauthorized {
			t.Fatalf("GET %s without a token = %d, want 401", path, got)
		}
		if got := get(path, "wrong"); got != http.StatusUnauthorized {
			t.Fatalf("GET %s with a wrong token = %d, want 401", path, got)
		}
	}
	if got := get("/v1/provision-lpr/captures", "agent-secret"); got != http.StatusOK {
		t.Fatalf("list with the token = %d, want 200", got)
	}
}

func TestCaptureAllAcceptsPostsOnUnknownPathsWithoutBreakingKnownRoutes(t *testing.T) {
	store, _ := provisionlpr.NewCaptureStore(filepath.Join(t.TempDir(), "c"), 0, 0)
	receiver := provisionlpr.NewReceiver(provisionlpr.DefaultConfig(), nil)
	receiver.SetCaptures(store, true)
	mux := buildMux("agent-secret", "test", nil, nil, nil, nil, receiver, func(*http.Request) error { return nil }, func() {})
	mux.Handle("/", postsAnywhere(receiver)) // what main does in capture-all mode

	do := func(method, path string) int {
		req := httptest.NewRequest(method, path, strings.NewReader("vendor bytes"))
		req.RemoteAddr = "10.0.15.107:5000"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := do(http.MethodPost, "/Some/Vendor/Path"); got != http.StatusOK {
		t.Fatalf("POST on an unknown path = %d, want 200", got)
	}
	if list, _ := store.List(0); len(list) != 1 || list[0].Path != "/Some/Vendor/Path" {
		t.Fatalf("captures = %+v, want the unknown-path request", list)
	}
	if got := do(http.MethodGet, "/Some/Vendor/Path"); got != http.StatusNotFound {
		t.Fatalf("GET on an unknown path = %d, want 404", got)
	}
	// Routes that exist keep their own rules.
	if got := do(http.MethodPost, "/v1/update"); got != http.StatusUnauthorized {
		t.Fatalf("POST /v1/update = %d, want 401: the catch-all must not take it over", got)
	}
	if got := do(http.MethodPost, "/SendKeepalive"); got != http.StatusOK {
		t.Fatalf("POST /SendKeepalive = %d, want 200", got)
	}
}
