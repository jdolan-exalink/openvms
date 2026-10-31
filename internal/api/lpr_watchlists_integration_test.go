//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type lprAPI struct {
	t      *testing.T
	env    *demofix.Env
	ts     *httptest.Server
	tenant uuid.UUID
}

type lprResp struct {
	code int
	raw  string
}

func (r lprResp) json() map[string]any {
	var m map[string]any
	_ = json.Unmarshal([]byte(r.raw), &m)
	return m
}

func newLprAPI(t *testing.T) *lprAPI {
	t.Helper()
	env := demofix.Setup(t)
	adapters := inventory.NewAdapters(env.Svc)
	h := &api.Handlers{
		Store: env.Store, Inv: env.Svc, Log: pgtest.Discard(),
		Events: &events.Service{Store: env.Store, Adapters: adapters, Log: pgtest.Discard()},
		Alarms: &alarms.Service{Store: env.Store, Log: pgtest.Discard()},
		Rules:  rules.NewService(env.Store, nil, pgtest.Discard()),
	}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{Queries: db.New(env.Pool)})
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return &lprAPI{t: t, env: env, ts: ts, tenant: env.Demo.TenantID}
}

func (f *lprAPI) user(tenant uuid.UUID, username string, perms ...authz.Permission) string {
	f.t.Helper()
	ctx := context.Background()
	actor, token, err := bootstrap.TenantUser(ctx, f.env.Store, tenant, username+"-"+uuid.NewString()[:6])
	if err != nil {
		f.t.Fatalf("create user: %v", err)
	}
	for _, p := range perms {
		if _, err := f.env.Svc.CreateGrant(ctx, f.env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: actor.UserID, Permission: p, Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenant,
		}); err != nil {
			f.t.Fatalf("grant %s: %v", p, err)
		}
	}
	return token
}

func (f *lprAPI) js(token, method, path, body string) lprResp {
	f.t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, f.ts.URL+path, bodyReader)
	if err != nil {
		f.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return lprResp{code: res.StatusCode, raw: string(b)}
}

func TestLprWatchlistsAPI(t *testing.T) {
	f := newLprAPI(t)
	mgr := f.user(f.tenant, "lpr-mgr", authz.LPRManage, authz.LPRView)
	viewer := f.user(f.tenant, "lpr-viewer", authz.LPRView)
	blind := f.user(f.tenant, "blind-user", authz.EventsView)

	// 1. Without lpr.view: 403
	if r := f.js(blind, "GET", "/api/v1/lpr/watchlists", ""); r.code != http.StatusForbidden {
		t.Errorf("list without lpr.view = %d, want 403", r.code)
	}

	// 2. Viewer cannot create: 403
	if r := f.js(viewer, "POST", "/api/v1/lpr/watchlists", `{"name":"VIP"}`); r.code != http.StatusForbidden {
		t.Errorf("viewer create = %d, want 403", r.code)
	}

	// 3. Manager can create: 201
	r := f.js(mgr, "POST", "/api/v1/lpr/watchlists", `{"name":"VIP","description":"Vehículos VIP","color":"#10b981"}`)
	if r.code != http.StatusCreated {
		t.Fatalf("create = %d %s", r.code, r.raw)
	}
	wl := r.json()
	id := wl["id"].(string)
	if wl["name"] != "VIP" || wl["color"] != "#10b981" {
		t.Fatalf("created watchlist mismatch: %v", wl)
	}

	// 4. Duplicate name: 409
	if r := f.js(mgr, "POST", "/api/v1/lpr/watchlists", `{"name":"vip"}`); r.code != http.StatusConflict {
		t.Errorf("duplicate name = %d, want 409", r.code)
	}

	// 5. Viewer can list: 200
	if r := f.js(viewer, "GET", "/api/v1/lpr/watchlists", ""); r.code != http.StatusOK || len(r.json()["items"].([]any)) != 1 {
		t.Errorf("viewer list = %d %s", r.code, r.raw)
	}

	// 6. Viewer can get: 200
	if r := f.js(viewer, "GET", "/api/v1/lpr/watchlists/"+id, ""); r.code != http.StatusOK || r.json()["name"] != "VIP" {
		t.Errorf("viewer get = %d %s", r.code, r.raw)
	}

	// 7. Manager can update: 200
	if r := f.js(mgr, "PATCH", "/api/v1/lpr/watchlists/"+id, `{"name":"VIP Executive","color":"#3b82f6"}`); r.code != http.StatusOK ||
		r.json()["name"] != "VIP Executive" || r.json()["color"] != "#3b82f6" {
		t.Errorf("patch = %d %s", r.code, r.raw)
	}

	// 8. Add item: 201
	rItem := f.js(mgr, "POST", "/api/v1/lpr/watchlists/"+id+"/items", `{"plate":"AB123CD","vehicle_brand":"BMW","vehicle_model":"X5","notes":"CEO"}`)
	if rItem.code != http.StatusCreated {
		t.Fatalf("add item = %d %s", rItem.code, rItem.raw)
	}
	item := rItem.json()
	itemID := item["id"].(string)
	if item["plate"] != "AB123CD" || item["vehicle_brand"] != "BMW" {
		t.Fatalf("item mismatch: %v", item)
	}

	// 9. List items: 200
	if r := f.js(viewer, "GET", "/api/v1/lpr/watchlists/"+id+"/items", ""); r.code != http.StatusOK || len(r.json()["items"].([]any)) != 1 {
		t.Errorf("list items = %d %s", r.code, r.raw)
	}

	// 10. Delete item: 204
	if r := f.js(mgr, "DELETE", "/api/v1/lpr/watchlists/"+id+"/items/"+itemID, ""); r.code != http.StatusNoContent {
		t.Errorf("delete item = %d, want 204", r.code)
	}

	// 11. Viewer cannot delete watchlist: 403
	if r := f.js(viewer, "DELETE", "/api/v1/lpr/watchlists/"+id, ""); r.code != http.StatusForbidden {
		t.Errorf("viewer delete = %d, want 403", r.code)
	}

	// 12. Manager can delete watchlist: 204
	if r := f.js(mgr, "DELETE", "/api/v1/lpr/watchlists/"+id, ""); r.code != http.StatusNoContent {
		t.Errorf("delete watchlist = %d, want 204", r.code)
	}

	// 13. Get after delete: 404
	if r := f.js(mgr, "GET", "/api/v1/lpr/watchlists/"+id, ""); r.code != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", r.code)
	}
}
