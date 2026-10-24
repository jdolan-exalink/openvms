package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/agent/provisionlpr"
)

func registryMux(t *testing.T) (*http.ServeMux, *provisionlpr.Registry) {
	t.Helper()
	reg, err := provisionlpr.LoadRegistry(filepath.Join(t.TempDir(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	receiver := provisionlpr.NewReceiver(provisionlpr.DefaultConfig(), nil)
	receiver.SetRegistry(reg)
	return buildMux("agent-secret", "test", nil, nil, nil, nil, receiver, func(*http.Request) error { return nil }, func() {}), reg
}

func call(mux http.Handler, method, path, token, body, remote string) int {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if remote != "" {
		req.RemoteAddr = remote
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code
}

func TestProvisionStatusAndRegistryRoutesNeedTheAgentToken(t *testing.T) {
	mux, _ := registryMux(t)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/provision-lpr/status", ""},
		{http.MethodGet, "/v1/provision-lpr/registry", ""},
		{http.MethodPost, "/v1/provision-lpr/registry/cameras", `{"camera_id":"cam-a","ip":"10.0.15.107"}`},
		{http.MethodPut, "/v1/provision-lpr/registry/enforce", `{"enforce":true}`},
		{http.MethodDelete, "/v1/provision-lpr/registry/cameras/cam-a", ""},
	} {
		if got := call(mux, c.method, c.path, "", c.body, ""); got != http.StatusUnauthorized {
			t.Fatalf("%s %s without a token = %d, want 401", c.method, c.path, got)
		}
		if got := call(mux, c.method, c.path, "wrong", c.body, ""); got != http.StatusUnauthorized {
			t.Fatalf("%s %s with a wrong token = %d, want 401", c.method, c.path, got)
		}
	}
	if got := call(mux, http.MethodGet, "/v1/provision-lpr/status", "agent-secret", "", ""); got != http.StatusOK {
		t.Fatalf("status with the token = %d, want 200", got)
	}
	if got := call(mux, http.MethodPost, "/v1/provision-lpr/registry/cameras", "agent-secret", `{"camera_id":"cam-a","ip":"10.0.15.107"}`, ""); got != http.StatusOK {
		t.Fatalf("register with the token = %d, want 200", got)
	}
}

func TestCameraRoutesStayOpenToCamerasButFollowTheRegistry(t *testing.T) {
	mux, reg := registryMux(t)
	_ = reg.Upsert(provisionlpr.RegisteredCamera{CameraID: "cam-a", IP: "10.0.15.107"})
	// Cameras carry no agent token: before enforcement anyone is still accepted.
	if got := call(mux, http.MethodPost, "/SendKeepalive", "", "", "10.0.15.200:5000"); got != http.StatusOK {
		t.Fatalf("keepalive from an unregistered camera before enforcement = %d, want 200", got)
	}
	_ = reg.SetEnforce(true)
	if got := call(mux, http.MethodPost, "/SendKeepalive", "", "", "10.0.15.200:5000"); got != http.StatusForbidden {
		t.Fatalf("keepalive from an unregistered camera under enforcement = %d, want 403", got)
	}
	if got := call(mux, http.MethodPost, "/SendKeepalive", "", "", "10.0.15.107:5000"); got != http.StatusOK {
		t.Fatalf("keepalive from the registered camera under enforcement = %d, want 200", got)
	}
}
