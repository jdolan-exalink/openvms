//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// noopBlobs is a Blobs implementation that never actually stores anything; the tests below
// never fetch a thumbnail, only ListEvents query binding and response mapping.
type noopBlobs struct{}

func (noopBlobs) Put(context.Context, string, []byte, string) error   { return nil }
func (noopBlobs) Get(context.Context, string) ([]byte, string, error) { return nil, "", nil }

// TestEventsAPISnapshotAndPreviewFields covers M3-5b item 4: has_snapshot/has_preview query
// binding (events_handlers.go ListEvents) and toEvent's response mapping, at the HTTP layer.
func TestEventsAPISnapshotAndPreviewFields(t *testing.T) {
	env := demofix.Setup(t)
	adapters := inventory.NewAdapters(env.Svc)
	syncer := &events.Syncer{
		Store: env.Store, Adapters: adapters, Blobs: noopBlobs{}, Log: pgtest.Discard(),
		Interval: time.Second, Backfill: 24 * time.Hour, Concurrency: 2,
	}
	syncer.SyncAll(context.Background())
	evSvc := &events.Service{Store: env.Store, Blobs: noopBlobs{}, Adapters: adapters, Log: pgtest.Discard()}

	h := &api.Handlers{Inv: env.Svc, Events: evSvc, Log: pgtest.Discard()}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{Queries: db.New(env.Pool)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()

	do := func(t *testing.T, path string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequestWithContext(context.Background(), "GET", ts.URL+path, strings.NewReader(""))
		req.Header.Set("Authorization", "Bearer "+env.AdminToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, out
	}

	t.Run("toEvent maps has_snapshot and has_preview as booleans", func(t *testing.T) {
		code, body := do(t, "/api/v1/events?limit=500")
		if code != http.StatusOK {
			t.Fatalf("list events: %d %v", code, body)
		}
		items, _ := body["items"].([]any)
		if len(items) == 0 {
			t.Fatal("no events returned")
		}
		for _, it := range items {
			m := it.(map[string]any)
			if _, ok := m["has_snapshot"].(bool); !ok {
				t.Fatalf("event %v: has_snapshot is not a bool: %v", m["id"], m["has_snapshot"])
			}
			if _, ok := m["has_preview"].(bool); !ok {
				t.Fatalf("event %v: has_preview is not a bool: %v", m["id"], m["has_preview"])
			}
		}
	})

	t.Run("has_snapshot query param binds to the filter", func(t *testing.T) {
		code, body := do(t, "/api/v1/events?has_snapshot=true&limit=500")
		if code != http.StatusOK {
			t.Fatalf("list events: %d %v", code, body)
		}
		items, _ := body["items"].([]any)
		if len(items) == 0 {
			t.Fatal("has_snapshot=true matched no events")
		}
		for _, it := range items {
			m := it.(map[string]any)
			if m["has_snapshot"] != true {
				t.Errorf("event %v: has_snapshot=%v, want true", m["id"], m["has_snapshot"])
			}
		}

		code, body = do(t, "/api/v1/events?has_snapshot=false&limit=500")
		if code != http.StatusOK {
			t.Fatalf("list events: %d %v", code, body)
		}
		items, _ = body["items"].([]any)
		if len(items) == 0 {
			t.Fatal("has_snapshot=false matched no events")
		}
		for _, it := range items {
			m := it.(map[string]any)
			if m["has_snapshot"] != false {
				t.Errorf("event %v: has_snapshot=%v, want false", m["id"], m["has_snapshot"])
			}
		}
	})

	t.Run("has_preview query param binds to the filter", func(t *testing.T) {
		code, body := do(t, "/api/v1/events?has_preview=false&limit=500")
		if code != http.StatusOK {
			t.Fatalf("list events: %d %v", code, body)
		}
		items, _ := body["items"].([]any)
		// No ingestion path populates preview_key yet (M3-5 evidence), so every event today
		// has has_preview=false and has_preview=true must match none.
		if len(items) == 0 {
			t.Fatal("has_preview=false matched no events")
		}
		for _, it := range items {
			m := it.(map[string]any)
			if m["has_preview"] != false {
				t.Errorf("event %v: has_preview=%v, want false", m["id"], m["has_preview"])
			}
		}

		code, body = do(t, "/api/v1/events?has_preview=true&limit=500")
		if code != http.StatusOK {
			t.Fatalf("list events: %d %v", code, body)
		}
		items, _ = body["items"].([]any)
		if len(items) != 0 {
			t.Errorf("has_preview=true matched %d events, want 0 (no preview-copy job exists yet)", len(items))
		}
	})
}
