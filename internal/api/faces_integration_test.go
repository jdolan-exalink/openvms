//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/faces"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type delBlobs struct{ *memBlobs }

func (b delBlobs) Delete(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.data, key)
	return nil
}

type faceAPI struct {
	t      *testing.T
	env    *demofix.Env
	ts     *httptest.Server
	tenant uuid.UUID
	blobs  delBlobs
}

func newFaceAPI(t *testing.T) *faceAPI {
	t.Helper()
	env := demofix.Setup(t)
	adapters := inventory.NewAdapters(env.Svc)
	blobs := delBlobs{&memBlobs{}}
	h := &api.Handlers{
		Inv: env.Svc, Log: pgtest.Discard(),
		Faces:  &faces.Service{Store: env.Store, Blobs: blobs, Adapters: adapters, Log: pgtest.Discard()},
		Events: &events.Service{Store: env.Store, Blobs: blobs, Adapters: adapters, Log: pgtest.Discard()},
		Alarms: &alarms.Service{Store: env.Store, Log: pgtest.Discard()},
	}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{Queries: db.New(env.Pool)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return &faceAPI{t: t, env: env, ts: ts, tenant: env.Demo.TenantID, blobs: blobs}
}

// user returns a bearer token of a tenant user holding the permissions at tenant scope.
func (f *faceAPI) user(tenant uuid.UUID, name string, perms ...authz.Permission) string {
	f.t.Helper()
	actor, token, err := bootstrap.TenantUser(context.Background(), f.env.Store, tenant, name+"-"+uuid.NewString()[:6])
	if err != nil {
		f.t.Fatal(err)
	}
	for _, p := range perms {
		if _, err := f.env.Svc.CreateGrant(context.Background(), f.env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: actor.UserID, Permission: p, Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenant,
		}); err != nil {
			f.t.Fatal(err)
		}
	}
	return token
}

type reply struct {
	code   int
	header http.Header
	raw    []byte
}

func (r reply) json() map[string]any {
	var out map[string]any
	_ = json.Unmarshal(r.raw, &out)
	return out
}

func (f *faceAPI) do(token, method, path, contentType string, body []byte) reply {
	f.t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), method, f.ts.URL+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return reply{code: resp.StatusCode, header: resp.Header, raw: raw}
}

func (f *faceAPI) js(token, method, path, body string) reply {
	return f.do(token, method, path, "application/json", []byte(body))
}

