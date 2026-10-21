package api

import (
	"net/http"
	"testing"
	"time"

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
