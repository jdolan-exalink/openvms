package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent/camproxy"
	"github.com/jdolan-exalink/openvms/internal/agent/tokens"
)

func cameraWebMux(t *testing.T) (*http.ServeMux, *tokens.Store) {
	t.Helper()
	store := tokens.Open(filepath.Join(t.TempDir(), "agent.tokens.json"))
	auth := &authenticator{secret: "legacy-secret", tokens: store}
	mux := http.NewServeMux()
	mountCameraWeb(mux, auth, camproxy.New(camproxy.Config{BlockedPorts: ownListenPorts("0.0.0.0:7419", "")}))
	return mux, store
}

func callCameraWeb(mux *http.ServeMux, bearer, target string) int {
	req := httptest.NewRequest(http.MethodGet, camproxy.Path+"anything", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set(camproxy.HeaderTarget, target)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code
}

func TestCameraWebNeedsTheControlScope(t *testing.T) {
	mux, store := cameraWebMux(t)
	mk := func(scope tokens.Scope) string {
		secret, _, err := store.Create(string(scope), []tokens.Scope{scope}, 0, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	read, control := mk(tokens.ScopeRead), mk(tokens.ScopeControl)
	const blocked = "http://127.0.0.1:80" // refused by the proxy itself, so no upstream is needed
	if got := callCameraWeb(mux, "", blocked); got != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", got)
	}
	if got := callCameraWeb(mux, "wrong", blocked); got != http.StatusUnauthorized {
		t.Errorf("wrong token = %d, want 401", got)
	}
	if got := callCameraWeb(mux, read, blocked); got != http.StatusForbidden {
		t.Errorf("read token = %d, want 403", got)
	}
	if got := callCameraWeb(mux, control, blocked); got != http.StatusForbidden {
		t.Errorf("control token reaches the proxy, which refuses loopback with 403; got %d", got)
	}
	if got := callCameraWeb(mux, "legacy-secret", "http://10.0.0.5:7419"); got != http.StatusForbidden {
		t.Errorf("the agent's own port = %d, want 403", got)
	}
	if got := callCameraWeb(mux, "legacy-secret", "http://camera.lan:80"); got != http.StatusBadRequest {
		t.Errorf("hostname = %d, want 400", got)
	}
}

func TestOwnListenPorts(t *testing.T) {
	got := ownListenPorts("0.0.0.0:7419", "10.0.0.2:9000")
	want := map[int]bool{7419: true, 7443: true, 9000: true}
	for _, p := range got {
		delete(want, p)
	}
	if len(want) != 0 {
		t.Fatalf("ports = %v, missing %v", got, want)
	}
}
