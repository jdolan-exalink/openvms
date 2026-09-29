package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/platform/config"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func TestGetFeaturesReportsConfiguredFlags(t *testing.T) {
	h := &Handlers{Features: config.Features{PersistentPlayers: true, StreamPrewarming: true}}
	res, err := h.GetFeatures(context.Background(), gen.GetFeaturesRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	body, ok := res.(gen.GetFeatures200JSONResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", res)
	}
	want := gen.Features{PersistentPlayers: true, StreamPrewarming: true}
	if gen.Features(body) != want {
		t.Fatalf("got %+v, want %+v", body, want)
	}
}

func TestFeaturesRouteRequiresAuthentication(t *testing.T) {
	h := &Handlers{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	router, err := NewRouter(h, h.Log, Options{Queries: db.New(nil)})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/features", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without credentials, got %d", rec.Code)
	}
	var e gen.Error
	if err := json.NewDecoder(rec.Body).Decode(&e); err != nil {
		t.Fatal(err)
	}
}
