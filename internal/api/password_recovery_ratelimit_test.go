package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
)

// The public recovery endpoints share one per-IP budget, so a client cannot flood the API log
// with codes or the audit log with failed confirmations.
func TestPasswordRecoveryRateLimitSharesOneBudgetPerIP(t *testing.T) {
	now := time.Unix(1_000, 0)
	l := httpx.NewRateLimiter(2, time.Minute, func() time.Time { return now })
	reached := 0
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached++; w.WriteHeader(http.StatusOK) })
	h := httpx.ClientAddr(false)(limitPasswordRecovery(l)(final))

	if rec := post(h, "/api/v1/auth/recovery", "198.51.100.7:4000", ""); rec.Code != http.StatusOK {
		t.Fatalf("first request: %d", rec.Code)
	}
	if rec := post(h, "/api/v1/auth/recovery/confirm", "198.51.100.7:4001", ""); rec.Code != http.StatusOK {
		t.Fatalf("second request: %d", rec.Code)
	}
	rec := post(h, "/api/v1/auth/recovery/confirm", "198.51.100.7:4002", "")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("third request from the same IP: %d (Retry-After %q), want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := post(h, "/api/v1/auth/recovery", "203.0.113.9:4000", ""); rec.Code != http.StatusOK {
		t.Fatalf("another IP keeps its own budget: %d", rec.Code)
	}
	if rec := post(h, "/api/v1/auth/login", "198.51.100.7:4003", ""); rec.Code != http.StatusOK {
		t.Fatalf("other paths are not limited here: %d", rec.Code)
	}
	if reached != 4 {
		t.Fatalf("handler reached %d times, want 4", reached)
	}
}

// Oversized bodies are cut before decoding: the endpoints are anonymous.
func TestPasswordRecoveryBodyIsBounded(t *testing.T) {
	l := httpx.NewRateLimiter(10, time.Minute, nil)
	var readErr error
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	h := httpx.ClientAddr(false)(limitPasswordRecovery(l)(final))
	req := httptest.NewRequest(http.MethodPost, passwordRecoveryConfirmPath, strings.NewReader(strings.Repeat("x", passwordRecoveryMaxBodyBytes+1)))
	req.RemoteAddr = "198.51.100.7:4000"
	h.ServeHTTP(httptest.NewRecorder(), req)
	if readErr == nil {
		t.Fatal("a body over the limit must fail to read")
	}
}

func TestPasswordRecoveryHandlersRejectOutOfContractInput(t *testing.T) {
	h := &Handlers{} // validation happens before the identity service is reached
	ctx := context.Background()
	long := strings.Repeat("a", 201)
	for name, body := range map[string]gen.PasswordRecoveryConfirm{
		"long username":   {Username: long, Code: "123456", NewPassword: "New-Password-22"},
		"empty username":  {Username: " ", Code: "123456", NewPassword: "New-Password-22"},
		"letters in code": {Username: "admin", Code: "12ab56", NewPassword: "New-Password-22"},
		"short code":      {Username: "admin", Code: "12345", NewPassword: "New-Password-22"},
		"long password":   {Username: "admin", Code: "123456", NewPassword: strings.Repeat("p", 1025)},
	} {
		resp, err := h.ConfirmPasswordRecovery(ctx, gen.ConfirmPasswordRecoveryRequestObject{Body: &body})
		if _, ok := resp.(gen.ConfirmPasswordRecovery400JSONResponse); err != nil || !ok {
			t.Errorf("%s: %T %v, want 400", name, resp, err)
		}
	}
	resp, err := h.RequestPasswordRecovery(ctx, gen.RequestPasswordRecoveryRequestObject{Body: &gen.PasswordRecoveryRequest{Username: long}})
	if _, ok := resp.(gen.RequestPasswordRecovery400JSONResponse); err != nil || !ok {
		t.Errorf("long username request: %T %v, want 400", resp, err)
	}
}
