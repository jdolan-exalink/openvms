package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
)

func enrollLimitHandler(l *httpx.RateLimiter, trustForwarded bool) (http.Handler, *int) {
	reached := 0
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached++; w.WriteHeader(http.StatusOK) })
	return httpx.ClientAddr(trustForwarded)(limitAgentEnroll(l)(final)), &reached
}

func post(h http.Handler, path, remote, xff string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	req.RemoteAddr = remote
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestEnrollRateLimitIsPerClientIPAndAnswers429(t *testing.T) {
	now := time.Unix(1_000, 0)
	l := httpx.NewRateLimiter(2, time.Minute, func() time.Time { return now })
	h, reached := enrollLimitHandler(l, false)
	for i := range 2 {
		if rec := post(h, "/api/v1/agent/enroll", "198.51.100.7:4000", ""); rec.Code != http.StatusOK {
			t.Fatalf("request %d: %d", i+1, rec.Code)
		}
	}
	rec := post(h, "/api/v1/agent/enroll", "198.51.100.7:4001", "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request: %d, want 429", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Fatal("429 without Retry-After")
	}
	if !strings.Contains(rec.Body.String(), "rate_limited") {
		t.Fatalf("429 body = %s", rec.Body.String())
	}
	if *reached != 2 {
		t.Fatalf("the handler ran %d times, want 2 (the refused request must not reach it)", *reached)
	}
	// Another client is unaffected.
	if rec := post(h, "/api/v1/agent/enroll", "198.51.100.8:4000", ""); rec.Code != http.StatusOK {
		t.Fatalf("another client: %d", rec.Code)
	}
}

func TestEnrollRateLimitOnlyCoversTheEnrollEndpoint(t *testing.T) {
	l := httpx.NewRateLimiter(1, time.Hour, time.Now)
	h, _ := enrollLimitHandler(l, false)
	for range 5 {
		if rec := post(h, "/api/v1/servers", "198.51.100.7:4000", ""); rec.Code != http.StatusOK {
			t.Fatalf("an unrelated route was limited: %d", rec.Code)
		}
	}
	// GET on the enroll path is not the endpoint either.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/enroll", nil)
	req.RemoteAddr = "198.51.100.7:4000"
	for range 3 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET limited: %d", rec.Code)
		}
	}
}

// Behind the proxy the key is the address the proxy appended, as for the audit log; a client
// cannot dodge the limit by sending its own X-Forwarded-For prefix.
func TestEnrollRateLimitFollowsTheProxyConvention(t *testing.T) {
	l := httpx.NewRateLimiter(1, time.Hour, time.Now)
	h, _ := enrollLimitHandler(l, true)
	if rec := post(h, "/api/v1/agent/enroll", "10.0.0.1:1", "1.1.1.1, 203.0.113.9"); rec.Code != http.StatusOK {
		t.Fatalf("first: %d", rec.Code)
	}
	if rec := post(h, "/api/v1/agent/enroll", "10.0.0.1:1", "2.2.2.2, 203.0.113.9"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("same real client with a different spoofed prefix: %d, want 429", rec.Code)
	}
	if rec := post(h, "/api/v1/agent/enroll", "10.0.0.1:1", "1.1.1.1, 203.0.113.10"); rec.Code != http.StatusOK {
		t.Fatalf("a different real client: %d", rec.Code)
	}
}
