//go:build integration

package api_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
)

// foreignTenant is a second tenant with a site, a face-capable server and a camera.
type foreignTenant struct {
	id, site, server, camera uuid.UUID
}

func (f *faceAPI) foreignTenant() foreignTenant {
	f.t.Helper()
	w := foreignTenant{site: uuid.New(), server: uuid.New(), camera: uuid.New()}
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		ctx := context.Background()
		if err := tx.QueryRow(ctx, `INSERT INTO tenants (slug, name) VALUES ($1, 'foreign') RETURNING id`, "foreign-"+uuid.NewString()[:6]).Scan(&w.id); err != nil {
			return err
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, 'Foreign site')`, []any{w.site, w.id}},
			{`INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url, capabilities)
			  VALUES ($1, $2, $3, 'foreign-server', 'http://example.invalid', '{"face_recognition": true}'::jsonb)`, []any{w.server, w.id, w.site}},
			{`INSERT INTO cameras (id, tenant_id, site_id, server_id, remote_name, display_name)
			  VALUES ($1, $2, $3, $4, 'cam1', 'Foreign camera')`, []any{w.camera, w.id, w.site, w.server}},
		} {
			if _, err := tx.Exec(ctx, q.sql, q.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return w
}

func (f *faceAPI) event(tenant, site, server, camera uuid.UUID, remote string, labels ...string) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `INSERT INTO events (tenant_id, site_id, server_id, camera_id, remote_id, severity, sub_labels, start_time)
			VALUES ($1, $2, $3, $4, $5, 'alert', $6, $7) RETURNING id`, tenant, site, server, camera, remote, labels, time.Now().Add(-time.Minute)).Scan(&id)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func idsOf(r reply, key string) map[string]bool {
	out := map[string]bool{}
	items, _ := r.json()["items"].([]any)
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			if v, ok := m[key].(string); ok {
				out[v] = true
			}
		}
	}
	return out
}

func TestFacesAPIWithSeveralTenantsListsNeedOneButObjectsResolveTheirOwn(t *testing.T) {
	f := newFaceAPI(t)
	w := f.foreignTenant()
	admin := f.env.AdminToken
	foreignMgr := f.user(w.id, "foreign-mgr", authz.FacesManage, authz.FacesView)
	ownMgr := f.user(f.tenant, "own-mgr", authz.FacesManage, authz.FacesView)

	bea := f.js(foreignMgr, "POST", "/api/v1/faces/persons", `{"display_name":"Bea"}`).json()["id"].(string)
	wl := f.js(foreignMgr, "POST", "/api/v1/faces/watchlists", `{"name":"VIP"}`)
	if wl.code != http.StatusCreated {
		t.Fatalf("create watchlist = %d %s", wl.code, wl.raw)
	}
	wlID := wl.json()["id"].(string)

	t.Run("lists and creates need a tenant when several exist", func(t *testing.T) {
		for _, c := range [][3]string{
			{"GET", "/api/v1/faces/persons", ""}, {"GET", "/api/v1/faces/watchlists", ""},
			{"POST", "/api/v1/faces/persons", `{"display_name":"X"}`}, {"POST", "/api/v1/faces/watchlists", `{"name":"X"}`},
		} {
			if r := f.js(admin, c[0], c[1], c[2]); r.code != http.StatusBadRequest {
				t.Errorf("admin %s %s without tenant_id = %d %s, want 400", c[0], c[1], r.code, r.raw)
			}
		}
		if r := f.js(admin, "GET", "/api/v1/faces/persons?tenant_id="+w.id.String(), ""); r.code != http.StatusOK || !idsOf(r, "id")[bea] {
			t.Errorf("admin with tenant_id = %d %s", r.code, r.raw)
		}
	})

	t.Run("object routes resolve the tenant from the object", func(t *testing.T) {
		personPath := "/api/v1/faces/persons/" + bea
		for _, c := range [][2]string{
			{"GET", personPath}, {"GET", personPath + "/images"}, {"GET", personPath + "/sync"}, {"POST", personPath + "/sync/retry"},
			{"GET", "/api/v1/faces/watchlists/" + wlID},
		} {
			if r := f.js(admin, c[0], c[1], ""); r.code != http.StatusOK {
				t.Errorf("admin %s %s without tenant_id = %d %s, want 200", c[0], c[1], r.code, r.raw)
			}
		}
		if r := f.js(admin, "PATCH", personPath, `{"notes":"hi"}`); r.code != http.StatusOK || r.json()["tenant_id"] != w.id.String() {
			t.Errorf("admin PATCH person = %d %s", r.code, r.raw)
		}
		if r := f.js(admin, "PATCH", "/api/v1/faces/watchlists/"+wlID, `{"description":"d"}`); r.code != http.StatusOK {
			t.Errorf("admin PATCH watchlist = %d %s", r.code, r.raw)
		}
		if r := f.js(admin, "GET", "/api/v1/faces/persons/"+uuid.NewString(), ""); r.code != http.StatusNotFound {
			t.Errorf("unknown person = %d, want 404", r.code)
		}
		if r := f.js(admin, "DELETE", personPath, ""); r.code != http.StatusNoContent {
			t.Errorf("admin DELETE person = %d %s", r.code, r.raw)
		}
		if r := f.js(admin, "DELETE", "/api/v1/faces/watchlists/"+wlID, ""); r.code != http.StatusNoContent {
			t.Errorf("admin DELETE watchlist = %d %s", r.code, r.raw)
		}
	})

	t.Run("a member never reaches another tenant's objects", func(t *testing.T) {
		other := f.js(foreignMgr, "POST", "/api/v1/faces/persons", `{"display_name":"Cleo"}`).json()["id"].(string)
		otherWL := f.js(foreignMgr, "POST", "/api/v1/faces/watchlists", `{"name":"Other"}`).json()["id"].(string)
		for _, path := range []string{
			"/api/v1/faces/persons/" + other, "/api/v1/faces/persons/" + other + "/images", "/api/v1/faces/persons/" + other + "/sync",
			"/api/v1/faces/watchlists/" + otherWL,
		} {
			if r := f.js(ownMgr, "GET", path, ""); r.code != http.StatusNotFound {
				t.Errorf("member GET %s = %d, want 404", path, r.code)
			}
		}
		if r := f.js(ownMgr, "GET", "/api/v1/faces/persons", ""); r.code != http.StatusOK || idsOf(r, "id")[other] {
			t.Errorf("member list = %d %s", r.code, r.raw)
		}
		if r := f.js(ownMgr, "GET", "/api/v1/faces/persons?tenant_id="+w.id.String(), ""); r.code != http.StatusNotFound {
			t.Errorf("member naming a foreign tenant = %d, want 404", r.code)
		}
	})
}

func TestFacesAPIRecognitionsFilterBySiteServerGroupAndCamera(t *testing.T) {
	f := newFaceAPI(t)
	w := f.foreignTenant()
	admin := f.env.AdminToken
	ownMgr := f.user(f.tenant, "own-mgr", authz.FacesManage, authz.FacesView, authz.EventsView)
	foreignMgr := f.user(w.id, "foreign-mgr", authz.FacesManage, authz.FacesView, authz.EventsView)

	ana := f.js(ownMgr, "POST", "/api/v1/faces/persons", `{"display_name":"Ana"}`).json()
	bea := f.js(foreignMgr, "POST", "/api/v1/faces/persons", `{"display_name":"Bea"}`).json()
	cam := f.env.Cameras["frigate-h01/plaza"]
	own := f.event(cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, "own-1", ana["frigate_name"].(string))
	foreign := f.event(w.id, w.site, w.server, w.camera, "foreign-1", bea["frigate_name"].(string))

	events := func(token, query string) map[string]bool {
		t.Helper()
		r := f.js(token, "GET", "/api/v1/faces/recognitions"+query, "")
		if r.code != http.StatusOK {
			t.Fatalf("GET recognitions%s = %d %s", query, r.code, r.raw)
		}
		return idsOf(r, "event_id")
	}
	if got := events(admin, ""); len(got) != 2 || !got[own.String()] || !got[foreign.String()] {
		t.Errorf("admin without filters = %v", got)
	}
	for name, c := range map[string]struct {
		query string
		want  uuid.UUID
	}{
		"site":   {"?site_id=" + w.site.String(), foreign},
		"server": {"?server_id=" + cam.ServerID.String(), own},
		"camera": {"?camera_id=" + w.camera.String(), foreign},
	} {
		if got := events(admin, c.query); len(got) != 1 || !got[c.want.String()] {
			t.Errorf("admin %s filter = %v, want only %v", name, got, c.want)
		}
	}
	groupID := uuid.New()
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		ctx := context.Background()
		if _, err := tx.Exec(ctx, `INSERT INTO camera_groups (id, tenant_id, name) VALUES ($1, $2, 'G')`, groupID, w.id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO camera_group_members (group_id, camera_id, tenant_id) VALUES ($1, $2, $3)`, groupID, w.camera, w.id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := events(admin, "?camera_group_id="+groupID.String()); len(got) != 1 || !got[foreign.String()] {
		t.Errorf("admin group filter = %v", got)
	}

	// A member sees only its tenant, and probing a foreign site, camera or tenant reveals nothing.
	if got := events(ownMgr, ""); len(got) != 1 || !got[own.String()] {
		t.Errorf("member without filters = %v", got)
	}
	if got := events(ownMgr, "?site_id="+w.site.String()+"&camera_id="+w.camera.String()); len(got) != 0 {
		t.Errorf("member probing foreign filters = %v", got)
	}
	if r := f.js(ownMgr, "GET", "/api/v1/faces/recognitions?tenant_id="+url.QueryEscape(w.id.String()), ""); r.code != http.StatusNotFound {
		t.Errorf("member naming a foreign tenant = %d, want 404", r.code)
	}
	// Without faces.view the names are not shown at all.
	noFaces := f.user(f.tenant, "nofaces", authz.EventsView)
	if r := f.js(noFaces, "GET", "/api/v1/faces/recognitions", ""); r.code != http.StatusForbidden {
		t.Errorf("member without faces.view = %d, want 403", r.code)
	}
}

