package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	mux := buildTLSMux(&authenticator{secret: "agent-secret"}, nil, nil, nil, nil, nil, newTestOps(t))
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
	plain := buildMux("agent-secret", "test", nil, nil, nil, ops, nil, nil, func() {})
	tls := buildTLSMux(&authenticator{secret: "agent-secret"}, nil, nil, nil, nil, nil, ops)
	for _, path := range opsRoutes {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		_, onPlain := plain.Handler(req)
		_, onTLS := tls.Handler(req)
		if onPlain == "" || onTLS == "" {
			t.Errorf("%s: plain=%q tls=%q, want it registered on both", path, onPlain, onTLS)
		}
	}
}

func TestTLSMuxWithoutOpsDoesNotRegisterThem(t *testing.T) {
	mux := buildTLSMux(&authenticator{secret: "agent-secret"}, nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/onvif/video-device", nil)
	if _, pattern := mux.Handler(req); pattern != "" {
		t.Fatalf("video-device registered without an ops handler: %q", pattern)
	}
}
