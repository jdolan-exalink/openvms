package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/health"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func newTestServer(t *testing.T, checks ...health.Check) *httptest.Server {
	t.Helper()
	h := &Handlers{
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Checks:        checks,
		CheckTimeout:  time.Second,
		SchemaVersion: func(context.Context) (int64, error) { return 1, nil },
	}
	router, err := NewRouter(h, h.Log, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts
}

func ok(context.Context) error { return nil }

func TestReadinessReportsEachDependency(t *testing.T) {
	ts := newTestServer(t,
		health.Check{Name: "postgres", Probe: ok},
		health.Check{Name: "nats", Probe: func(context.Context) error { return errors.New("connection refused") }},
	)
	resp, err := http.Get(ts.URL + "/health/ready")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when a dependency is down, got %d", resp.StatusCode)
	}
	var body gen.ReadinessStatus
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != gen.ReadinessStatusStatusDegraded || len(body.Checks) != 2 {
		t.Fatalf("unexpected body: %+v", body)
	}
	if body.Checks[0].Status != gen.DependencyCheckStatusOk || body.Checks[1].Status != gen.DependencyCheckStatusError {
		t.Errorf("checks not reported in order with their status: %+v", body.Checks)
	}
}

func TestReadinessOK(t *testing.T) {
	ts := newTestServer(t, health.Check{Name: "postgres", Probe: ok})
	resp, err := http.Get(ts.URL + "/health/ready")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}

func TestRequestIDIsPropagatedOrCreated(t *testing.T) {
	ts := newTestServer(t)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/health/live", nil)
	req.Header.Set("X-Request-ID", "abc-123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("X-Request-ID"); got != "abc-123" {
		t.Errorf("incoming request id not echoed, got %q", got)
	}

	req.Header.Set("X-Request-ID", "bad value with spaces")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("X-Request-ID"); got == "" || got == "bad value with spaces" {
		t.Errorf("invalid request id should be replaced, got %q", got)
	}
}

func TestOpenAPIServed(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var spec map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		t.Fatal(err)
	}
	if _, ok := spec["paths"].(map[string]any)["/api/v1/system/info"]; !ok {
		t.Error("spec is missing /api/v1/system/info")
	}
}

func TestWebsocketRouteRequiresAuthenticationBeforeUpgrade(t *testing.T) {
	h := &Handlers{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), CheckTimeout: time.Second}
	called := false
	router, err := NewRouter(h, h.Log, Options{
		Queries:  db.New(nil),
		Realtime: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/ws")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || called {
		t.Fatalf("want 401 without invoking the handler, got %d (called=%v)", resp.StatusCode, called)
	}
}