func TestFacesAPITrainAndConfigScopeBySiteAndServerWithoutTenant(t *testing.T) {
	f := newFaceAPI(t)
	w := f.foreignTenant()
	admin := f.env.AdminToken
	cam := f.env.Cameras["frigate-h01/plaza"]
	h01 := f.env.Mocks["frigate-h01"].Server
	h01.FaceRecognition = true
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE frigate_servers SET capabilities = capabilities || '{"face_recognition": true}'::jsonb WHERE id = $1`, cam.ServerID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	const file = "1700000000.1-abc123-1700000100-unknown-0.41.webp"
	png := testPNG(t)
	h01.AddFaceTrainImage(file, png)

	r := f.js(admin, "GET", "/api/v1/faces/train", "")
	if r.code != http.StatusOK || len(r.json()["servers"].([]any)) != 2 || len(r.json()["items"].([]any)) != 1 {
		t.Fatalf("admin train = %d %s; want both tenants' servers", r.code, r.raw)
	}
	if r := f.js(admin, "GET", "/api/v1/faces/train?site_id="+w.site.String(), ""); r.code != http.StatusOK || len(r.json()["servers"].([]any)) != 1 {
		t.Errorf("train site filter = %d %s", r.code, r.raw)
	}
	if r := f.js(admin, "GET", "/api/v1/faces/train?server_id="+cam.ServerID.String(), ""); r.code != http.StatusOK || len(r.json()["items"].([]any)) != 1 {
		t.Errorf("train server filter = %d %s", r.code, r.raw)
	}

	imgPath := "/api/v1/faces/train/" + cam.ServerID.String() + "/image?file=" + file
	if im := f.js(admin, "GET", imgPath, ""); im.code != http.StatusOK {
		t.Errorf("admin train image without tenant = %d %s", im.code, im.raw)
	}
	ana := f.js(admin, "POST", "/api/v1/faces/persons?tenant_id="+f.tenant.String(), `{"display_name":"Ana"}`).json()["id"].(string)
	assign := `{"server_id":"` + cam.ServerID.String() + `","file":"` + file + `","person_id":"` + ana + `"}`
	if as := f.js(admin, "POST", "/api/v1/faces/train/assign", assign); as.code != http.StatusCreated {
		t.Errorf("admin assign without tenant = %d %s", as.code, as.raw)
	}
	if im := f.js(admin, "GET", "/api/v1/faces/train/"+uuid.NewString()+"/image?file="+file, ""); im.code != http.StatusNotFound {
		t.Errorf("unknown server = %d, want 404", im.code)
	}

	member := f.user(f.tenant, "member", authz.FacesView, authz.FacesManage, authz.ServersView)
	foreignImg := "/api/v1/faces/train/" + w.server.String() + "/image?file=" + file
	if im := f.js(member, "GET", foreignImg, ""); im.code != http.StatusNotFound {
		t.Errorf("member train image of a foreign server = %d, want 404", im.code)
	}
	if mr := f.js(member, "GET", "/api/v1/faces/train", ""); mr.code != http.StatusOK || len(mr.json()["servers"].([]any)) != 1 {
		t.Errorf("member train = %d %s", mr.code, mr.raw)
	}

	cfg := f.js(admin, "GET", "/api/v1/faces/config", "")
	if cfg.code != http.StatusOK || len(idsOf(cfg, "server_id")) != 3 {
		t.Fatalf("admin config = %d %s; want every server of every tenant", cfg.code, cfg.raw)
	}
	if cfg := f.js(admin, "GET", "/api/v1/faces/config?site_id="+w.site.String(), ""); cfg.code != http.StatusOK || !idsOf(cfg, "server_id")[w.server.String()] || len(idsOf(cfg, "server_id")) != 1 {
		t.Errorf("config site filter = %d %s", cfg.code, cfg.raw)
	}
	if mc := f.js(member, "GET", "/api/v1/faces/config", ""); mc.code != http.StatusOK || len(idsOf(mc, "server_id")) != 2 || idsOf(mc, "server_id")[w.server.String()] {
		t.Errorf("member config = %d %s", mc.code, mc.raw)
	}
}
