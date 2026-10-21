//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// The recovery endpoints are public (no session), answer the same for any username, and turn a
// logged code into a working password.
func TestPasswordRecoveryEndpoints(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	var logs bytes.Buffer
	idSvc := &identity.Service{Store: env.Store, Log: slog.New(slog.NewTextHandler(&logs, nil)),
		SessionTTL: time.Hour, IdleTimeout: time.Hour, MaxFailures: 5, LockFor: 15 * time.Minute, RecoveryLogCodes: true}
	h := &api.Handlers{Inv: env.Svc, Identity: idSvc, Log: pgtest.Discard()}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{Queries: db.New(env.Pool)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()

	var admin string
	if err := env.Pool.QueryRow(ctx, "SELECT username FROM users WHERE tenant_id IS NULL AND status = 'active' ORDER BY created_at LIMIT 1").Scan(&admin); err != nil {
		t.Fatal(err)
	}
	post := func(t *testing.T, path string, body any) (int, map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(api.CSRFHeader, "1")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	// Same answer for the admin, a tenant user and an unknown name.
	var answers []string
	for _, name := range []string{"operador", "nobody-here", admin} {
		code, body := post(t, "/api/v1/auth/recovery", map[string]string{"username": name})
		if code != http.StatusAccepted {
			t.Fatalf("request for %q: status %d %v, want 202", name, code, body)
		}
		answers = append(answers, body["message"].(string))
	}
	if answers[0] != answers[1] || answers[1] != answers[2] {
		t.Fatalf("answers differ by account: %q", answers)
	}
	m := regexp.MustCompile(`code=([0-9]{6})`).FindAllStringSubmatch(logs.String(), -1)
	if len(m) != 1 || !strings.Contains(logs.String(), "user="+admin) {
		t.Fatalf("want exactly one code, for %q, in the log: %s", admin, logs.String())
	}
	code := m[0][1]

	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	if status, body := post(t, "/api/v1/auth/recovery/confirm", map[string]string{"username": admin, "code": wrong, "new_password": "New-Password-22"}); status != http.StatusUnauthorized || body["code"] != "invalid_recovery_code" {
		t.Fatalf("wrong code: %d %v, want 401 invalid_recovery_code", status, body)
	}
	if status, body := post(t, "/api/v1/auth/recovery/confirm", map[string]string{"username": admin, "code": code, "new_password": "short"}); status != http.StatusBadRequest {
		t.Fatalf("weak password: %d %v, want 400", status, body)
	}
	if status, body := post(t, "/api/v1/auth/recovery/confirm", map[string]string{"username": admin, "code": code, "new_password": "New-Password-22"}); status != http.StatusNoContent {
		t.Fatalf("right code: %d %v, want 204", status, body)
	}
	if status, body := post(t, "/api/v1/auth/login", map[string]string{"username": admin, "password": "New-Password-22"}); status != http.StatusOK {
		t.Fatalf("login with the recovered password: %d %v", status, body)
	}

	idSvc.RecoveryLogCodes = false
	if status, body := post(t, "/api/v1/auth/recovery", map[string]string{"username": admin}); status != http.StatusNotFound || body["code"] != "recovery_disabled" {
		t.Fatalf("disabled: %d %v, want 404 recovery_disabled", status, body)
	}
}
