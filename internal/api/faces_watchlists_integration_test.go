//go:build integration

package api_test

import (
	"net/http"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

func TestFacesAPIWatchlists(t *testing.T) {
	f := newFaceAPI(t)
	mgr := f.user(f.tenant, "mgr", authz.FacesManage, authz.FacesView)
	viewer := f.user(f.tenant, "viewer", authz.FacesView)
	blind := f.user(f.tenant, "blind", authz.EventsView)

	person := f.js(mgr, "POST", "/api/v1/faces/persons", `{"display_name":"Ana"}`).json()["id"].(string)

	if r := f.js(blind, "GET", "/api/v1/faces/watchlists", ""); r.code != http.StatusForbidden {
		t.Errorf("list without faces.view = %d, want 403", r.code)
	}
	if r := f.js(viewer, "POST", "/api/v1/faces/watchlists", `{"name":"VIP"}`); r.code != http.StatusForbidden {
		t.Errorf("viewer create = %d, want 403", r.code)
	}
	r := f.js(mgr, "POST", "/api/v1/faces/watchlists", `{"name":"VIP","description":"front desk","person_ids":["`+person+`"]}`)
	if r.code != http.StatusCreated {
		t.Fatalf("create = %d %s", r.code, r.raw)
	}
	wl := r.json()
	id := wl["id"].(string)
	if ids, _ := wl["person_ids"].([]any); len(ids) != 1 || ids[0] != person {
		t.Fatalf("created watchlist = %v", wl)
	}
	if r := f.js(mgr, "POST", "/api/v1/faces/watchlists", `{"name":"vip"}`); r.code != http.StatusConflict {
		t.Errorf("duplicate name = %d, want 409", r.code)
	}
	if r := f.js(mgr, "POST", "/api/v1/faces/watchlists", `{"name":"Ghost","person_ids":["00000000-0000-4000-8000-000000000001"]}`); r.code != http.StatusBadRequest {
		t.Errorf("unknown person = %d, want 400", r.code)
	}
	if r := f.js(viewer, "GET", "/api/v1/faces/watchlists", ""); r.code != http.StatusOK || len(r.json()["items"].([]any)) != 1 {
		t.Errorf("viewer list = %d %s", r.code, r.raw)
	}
	if r := f.js(viewer, "GET", "/api/v1/faces/watchlists/"+id, ""); r.code != http.StatusOK || r.json()["name"] != "VIP" {
		t.Errorf("viewer get = %d %s", r.code, r.raw)
	}
	if r := f.js(mgr, "PATCH", "/api/v1/faces/watchlists/"+id, `{"name":"VIP guests","person_ids":[]}`); r.code != http.StatusOK ||
		r.json()["name"] != "VIP guests" || r.json()["description"] != "front desk" || len(r.json()["person_ids"].([]any)) != 0 {
		t.Errorf("patch = %d %s", r.code, r.raw)
	}
	if r := f.js(viewer, "DELETE", "/api/v1/faces/watchlists/"+id, ""); r.code != http.StatusForbidden {
		t.Errorf("viewer delete = %d, want 403", r.code)
	}
	if r := f.js(mgr, "DELETE", "/api/v1/faces/watchlists/"+id, ""); r.code != http.StatusNoContent {
		t.Errorf("delete = %d, want 204", r.code)
	}
	if r := f.js(mgr, "GET", "/api/v1/faces/watchlists/"+id, ""); r.code != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", r.code)
	}
}
