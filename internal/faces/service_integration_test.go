//go:build integration

package faces_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/faces"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
	"github.com/jdolan-exalink/openvms/migrations"
)

type memBlobs struct {
	mu   sync.Mutex
	data map[string][]byte
	ct   map[string]string
	// failPut makes Put fail, to prove a failed upload leaves no metadata behind.
	failPut bool
}

func newBlobs() *memBlobs { return &memBlobs{data: map[string][]byte{}, ct: map[string]string{}} }

func (m *memBlobs) Put(_ context.Context, key string, body []byte, ct string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failPut {
		return errors.New("object store down")
	}
	m.data[key] = append([]byte(nil), body...)
	m.ct[key] = ct
	return nil
}

func (m *memBlobs) Get(_ context.Context, key string) ([]byte, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.data[key]
	if !ok {
		return nil, "", store.ErrNotFound
	}
	return b, m.ct[key], nil
}

func (m *memBlobs) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *memBlobs) keys() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.data) }

type fixture struct {
	t      *testing.T
	env    *demofix.Env
	svc    *faces.Service
	blobs  *memBlobs
	tenant uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	env := demofix.Setup(t)
	blobs := newBlobs()
	return &fixture{t: t, env: env, blobs: blobs, tenant: env.Demo.TenantID,
		svc: &faces.Service{Store: env.Store, Blobs: blobs, Log: pgtest.Discard()}}
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), sql, args...)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) queryRow(sql string, dest any, args ...any) {
	f.t.Helper()
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), sql, args...).Scan(dest)
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) scanInt(sql string, args ...any) int {
	f.t.Helper()
	var n int
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), sql, args...).Scan(&n)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

// user creates a tenant user holding the given tenant-scoped permissions.
func (f *fixture) user(tenantID uuid.UUID, name string, perms ...authz.Permission) authz.Actor {
	f.t.Helper()
	actor, _, err := bootstrap.TenantUser(context.Background(), f.env.Store, tenantID, name+"-"+uuid.NewString()[:6])
	if err != nil {
		f.t.Fatal(err)
	}
	for _, p := range perms {
		if _, err := f.env.Svc.CreateGrant(context.Background(), f.env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: actor.UserID, Permission: p, Effect: authz.Allow,
			ScopeType: authz.ScopeTenant, ScopeID: &tenantID,
		}); err != nil {
			f.t.Fatal(err)
		}
	}
	return actor
}

func (f *fixture) otherTenant() uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `INSERT INTO tenants (slug, name) VALUES ($1, 'other') RETURNING id`,
			"other-"+uuid.NewString()[:6]).Scan(&id)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func pngOf(t *testing.T, seed uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 16, 16))
	img.Pix[0] = seed
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func migrationSection(t *testing.T, file, from, to string) string {
	t.Helper()
	b, err := migrations.FS.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, from)
	if i < 0 {
		t.Fatalf("%s: marker %q not found", file, from)
	}
	s = s[i:]
	if j := strings.Index(s, to); j >= 0 {
		s = s[:j]
	}
	return s
}