func multipartFile(t *testing.T, field, contentType string, data []byte) (string, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="`+field+`"; filename="x.png"`)
	h.Set("Content-Type", contentType)
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(data)
	_ = mw.Close()
	return mw.FormDataContentType(), buf.Bytes()
}

func TestFacesAPIPersonsImagesAndAuthz(t *testing.T) {
	f := newFaceAPI(t)
	mgr := f.user(f.tenant, "mgr", authz.FacesManage, authz.FacesView)
	viewer := f.user(f.tenant, "viewer", authz.FacesView)
	blind := f.user(f.tenant, "blind", authz.EventsView)

	t.Run("without faces.view every person endpoint is 403", func(t *testing.T) {
		for _, c := range [][2]string{{"GET", "/api/v1/faces/persons"}, {"GET", "/api/v1/faces/recognitions"}, {"GET", "/api/v1/faces/train"}} {
			if r := f.js(blind, c[0], c[1], ""); r.code != http.StatusForbidden {
				t.Errorf("%s %s = %d, want 403", c[0], c[1], r.code)
			}
		}
		if r := f.js(blind, "POST", "/api/v1/faces/persons", `{"display_name":"Ana"}`); r.code != http.StatusForbidden {
			t.Errorf("create = %d, want 403", r.code)
		}
	})
	t.Run("faces.view alone cannot change anything", func(t *testing.T) {
		if r := f.js(viewer, "POST", "/api/v1/faces/persons", `{"display_name":"Ana"}`); r.code != http.StatusForbidden {
			t.Errorf("create = %d, want 403", r.code)
		}
	})

	r := f.js(mgr, "POST", "/api/v1/faces/persons", `{"display_name":"Ana Perez","notes":"vip"}`)
	if r.code != http.StatusCreated {
		t.Fatalf("create = %d %s", r.code, r.raw)
	}
	person := r.json()
	pid := person["id"].(string)
	base := "/api/v1/faces/persons/" + pid
	if person["frigate_name"] != "ana_perez" || person["image_count"].(float64) != 0 || person["sync"] == nil {
		t.Fatalf("person = %v", person)
	}
	if r := f.js(mgr, "POST", "/api/v1/faces/persons", `{"display_name":"  "}`); r.code != http.StatusBadRequest {
		t.Errorf("blank name = %d, want 400", r.code)
	}
	if r := f.js(mgr, "POST", "/api/v1/faces/persons", `{"display_name":"ana perez"}`); r.code != http.StatusConflict {
		t.Errorf("duplicate name = %d, want 409", r.code)
	}
	if r := f.js(viewer, "PATCH", base, `{"notes":"x"}`); r.code != http.StatusForbidden {
		t.Errorf("viewer patch = %d, want 403", r.code)
	}
	if r := f.js(mgr, "PATCH", base, `{"display_name":"Ana P."}`); r.code != http.StatusOK || r.json()["display_name"] != "Ana P." {
		t.Errorf("rename = %d %s", r.code, r.raw)
	}

	// Upload: multipart, validated server side.
	png := testPNG(t)
	ct, body := multipartFile(t, "file", "image/png", png)
	r = f.do(mgr, "POST", base+"/images", ct, body)
	if r.code != http.StatusCreated {
		t.Fatalf("upload = %d %s", r.code, r.raw)
	}
	iid := r.json()["id"].(string)
	ct, body = multipartFile(t, "file", "image/png", png)
	if r := f.do(mgr, "POST", base+"/images", ct, body); r.code != http.StatusConflict {
		t.Errorf("duplicate image = %d, want 409", r.code)
	}
	ct, body = multipartFile(t, "file", "image/png", []byte("not an image"))
	if r := f.do(mgr, "POST", base+"/images", ct, body); r.code != http.StatusBadRequest {
		t.Errorf("non-image = %d, want 400", r.code)
	}
	ct, body = multipartFile(t, "other", "image/png", png)
	if r := f.do(mgr, "POST", base+"/images", ct, body); r.code != http.StatusBadRequest {
		t.Errorf("missing file part = %d, want 400", r.code)
	}
	if r := f.js(mgr, "POST", base+"/images", `{"a":1}`); r.code != http.StatusBadRequest {
		t.Errorf("json instead of multipart = %d, want 400", r.code)
	}
	ct, body = multipartFile(t, "file", "image/png", png)
	if r := f.do(viewer, "POST", base+"/images", ct, body); r.code != http.StatusForbidden {
		t.Errorf("viewer upload = %d, want 403", r.code)
	}

	// Authorized media: bytes round-trip, never cached publicly, reads audited.
	img := f.js(viewer, "GET", base+"/images/"+iid, "")
	if img.code != http.StatusOK || !bytes.Equal(img.raw, png) || img.header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("image = %d cache=%q equal=%v", img.code, img.header.Get("Cache-Control"), bytes.Equal(img.raw, png))
	}
	if r := f.js(blind, "GET", base+"/images/"+iid, ""); r.code != http.StatusForbidden {
		t.Errorf("blind image = %d, want 403", r.code)
	}
	list := f.js(viewer, "GET", "/api/v1/faces/persons", "")
	items := list.json()["items"].([]any)
	if list.code != 200 || len(items) != 1 || items[0].(map[string]any)["image_count"].(float64) != 1 {
		t.Fatalf("list = %d %s", list.code, list.raw)
	}
	if r := f.js(viewer, "GET", base+"/images", ""); r.code != 200 || len(r.json()["items"].([]any)) != 1 {
		t.Errorf("images = %d %s", r.code, r.raw)
	}
	// Jobs are created by the worker, so a fresh person has none yet.
	if r := f.js(viewer, "GET", base+"/sync", ""); r.code != 200 || r.json()["items"] == nil {
		t.Errorf("sync status = %d %s", r.code, r.raw)
	}
	if r := f.js(viewer, "POST", base+"/sync/retry", ""); r.code != http.StatusForbidden {
		t.Errorf("viewer retry = %d, want 403", r.code)
	}
	if r := f.js(mgr, "POST", base+"/sync/retry", ""); r.code != 200 || r.json()["requeued"] == nil {
		t.Errorf("retry = %d %s", r.code, r.raw)
	}

	// Cross-tenant: another tenant's manager cannot see or touch it.
	var otherTenant uuid.UUID
	if err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `INSERT INTO tenants (slug, name) VALUES ('otro-faces', 'o') RETURNING id`).Scan(&otherTenant)
	}); err != nil {
		t.Fatal(err)
	}
	stranger := f.user(otherTenant, "stranger", authz.FacesManage, authz.FacesView)
	for _, c := range [][2]string{{"GET", base}, {"GET", base + "/images/" + iid}, {"DELETE", base}, {"POST", base + "/sync/retry"}} {
		if r := f.js(stranger, c[0], c[1], ""); r.code != http.StatusNotFound {
			t.Errorf("stranger %s %s = %d, want 404", c[0], c[1], r.code)
		}
	}
	if r := f.js(stranger, "GET", "/api/v1/faces/persons?tenant_id="+f.tenant.String(), ""); r.code != http.StatusNotFound {
		t.Errorf("stranger naming our tenant = %d, want 404", r.code)
	}
	if r := f.js(stranger, "GET", "/api/v1/faces/persons", ""); r.code != 200 || len(r.json()["items"].([]any)) != 0 {
		t.Errorf("stranger list = %d %s", r.code, r.raw)
	}

	// Audit: reads and mutations leave rows, none with the person's name.
	audited := func(action string) int {
		var n int
		_ = f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = $1`, action).Scan(&n)
		})
		return n
	}
	for _, a := range []string{faces.ActionPersonCreated, faces.ActionPersonUpdated, faces.ActionImageAdded, faces.ActionImageRead, faces.ActionPersonRead, faces.ActionSyncRetried} {
		if audited(a) == 0 {
			t.Errorf("no audit row for %s", a)
		}
	}

	if r := f.js(viewer, "DELETE", base+"/images/"+iid, ""); r.code != http.StatusForbidden {
		t.Errorf("viewer delete image = %d, want 403", r.code)
	}
	if r := f.js(mgr, "DELETE", base+"/images/"+iid, ""); r.code != http.StatusNoContent {
		t.Errorf("delete image = %d", r.code)
	}
	if r := f.js(mgr, "DELETE", base, ""); r.code != http.StatusNoContent {
		t.Errorf("delete person = %d", r.code)
	}
	if r := f.js(mgr, "GET", base, ""); r.code != http.StatusNotFound {
		t.Errorf("deleted person = %d, want 404", r.code)
	}
	if audited(faces.ActionPersonDeleted) == 0 || audited(faces.ActionImageRemoved) == 0 {
		t.Error("deletions were not audited")
	}
}

