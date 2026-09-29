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

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func TestReviewEventsEndpoint(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	adapters := inventory.NewAdapters(env.Svc)
	syncer := &events.Syncer{Store: env.Store, Adapters: adapters, Blobs: noopBlobs{}, Log: pgtest.Discard(), Interval: time.Second, Backfill: 24 * time.Hour, Concurrency: 2}
	syncer.SyncAll(ctx)
	evSvc := &events.Service{Store: env.Store, Blobs: noopBlobs{}, Adapters: adapters, Log: pgtest.Discard()}
	page, err := evSvc.ListEvents(ctx, env.Admin, events.Filter{Limit: 2})
	if err != nil || len(page.Items) < 2 {
		t.Fatalf("seed events: %v", err)
	}
	h := &api.Handlers{Inv: env.Svc, Events: evSvc, Log: pgtest.Discard()}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{Queries: db.New(env.Pool)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()
	post := func(token, body string) (int, map[string]any) {
		req, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/api/v1/events/review", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	a, b := page.Items[0].ID, page.Items[1].ID

	if code, out := post(env.AdminToken, `{"ids":["`+a.String()+`","`+b.String()+`"],"reviewed":true}`); code != 200 || len(out["items"].([]any)) != 2 {
		t.Fatalf("admin bulk = %d %v", code, out)
	}
	if code, _ := post(env.AdminToken, `{"ids":[],"reviewed":true}`); code != 400 {
		t.Errorf("empty ids = %d, want 400", code)
	}
	if code, _ := post(env.AdminToken, `{"ids":["`+uuid.NewString()+`"],"reviewed":true}`); code != 404 {
		t.Errorf("unknown id = %d, want 404", code)
	}
	if code, _ := post(env.Demo.Tokens["operador"], `{"ids":["`+a.String()+`"],"reviewed":false}`); code != 403 {
		t.Errorf("operator without events.review = %d, want 403", code)
	}
}