func can(t *testing.T, f *fixture, actor authz.Actor, p authz.Permission) bool {
	t.Helper()
	var ok bool
	err := f.env.Store.Tx(context.Background(), store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(context.Background(), q, actor)
		if err != nil {
			return err
		}
		ok = chk.Can(p, access.Tenant(f.tenant))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestFaceGrantSeedingMirrorsTenantManageAndLPRView(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tenantID := f.tenant
	cam := f.env.Cameras["frigate-c01/muelle"].ID

	mk := func(name string) uuid.UUID {
		a, _, err := bootstrap.TenantUser(ctx, f.env.Store, tenantID, name+"-"+uuid.NewString()[:6])
		if err != nil {
			t.Fatal(err)
		}
		return a.UserID
	}
	grant := func(u uuid.UUID, perm, effect, scope string, scopeID *uuid.UUID) {
		f.exec(`INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id)
VALUES ($1, 'user', $2, $3, $4, $5, $6)`, tenantID, u, perm, effect, scope, scopeID)
	}
	manager, lprViewer, lprDenied, cameraOnly, none := mk("manager"), mk("lpr"), mk("lprdeny"), mk("camonly"), mk("none")
	grant(manager, "tenant.manage", "allow", "tenant", &tenantID)
	grant(lprViewer, "lpr.view", "allow", "tenant", &tenantID)
	grant(lprDenied, "tenant.manage", "allow", "tenant", &tenantID)
	grant(lprDenied, "lpr.view", "deny", "tenant", &tenantID)
	grant(cameraOnly, "lpr.view", "allow", "camera", &cam)

	sql := migrationSection(t, "00056_face_persons.sql", "-- Seed grants", "-- +goose Down")
	f.exec(sql)
	f.exec(sql) // idempotent

	want := map[string]struct {
		id           uuid.UUID
		view, manage bool
	}{
		"manager":    {manager, true, true},
		"lprViewer":  {lprViewer, true, false},
		"lprDenied":  {lprDenied, false, true},
		"cameraOnly": {cameraOnly, false, false},
		"none":       {none, false, false},
	}
	for name, w := range want {
		actor := authz.Actor{UserID: w.id, Username: name, TenantID: &tenantID}
		if got := can(t, f, actor, authz.FacesView); got != w.view {
			t.Errorf("%s: faces.view = %v, want %v", name, got, w.view)
		}
		if got := can(t, f, actor, authz.FacesManage); got != w.manage {
			t.Errorf("%s: faces.manage = %v, want %v", name, got, w.manage)
		}
	}
}

func TestFaceSchemaConstraintsAndRLS(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	other := f.otherTenant()

	insertPerson := func(tenant uuid.UUID, display, frigate string) (uuid.UUID, error) {
		var id uuid.UUID
		err := f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `INSERT INTO face_persons (tenant_id, display_name, frigate_name) VALUES ($1,$2,$3) RETURNING id`,
				tenant, display, frigate).Scan(&id)
		})
		return id, err
	}
	p1, err := insertPerson(f.tenant, "Ana Gómez", "ana_gomez")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := insertPerson(f.tenant, "ANA GÓMEZ", "ana_gomez_2"); err == nil {
		t.Error("display_name must be unique per tenant, case-insensitively")
	}
	if _, err := insertPerson(f.tenant, "Otra", "ana_gomez"); err == nil {
		t.Error("frigate_name must be unique per tenant")
	}
	for _, bad := range []string{"Ana", "ana gomez", "ana-gomez", "", strings.Repeat("a", 51)} {
		if _, err := insertPerson(f.tenant, "x"+bad, bad); err == nil {
			t.Errorf("frigate_name %q must be rejected", bad)
		}
	}
	if _, err := insertPerson(other, "Ana Gómez", "ana_gomez"); err != nil {
		t.Errorf("another tenant may reuse names: %v", err)
	}
	// A soft-deleted person frees its display name but keeps its frigate_name reserved.
	f.exec(`UPDATE face_persons SET deleted_at = now() WHERE id = $1`, p1)
	if _, err := insertPerson(f.tenant, "Ana Gómez", "ana_gomez_b"); err != nil {
		t.Errorf("display name must be reusable after delete: %v", err)
	}
	if _, err := insertPerson(f.tenant, "Nueva", "ana_gomez"); err == nil {
		t.Error("frigate_name of a tombstone must stay reserved")
	}

	img := func(person, tenant uuid.UUID, sha, ct string) error {
		return f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO face_person_images (tenant_id, person_id, storage_key, content_type, size_bytes, sha256)
VALUES ($1,$2,'k',$3,10,$4)`, tenant, person, ct, sha)
			return err
		})
	}
	sha := strings.Repeat("a", 64)
	if err := img(p1, f.tenant, sha, "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := img(p1, f.tenant, sha, "image/png"); err == nil {
		t.Error("same image twice for a person must conflict")
	}
	if err := img(p1, f.tenant, strings.Repeat("b", 64), "image/gif"); err == nil {
		t.Error("content type must be restricted")
	}
	if err := img(p1, f.tenant, "XYZ", "image/png"); err == nil {
		t.Error("sha256 must be 64 hex chars")
	}
	if err := img(p1, other, strings.Repeat("c", 64), "image/png"); err == nil {
		t.Error("image tenant must match the person's tenant")
	}

	// RLS: the other tenant's scope sees none of this tenant's rows.
	for _, table := range []string{"face_persons", "face_person_images"} {
		var n int
		err := f.env.Store.TxRaw(ctx, store.TenantScope{TenantID: other}, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id = $1`, f.tenant).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s: tenant isolation leaks %d rows", table, n)
		}
	}
	// Writing another tenant's row through a tenant scope is rejected by the policy.
	err = f.env.Store.TxRaw(ctx, store.TenantScope{TenantID: other}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO face_persons (tenant_id, display_name, frigate_name) VALUES ($1,'Intruso','intruso')`, f.tenant)
		return err
	})
	if err == nil {
		t.Error("cross-tenant insert must be rejected by RLS")
	}
	// Deleting a person row cascades to its images.
	f.exec(`DELETE FROM face_persons WHERE id = $1`, p1)
	if n := f.scanInt(`SELECT count(*) FROM face_person_images WHERE person_id = $1`, p1); n != 0 {
		t.Errorf("images must cascade with their person, %d left", n)
	}
}

func TestPersonLifecycleAuthzAndAudit(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	manager := f.user(f.tenant, "mgr", authz.FacesManage, authz.FacesView)
	viewer := f.user(f.tenant, "view", authz.FacesView)
	nobody := f.user(f.tenant, "nobody")

	// Create: manage only.
	if _, err := f.svc.CreatePerson(ctx, viewer, f.tenant, faces.PersonInput{DisplayName: "Ana"}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer create = %v, want forbidden", err)
	}
	p, err := f.svc.CreatePerson(ctx, manager, f.tenant, faces.PersonInput{DisplayName: "  Juan Pérez ", Notes: "VIP"})
	if err != nil {
		t.Fatal(err)
	}
	if p.DisplayName != "Juan Pérez" || p.FrigateName != "juan_perez" || p.ImageCount != 0 {
		t.Fatalf("unexpected person %+v", p)
	}
	if _, err := f.svc.CreatePerson(ctx, manager, f.tenant, faces.PersonInput{DisplayName: "juan pérez"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate display name = %v, want conflict", err)
	}
	// A different display name that normalizes to the same Frigate name gets a distinct one.
	p2, err := f.svc.CreatePerson(ctx, manager, f.tenant, faces.PersonInput{DisplayName: "Juan Perez"})
	if err != nil {
		t.Fatal(err)
	}
	if p2.FrigateName == p.FrigateName || !strings.HasPrefix(p2.FrigateName, "juan_perez_") {
		t.Errorf("frigate name collision not resolved: %q vs %q", p2.FrigateName, p.FrigateName)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", 121)} {
		if _, err := f.svc.CreatePerson(ctx, manager, f.tenant, faces.PersonInput{DisplayName: bad}); !isValidation(err) {
			t.Errorf("display name %q = %v, want validation error", bad, err)
		}
	}

	// Read: view only.
	if _, err := f.svc.ListPersons(ctx, nobody, f.tenant); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("nobody list = %v, want forbidden", err)
	}
	list, err := f.svc.ListPersons(ctx, viewer, f.tenant)
	if err != nil || len(list) != 2 {
		t.Fatalf("viewer list = %v, %v", list, err)
	}
	if _, err := f.svc.GetPerson(ctx, nobody, f.tenant, p.ID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("nobody get = %v", err)
	}

	// Rename keeps the Frigate name stable.
	newName := "Juan P. Pérez"
	u, err := f.svc.UpdatePerson(ctx, manager, f.tenant, p.ID, faces.PersonUpdate{DisplayName: &newName})
	if err != nil {
		t.Fatal(err)
	}
	if u.DisplayName != newName || u.FrigateName != p.FrigateName || u.Notes != "VIP" {
		t.Fatalf("rename result %+v", u)
	}
	if _, err := f.svc.UpdatePerson(ctx, viewer, f.tenant, p.ID, faces.PersonUpdate{DisplayName: &newName}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer update = %v", err)
	}

	// Delete: viewer denied; manager succeeds; person disappears.
	if err := f.svc.DeletePerson(ctx, viewer, f.tenant, p.ID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer delete = %v", err)
	}
	if err := f.svc.DeletePerson(ctx, manager, f.tenant, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.GetPerson(ctx, manager, f.tenant, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get deleted = %v, want not found", err)
	}
	if err := f.svc.DeletePerson(ctx, manager, f.tenant, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("double delete = %v, want not found", err)
	}

	// Audit: one row per mutation, ids only (no names, notes or bytes).
	for action, want := range map[string]int{
		faces.ActionPersonCreated: 2, faces.ActionPersonUpdated: 1, faces.ActionPersonDeleted: 1,
	} {
		if n := f.scanInt(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`, f.tenant, action); n != want {
			t.Errorf("audit %s = %d, want %d", action, n, want)
		}
	}
	if n := f.scanInt(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action LIKE 'FACE_%' AND (details::text ILIKE '%Pérez%' OR details::text ILIKE '%VIP%')`, f.tenant); n != 0 {
		t.Errorf("audit details leak person names or notes (%d rows)", n)
	}
}

func isValidation(err error) bool {
	var v *faces.ValidationError
	return errors.As(err, &v)
}

func TestImagesStorageLimitsAndAudit(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	manager := f.user(f.tenant, "mgr", authz.FacesManage, authz.FacesView)
	viewer := f.user(f.tenant, "view", authz.FacesView)
	nobody := f.user(f.tenant, "nobody")
	p, err := f.svc.CreatePerson(ctx, manager, f.tenant, faces.PersonInput{DisplayName: "Ana"})
	if err != nil {
		t.Fatal(err)
	}
	data := pngOf(t, 1)

	if _, err := f.svc.AddImage(ctx, viewer, f.tenant, p.ID, data, "image/png"); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer add = %v", err)
	}
	if _, err := f.svc.AddImage(ctx, manager, f.tenant, p.ID, []byte("nope"), "image/png"); !isValidation(err) {
		t.Fatalf("invalid image = %v", err)
	}
	img, err := f.svc.AddImage(ctx, manager, f.tenant, p.ID, data, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 16 || img.Height != 16 || img.ContentType != "image/png" || img.SizeBytes != len(data) {
		t.Fatalf("image meta %+v", img)
	}
	if f.blobs.keys() != 1 {
		t.Fatalf("want 1 blob stored, got %d", f.blobs.keys())
	}
	if _, err := f.svc.AddImage(ctx, manager, f.tenant, p.ID, data, "image/png"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate image = %v, want conflict", err)
	}
	if f.blobs.keys() != 1 {
		t.Fatalf("duplicate upload must not leave a blob, got %d", f.blobs.keys())
	}

	// A failed object-store write leaves no metadata row behind.
	f.blobs.failPut = true
	if _, err := f.svc.AddImage(ctx, manager, f.tenant, p.ID, pngOf(t, 2), "image/png"); err == nil {
		t.Fatal("want store failure")
	}
	f.blobs.failPut = false
	imgs, err := f.svc.ListImages(ctx, viewer, f.tenant, p.ID)
	if err != nil || len(imgs) != 1 {
		t.Fatalf("images after failed put = %v, %v", imgs, err)
	}

	// Reads need faces.view and are audited.
	if _, _, err := f.svc.ReadImage(ctx, nobody, f.tenant, p.ID, img.ID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("nobody read = %v", err)
	}
	got, ct, err := f.svc.ReadImage(ctx, viewer, f.tenant, p.ID, img.ID)
	if err != nil || ct != "image/png" || !bytes.Equal(got, data) {
		t.Fatalf("read image: ct=%q err=%v", ct, err)
	}
	if n := f.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1 AND actor_id = $2`, faces.ActionImageRead, viewer.UserID); n != 1 {
		t.Errorf("image read audit rows = %d, want 1", n)
	}

	// Per-person cap.
	for i := 0; i < faces.MaxImagesPerPerson-1; i++ {
		if _, err := f.svc.AddImage(ctx, manager, f.tenant, p.ID, pngOf(t, uint8(10+i)), "image/png"); err != nil {
			t.Fatalf("image %d: %v", i, err)
		}
	}
	if _, err := f.svc.AddImage(ctx, manager, f.tenant, p.ID, pngOf(t, 200), "image/png"); !isValidation(err) {
		t.Fatalf("over the cap = %v, want validation error", err)
	}

	// Remove one image.
	if err := f.svc.RemoveImage(ctx, viewer, f.tenant, p.ID, img.ID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer remove = %v", err)
	}
	if err := f.svc.RemoveImage(ctx, manager, f.tenant, p.ID, img.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.ReadImage(ctx, viewer, f.tenant, p.ID, img.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("read removed = %v", err)
	}

	// Deleting the person removes every central image (rows and blobs).
	if err := f.svc.DeletePerson(ctx, manager, f.tenant, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.PurgeBlobs(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if f.blobs.keys() != 0 {
		t.Errorf("blobs left after person delete: %d", f.blobs.keys())
	}
	if n := f.scanInt(`SELECT count(*) FROM face_person_images WHERE person_id = $1`, p.ID); n != 0 {
		t.Errorf("image rows left after person delete: %d", n)
	}
	// Audit never carries bytes.
	if n := f.scanInt(`SELECT count(*) FROM audit_log WHERE action LIKE 'FACE_%' AND length(details::text) > 1000`); n != 0 {
		t.Errorf("suspiciously large audit details: %d rows", n)
	}
}

func TestCrossTenantAccessIsNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	other := f.otherTenant()
	owner := f.user(f.tenant, "owner", authz.FacesManage, authz.FacesView)
	p, err := f.svc.CreatePerson(ctx, owner, f.tenant, faces.PersonInput{DisplayName: "Ana"})
	if err != nil {
		t.Fatal(err)
	}
	intruder := f.user(other, "intruder", authz.FacesManage, authz.FacesView)
	if _, err := f.svc.GetPerson(ctx, intruder, f.tenant, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant get = %v, want not found", err)
	}
	if _, err := f.svc.ListPersons(ctx, intruder, f.tenant); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant list = %v, want not found", err)
	}
	if _, err := f.svc.GetPerson(ctx, intruder, other, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("own-tenant get of foreign id = %v, want not found", err)
	}
}