func TestFacesAPIPlatformAdminNeedsNoTenantWhileThereIsOnlyOne(t *testing.T) {
	f := newFaceAPI(t)
	for _, path := range []string{
		"/api/v1/faces/persons", "/api/v1/faces/watchlists", "/api/v1/faces/train", "/api/v1/faces/config", "/api/v1/faces/recognitions",
	} {
		if r := f.js(f.env.AdminToken, "GET", path, ""); r.code != http.StatusOK {
			t.Errorf("admin without tenant_id GET %s = %d %s, want 200", path, r.code, r.raw)
		}
	}
	if r := f.js(f.env.AdminToken, "POST", "/api/v1/faces/persons", `{"display_name":"Ana"}`); r.code != http.StatusCreated || r.json()["tenant_id"] != f.tenant.String() {
		t.Fatalf("admin create without tenant_id = %d %s", r.code, r.raw)
	}
	// tenant_id stays an optional, backward-compatible parameter.
	if r := f.js(f.env.AdminToken, "GET", "/api/v1/faces/persons?tenant_id="+f.tenant.String(), ""); r.code != http.StatusOK {
		t.Fatalf("admin with tenant_id = %d %s", r.code, r.raw)
	}
}

func TestFacesAPIRecognitionsTrainAndRedaction(t *testing.T) {
	f := newFaceAPI(t)
	mgr := f.user(f.tenant, "mgr", authz.FacesManage, authz.FacesView, authz.EventsView, authz.EventsSearch)
	noFaces := f.user(f.tenant, "nofaces", authz.EventsView, authz.EventsSearch)

	r := f.js(mgr, "POST", "/api/v1/faces/persons", `{"display_name":"Ana"}`)
	pid := r.json()["id"].(string)
	cam := f.env.Cameras["frigate-h01/plaza"]
	var eventID uuid.UUID
	if err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `INSERT INTO events (tenant_id, site_id, server_id, camera_id, remote_id, severity, sub_labels, start_time)
			VALUES ($1, $2, $3, $4, 'rec-1', 'alert', ARRAY['ana'], $5) RETURNING id`,
			cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, time.Now().Add(-time.Minute)).Scan(&eventID)
	}); err != nil {
		t.Fatal(err)
	}

	rec := f.js(mgr, "GET", "/api/v1/faces/recognitions?person_id="+pid, "")
	items, _ := rec.json()["items"].([]any)
	if rec.code != 200 || len(items) != 1 || items[0].(map[string]any)["event_id"] != eventID.String() {
		t.Fatalf("recognitions = %d %s", rec.code, rec.raw)
	}
	if r := f.js(mgr, "GET", "/api/v1/faces/recognitions?cursor=%25%25", ""); r.code != http.StatusBadRequest {
		t.Errorf("bad cursor = %d, want 400", r.code)
	}

	// The events API redacts the name for a caller without faces.view, and shows it with it.
	ev := f.js(noFaces, "GET", "/api/v1/events/"+eventID.String(), "")
	if ev.code != 200 || len(ev.json()["sub_labels"].([]any)) != 0 {
		t.Fatalf("redacted event = %d %s", ev.code, ev.raw)
	}
	if r := f.js(noFaces, "GET", "/api/v1/events?sub_label=ana", ""); r.code != 200 || len(r.json()["items"].([]any)) != 0 {
		t.Errorf("filter probe = %d %s", r.code, r.raw)
	}
	ev = f.js(mgr, "GET", "/api/v1/events/"+eventID.String(), "")
	if labels := ev.json()["sub_labels"].([]any); ev.code != 200 || len(labels) != 1 || labels[0] != "ana" {
		t.Fatalf("event with faces.view = %d %s", ev.code, ev.raw)
	}

	// Train from detections against the mock Frigate.
	h01 := f.env.Mocks["frigate-h01"].Server
	h01.FaceRecognition = true
	if err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE frigate_servers SET capabilities = capabilities || '{"face_recognition": true}'::jsonb WHERE id = $1`, cam.ServerID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	const file = "1700000000.1-abc123-1700000100-unknown-0.41.webp"
	png := testPNG(t)
	h01.AddFaceTrainImage(file, png)
	tr := f.js(mgr, "GET", "/api/v1/faces/train", "")
	titems, _ := tr.json()["items"].([]any)
	if tr.code != 200 || len(titems) != 1 || len(tr.json()["servers"].([]any)) != 1 {
		t.Fatalf("train list = %d %s", tr.code, tr.raw)
	}
	imgPath := "/api/v1/faces/train/" + cam.ServerID.String() + "/image?file=" + file
	if im := f.js(mgr, "GET", imgPath, ""); im.code != 200 || !bytes.Equal(im.raw, png) || im.header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("train image = %d", im.code)
	}
	if im := f.js(noFaces, "GET", imgPath, ""); im.code != http.StatusForbidden {
		t.Errorf("train image without faces.view = %d, want 403", im.code)
	}
	if im := f.js(mgr, "GET", "/api/v1/faces/train/"+cam.ServerID.String()+"/image?file=..%2Fx", ""); im.code != http.StatusBadRequest {
		t.Errorf("traversal = %d, want 400", im.code)
	}
	assign := `{"server_id":"` + cam.ServerID.String() + `","file":"` + file + `","person_id":"` + pid + `"}`
	if r := f.js(noFaces, "POST", "/api/v1/faces/train/assign", assign); r.code != http.StatusForbidden {
		t.Errorf("assign without faces.manage = %d, want 403", r.code)
	}
	as := f.js(mgr, "POST", "/api/v1/faces/train/assign", assign)
	if as.code != http.StatusCreated || as.json()["attempt_removed"] != true {
		t.Fatalf("assign = %d %s", as.code, as.raw)
	}
	if p := as.json()["person"].(map[string]any); p["image_count"].(float64) != 1 {
		t.Errorf("person after assign = %v", p)
	}
	if r := f.js(mgr, "POST", "/api/v1/faces/train/assign", `{"server_id":"`+cam.ServerID.String()+`","file":"`+file+`"}`); r.code != http.StatusBadRequest {
		t.Errorf("assign without target = %d, want 400", r.code)
	}
	if r := f.js(mgr, "POST", "/api/v1/faces/train/assign", assign); r.code != http.StatusNotFound {
		t.Errorf("assign of a consumed attempt = %d, want 404", r.code)
	}
}

func TestFacesAPIReviewInboxStableIDsAssignmentAndRejection(t *testing.T) {
	f := newFaceAPI(t)
	mgr := f.user(f.tenant, "review-mgr", authz.FacesManage, authz.FacesView)
	viewer := f.user(f.tenant, "review-viewer", authz.FacesView)
	var otherTenant uuid.UUID
	if err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `INSERT INTO tenants (slug, name) VALUES ('review-other', 'Review other') RETURNING id`).Scan(&otherTenant)
	}); err != nil {
		t.Fatal(err)
	}
	other := f.user(otherTenant, "review-other", authz.FacesView, authz.FacesManage)
	cam := f.env.Cameras["frigate-h01/acceso_norte"]
	h01 := f.env.Mocks["frigate-h01"].Server
	h01.FaceRecognition = true
	if err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE frigate_servers SET capabilities = capabilities || '{"face_recognition": true}'::jsonb WHERE id = $1`, cam.ServerID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	firstFile := "1700000000.1-abcd-1700000100-unknown-0.41.webp"
	secondFile := "1700000000.2-efgh-1700000200-unknown-0.52.webp"
	firstCrop, secondCrop := testPNG(t), testPNG(t)
	h01.AddFaceTrainImage(firstFile, firstCrop)
	h01.AddFaceTrainImage(secondFile, secondCrop)
	ingestor := &faces.ReviewIngestor{
		Store: f.env.Store, Blobs: f.blobs, Adapters: inventory.NewAdapters(f.env.Svc), Log: pgtest.Discard(),
	}
	if err := ingestor.RunOnce(context.Background(), 25); err != nil {
		t.Fatal(err)
	}

	list := f.js(mgr, http.MethodGet, "/api/v1/faces/review-inbox", "")
	items, _ := list.json()["items"].([]any)
	if list.code != http.StatusOK || len(items) != 2 {
		t.Fatalf("review inbox = %d %s", list.code, list.raw)
	}
	sightingID := items[0].(map[string]any)["id"].(string)
	if _, err := uuid.Parse(sightingID); err != nil {
		t.Fatalf("sighting ID = %q, not UUID", sightingID)
	}
	imagePath := "/api/v1/faces/review-inbox/" + sightingID + "/image"
	image := f.js(mgr, http.MethodGet, imagePath, "")
	if image.code != http.StatusOK || image.header.Get("Cache-Control") != "private, no-store" || image.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("private sighting image = %d, cache=%q", image.code, image.header.Get("Cache-Control"))
	}
	if r := f.js(viewer, http.MethodDelete, "/api/v1/faces/review-inbox/"+items[1].(map[string]any)["id"].(string), ""); r.code != http.StatusForbidden {
		t.Fatalf("reject without faces.manage = %d, want 403", r.code)
	}
	if r := f.js(other, http.MethodGet, imagePath, ""); r.code != http.StatusNotFound {
		t.Fatalf("cross-tenant image = %d, want 404", r.code)
	}

	person := f.js(mgr, http.MethodPost, "/api/v1/faces/persons", `{"display_name":"Reviewed person"}`)
	personID := person.json()["id"].(string)
	assigned := f.js(mgr, http.MethodPost, "/api/v1/faces/review-inbox/"+sightingID+"/assign", `{"person_id":"`+personID+`"}`)
	if assigned.code != http.StatusCreated || assigned.json()["attempt_removed"] != true {
		t.Fatalf("assign sighting = %d %s", assigned.code, assigned.raw)
	}
	rejectedID := items[1].(map[string]any)["id"].(string)
	if r := f.js(mgr, http.MethodDelete, "/api/v1/faces/review-inbox/"+rejectedID, ""); r.code != http.StatusNoContent {
		t.Fatalf("reject sighting = %d %s", r.code, r.raw)
	}
	if remaining := f.js(mgr, http.MethodGet, "/api/v1/faces/review-inbox", ""); len(remaining.json()["items"].([]any)) != 0 {
		t.Fatalf("reviewed sightings remain in inbox: %s", remaining.raw)
	}
}

