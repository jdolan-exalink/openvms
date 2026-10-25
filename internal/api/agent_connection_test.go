package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/provision"
)

// Every way a connection attempt can fail has its own answer, so the operator knows what to fix.
func TestConnectAgentErrorsMapToActionableAnswers(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"unreachable":  {provision.ErrAgentConnectUnreachable, http.StatusBadGateway, "agent_unreachable"},
		"bad token":    {provision.ErrAgentConnectUnauthorized, http.StatusUnprocessableEntity, "agent_token_rejected"},
		"missing read": {provision.ErrAgentConnectForbidden, http.StatusUnprocessableEntity, "agent_token_scope"},
		"not an agent": {provision.ErrAgentConnectNotAnAgent, http.StatusUnprocessableEntity, "not_an_agent"},
		"exists":       {provision.ErrAgentAlreadyProvisioned, http.StatusConflict, "conflict"},
	} {
		status, code, msg := statusFor(tc.err)
		if status != tc.status || code != tc.code || msg == "" {
			t.Errorf("%s: %d %q %q, want %d %q", name, status, code, msg, tc.status, tc.code)
		}
	}
}

func TestAgentConnectionRoutesNeedAnActor(t *testing.T) {
	router := connectionTestRouter(t)
	for name, tc := range map[string]struct{ method, body string }{
		"read":       {http.MethodGet, ""},
		"connect":    {http.MethodPut, `{"url":"https://a.example.com","token":"x"}`},
		"disconnect": {http.MethodDelete, ""},
	} {
		req := httptest.NewRequest(tc.method, "/api/v1/servers/11111111-1111-1111-1111-111111111111/agent/connection", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d body %s; want 401", name, rec.Code, rec.Body)
		}
	}
}

// The body carries the agent token: it is bounded, and the answer never repeats it.
func TestAgentConnectionBodyIsBounded(t *testing.T) {
	router := connectionTestRouter(t)
	huge := `{"url":"https://a.example.com","token":"` + strings.Repeat("x", 300<<10) + `"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/servers/11111111-1111-1111-1111-111111111111/agent/connection", strings.NewReader(huge))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d; want a refusal of the oversized body", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "xxxxxxxx") {
		t.Fatal("the response repeats the token")
	}
}

func connectionTestRouter(t *testing.T) http.Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, err := NewRouter(&Handlers{Log: log}, log, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return router
}
