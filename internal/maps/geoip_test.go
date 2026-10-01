package maps

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseCenter(t *testing.T) {
	c, ok := ParseCenter("-34.6,-58.4")
	if !ok || c.Lat != -34.6 || c.Lng != -58.4 {
		t.Fatalf("unexpected center: %+v ok=%v", c, ok)
	}
	c, ok = ParseCenter("  -14.2 , -51.9 ")
	if !ok || c.Lat != -14.2 || c.Lng != -51.9 {
		t.Fatalf("unexpected center: %+v ok=%v", c, ok)
	}
	for _, raw := range []string{"", "nope", "1.0", "1.0,2.0,3.0", "a,b"} {
		if _, ok := ParseCenter(raw); ok {
			t.Fatalf("ParseCenter(%q) should not parse", raw)
		}
	}
}

func TestDetectCenter(t *testing.T) {
	ctx := context.Background()
	client := &http.Client{Timeout: 2 * time.Second}

	// A float/number latitude (freeipapi shape) resolves to the server's position.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ipAddress":"203.0.113.7","latitude":-31.42,"longitude":-64.18,"countryCode":"AR"}`))
	}))
	defer srv.Close()
	c, err := detectCenter(ctx, client, srv.URL)
	if err != nil {
		t.Fatalf("detectCenter: %v", err)
	}
	if c.Lat != -31.42 || c.Lng != -64.18 {
		t.Fatalf("unexpected center: %+v", c)
	}

	// Some services report coordinates as strings.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"latitude":"-12.5","longitude":"-77.0"}`))
	}))
	defer srv2.Close()
	if c, err = detectCenter(ctx, client, srv2.URL); err != nil || c.Lat != -12.5 || c.Lng != -77.0 {
		t.Fatalf("string coordinates: %+v err=%v", c, err)
	}

	// Any failure — HTTP error, bad JSON, unreachable — is an error for the caller to
	// fall back from.
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv3.Close()
	if _, err := detectCenter(ctx, client, srv3.URL); err == nil {
		t.Fatal("expected an error on 503")
	}
	srv4 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<html>not json</html>`))
	}))
	defer srv4.Close()
	if _, err := detectCenter(ctx, client, srv4.URL); err == nil {
		t.Fatal("expected an error on malformed JSON")
	}
	if _, err := detectCenter(ctx, client, "http://127.0.0.1:1"); err == nil {
		t.Fatal("expected an error on an unreachable service")
	}
	if !errors.Is(context.DeadlineExceeded, context.DeadlineExceeded) {
		t.Fatal("unreachable")
	}
}

func TestDetectServerCenterPrecedence(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 1. Explicit override wins over everything.
	t.Setenv("OPENVMS_MAPS_CENTER", "-34.6,-58.4")
	if c := DetectServerCenter(context.Background(), log); c.Lat != -34.6 || c.Lng != -58.4 {
		t.Fatalf("env override ignored: %+v", c)
	}

	// 2. Without a reachable geolocation service, the Latin America default stands.
	t.Setenv("OPENVMS_MAPS_CENTER", "")
	def := DefaultConfig().DefaultCenter
	if c := DetectServerCenter(context.Background(), log); c.Lat != def.Lat || c.Lng != def.Lng {
		t.Fatalf("expected the default center on failure, got %+v", c)
	}
}