func TestFaceConfigAPI(t *testing.T) {
	f := newFaceAPI(t)
	cam := f.env.Cameras["frigate-h01/acceso_norte"]
	viewer := f.user(f.tenant, "viewer", authz.FacesView, authz.ServersView)
	manager := f.user(f.tenant, "mgr", authz.FacesView, authz.FacesManage, authz.ServersView, authz.ServersConfig)
	blind := f.user(f.tenant, "blind", authz.ServersView)

	if r := f.js(blind, http.MethodGet, "/api/v1/faces/config", ""); r.code != http.StatusForbidden {
		t.Fatalf("list without faces.view = %d", r.code)
	}
	r := f.js(viewer, http.MethodGet, "/api/v1/faces/config", "")
	items, _ := r.json()["items"].([]any)
	if r.code != http.StatusOK || len(items) != 2 {
		t.Fatalf("list = %d %s", r.code, r.raw)
	}
	if first, _ := items[0].(map[string]any); first["can_edit"] != false || first["model_size"] == nil {
		t.Fatalf("viewer item = %v", first)
	}

	body := `{"enabled":true,"model_size":"large","recognition_threshold":0.85,"cameras":[{"camera_id":"` + cam.ID.String() + `","min_area":900}]}`
	path := "/api/v1/faces/config/" + cam.ServerID.String()
	if r := f.js(viewer, http.MethodPatch, path, body); r.code != http.StatusForbidden {
		t.Fatalf("patch as viewer = %d", r.code)
	}
	if r := f.js(manager, http.MethodPatch, path, `{"recognition_threshold":1.5}`); r.code != http.StatusBadRequest {
		t.Fatalf("out of range = %d %s", r.code, r.raw)
	}
	if r := f.js(manager, http.MethodPatch, path, `{}`); r.code != http.StatusBadRequest {
		t.Fatalf("empty patch = %d", r.code)
	}
	r = f.js(manager, http.MethodPatch, path, body)
	ids, _ := r.json()["revision_ids"].([]any)
	if r.code != http.StatusOK || r.json()["restart_required"] != true || len(ids) != 2 {
		t.Fatalf("patch = %d %s", r.code, r.raw)
	}
	if r := f.js(manager, http.MethodPatch, "/api/v1/faces/config/"+uuid.NewString(), body); r.code != http.StatusNotFound {
		t.Fatalf("unknown server = %d", r.code)
	}
}
