package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/agent/provisionlpr"
)

// The queued events hold plates; central collects them with the agent token and nobody else can read
// or acknowledge them, whatever the method.
func TestProvisionEventRoutesNeedTheAgentToken(t *testing.T) {
	q, err := provisionlpr.OpenEventQueue(t.TempDir(), 10, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	receiver := provisionlpr.NewReceiver(provisionlpr.DefaultConfig(), nil)
	receiver.SetEventQueue(q)
	mux := buildMux("agent-secret", "test", nil, nil, nil, nil, receiver, func(*http.Request) error { return nil }, func() {})

	do := func(method, path, body, token string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/provision-lpr/events", ""},
		{http.MethodGet, "/v1/provision-lpr/events?after=0", ""},
		{http.MethodPost, "/v1/provision-lpr/events/ack", `{"through":1}`},
		{http.MethodPost, "/v1/provision-lpr/events", ""},
		{http.MethodGet, "/v1/provision-lpr/events/ack", ""},
	} {
		for _, token := range []string{"", "wrong"} {
			if got := do(c.method, c.path, c.body, token); got != http.StatusUnauthorized {
				t.Errorf("%s %s with token %q = %d, want 401", c.method, c.path, token, got)
			}
		}
	}
	if got := do(http.MethodGet, "/v1/provision-lpr/events", "", "agent-secret"); got != http.StatusOK {
		t.Fatalf("list with the token = %d, want 200", got)
	}
	if got := do(http.MethodPost, "/v1/provision-lpr/events/ack", `{"through":0}`, "agent-secret"); got != http.StatusOK {
		t.Fatalf("ack with the token = %d, want 200", got)
	}
}
