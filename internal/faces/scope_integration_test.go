//go:build integration

package faces_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/faces"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
)

// foreignWorld is a second tenant with its own site, face-capable server, camera, camera group,
// registered person and one recognition event.
type foreignWorld struct {
	tenant, site, server, camera, group uuid.UUID
	person                              faces.Person
	event                               uuid.UUID
}

func (sf *syncFixture) foreignWorld(personName string) foreignWorld {
	sf.t.Helper()
	w := foreignWorld{tenant: sf.otherTenant(), site: uuid.New(), server: uuid.New(), camera: uuid.New(), group: uuid.New()}
	sf.exec(`INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, 'Foreign site')`, w.site, w.tenant)
	sf.exec(`INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url, capabilities)
	         VALUES ($1, $2, $3, 'foreign-server', 'http://example.invalid', '{"face_recognition": true}'::jsonb)`, w.server, w.tenant, w.site)
	sf.exec(`INSERT INTO cameras (id, tenant_id, site_id, server_id, remote_name, display_name)
	         VALUES ($1, $2, $3, $4, 'cam1', 'Foreign camera')`, w.camera, w.tenant, w.site, w.server)
	sf.exec(`INSERT INTO camera_groups (id, tenant_id, name) VALUES ($1, $2, 'Foreign group')`, w.group, w.tenant)
	sf.exec(`INSERT INTO camera_group_members (group_id, camera_id, tenant_id) VALUES ($1, $2, $3)`, w.group, w.camera, w.tenant)
	mgr := sf.user(w.tenant, "foreign-mgr", authz.FacesManage, authz.FacesView)
	p, err := sf.svc.CreatePerson(context.Background(), mgr, w.tenant, faces.PersonInput{DisplayName: personName})
	if err != nil {
		sf.t.Fatal(err)
	}
	w.person = p
	sf.queryRow(`INSERT INTO events (tenant_id, site_id, server_id, camera_id, remote_id, severity, sub_labels, start_time)
	             VALUES ($1, $2, $3, $4, 'foreign-1', 'alert', $5, now() - interval '30 seconds') RETURNING id`, &w.event,
		w.tenant, w.site, w.server, w.camera, []string{p.FrigateName})
	return w
}

func TestRecognitionsWithoutTenantAreScopedBySiteServerGroupAndCamera(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	sf.grantTenant(sf.manager.UserID, authz.EventsView)
	ana := sf.person("Ana")
	now := time.Now().UTC().Truncate(time.Millisecond)
	own := sf.event("frigate-h01/acceso_norte", "own-1", now.Add(-time.Minute), ana.FrigateName)
	w := sf.foreignWorld("Bea")

	// A platform administrator needs no tenant and sees every tenant it may see.
	all, err := sf.svc.ListRecognitions(ctx, sf.env.Admin, faces.RecognitionFilter{})
	if err != nil || len(all.Items) != 2 {
		t.Fatalf("admin without tenant = %+v, %v; want both tenants", all.Items, err)
	}
	names := map[string]bool{all.Items[0].PersonName: true, all.Items[1].PersonName: true}
	if !names["Ana"] || !names["Bea"] {
		t.Fatalf("admin sees names %v", names)
	}

	cases := map[string]struct {
		f    faces.RecognitionFilter
		want uuid.UUID
	}{
		"site":   {faces.RecognitionFilter{SiteIDs: []uuid.UUID{w.site}}, w.event},
		"server": {faces.RecognitionFilter{ServerIDs: []uuid.UUID{sf.h01ID}}, own},
		"group":  {faces.RecognitionFilter{CameraGroupIDs: []uuid.UUID{w.group}}, w.event},
		"camera": {faces.RecognitionFilter{CameraIDs: []uuid.UUID{sf.env.Cameras["frigate-h01/acceso_norte"].ID}}, own},
	}
	for name, c := range cases {
		page, err := sf.svc.ListRecognitions(ctx, sf.env.Admin, c.f)
		if err != nil || len(page.Items) != 1 || page.Items[0].EventID != c.want {
			t.Errorf("%s filter = %+v, %v; want only %v", name, page.Items, err, c.want)
		}
	}

	// A member is limited to its own tenant: no foreign rows, whatever the filters name.
	mine, err := sf.svc.ListRecognitions(ctx, sf.manager, faces.RecognitionFilter{})
	if err != nil || len(mine.Items) != 1 || mine.Items[0].EventID != own {
		t.Fatalf("member without tenant = %+v, %v; want only its own", mine.Items, err)
	}
	probe, err := sf.svc.ListRecognitions(ctx, sf.manager, faces.RecognitionFilter{SiteIDs: []uuid.UUID{w.site}, CameraGroupIDs: []uuid.UUID{w.group}})
	if err != nil || len(probe.Items) != 0 {
		t.Fatalf("member probing a foreign site = %+v, %v", probe.Items, err)
	}
	if _, err := sf.svc.ListRecognitions(ctx, sf.manager, faces.RecognitionFilter{TenantID: &w.tenant}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("member naming a foreign tenant = %v, want not found", err)
	}
	// An explicit tenant still narrows a platform administrator.
	one, err := sf.svc.ListRecognitions(ctx, sf.env.Admin, faces.RecognitionFilter{TenantID: &w.tenant})
	if err != nil || len(one.Items) != 1 || one.Items[0].EventID != w.event {
		t.Fatalf("admin with tenant_id = %+v, %v", one.Items, err)
	}
}

func TestTrainAttemptsListEveryVisibleServerAndFilterBySiteAndServer(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	w := sf.foreignWorld("Bea")
	sf.svc.Adapters = failingSource{inner: inventory.NewAdapters(sf.env.Svc), fail: w.server}
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.h01.AddFaceTrainImage(trainFile, pngOf(t, 9))

	all, err := sf.svc.ListTrainAttempts(ctx, sf.env.Admin, faces.TrainFilter{})
	if err != nil || len(all.Servers) != 2 || len(all.Attempts) != 1 {
		t.Fatalf("admin train list = %+v, %v; want both tenants' servers", all, err)
	}
	bySite, err := sf.svc.ListTrainAttempts(ctx, sf.env.Admin, faces.TrainFilter{SiteIDs: []uuid.UUID{w.site}})
	if err != nil || len(bySite.Servers) != 1 || bySite.Servers[0].ServerID != w.server {
		t.Fatalf("site filter = %+v, %v", bySite.Servers, err)
	}
	byServer, err := sf.svc.ListTrainAttempts(ctx, sf.env.Admin, faces.TrainFilter{ServerIDs: []uuid.UUID{sf.h01ID}})
	if err != nil || len(byServer.Servers) != 1 || len(byServer.Attempts) != 1 {
		t.Fatalf("server filter = %+v, %v", byServer, err)
	}
	// A member never lists the servers of another tenant, with or without naming it.
	mine, err := sf.svc.ListTrainAttempts(ctx, sf.manager, faces.TrainFilter{})
	if err != nil || len(mine.Servers) != 1 || mine.Servers[0].ServerID != sf.h01ID {
		t.Fatalf("member train list = %+v, %v", mine.Servers, err)
	}
	if _, err := sf.svc.ListTrainAttempts(ctx, sf.manager, faces.TrainFilter{TenantID: &w.tenant}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("member naming a foreign tenant = %v, want not found", err)
	}
}

func TestObjectTenantResolution(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	w := sf.foreignWorld("Bea")
	wl, err := sf.svc.CreateWatchlist(ctx, sf.manager, sf.tenant, faces.WatchlistInput{Name: "VIP"})
	if err != nil {
		t.Fatal(err)
	}
	ana := sf.person("Ana")

	if got, err := sf.svc.PersonTenant(ctx, sf.env.Admin, w.person.ID); err != nil || got != w.tenant {
		t.Errorf("PersonTenant(admin, foreign person) = %v, %v", got, err)
	}
	if got, err := sf.svc.WatchlistTenant(ctx, sf.env.Admin, wl.ID); err != nil || got != sf.tenant {
		t.Errorf("WatchlistTenant(admin) = %v, %v", got, err)
	}
	if got, err := sf.svc.ServerTenant(ctx, sf.env.Admin, w.server); err != nil || got != w.tenant {
		t.Errorf("ServerTenant(admin) = %v, %v", got, err)
	}
	// A member cannot even tell that another tenant's object exists.
	if _, err := sf.svc.PersonTenant(ctx, sf.manager, w.person.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("PersonTenant(member, foreign) = %v, want not found", err)
	}
	if _, err := sf.svc.ServerTenant(ctx, sf.manager, w.server); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ServerTenant(member, foreign) = %v, want not found", err)
	}
	if got, err := sf.svc.PersonTenant(ctx, sf.manager, ana.ID); err != nil || got != sf.tenant {
		t.Errorf("PersonTenant(member, own) = %v, %v", got, err)
	}
	if _, err := sf.svc.PersonTenant(ctx, sf.env.Admin, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown person = %v, want not found", err)
	}

	// The default tenant of a platform administrator exists only while exactly one does.
	if _, err := sf.svc.SoleTenant(ctx, sf.env.Admin); err == nil {
		t.Error("SoleTenant with two tenants succeeded")
	} else {
		var ve *faces.ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("SoleTenant with two tenants = %v, want validation error", err)
		}
	}
	sf.exec(`UPDATE tenants SET deleted_at = now() WHERE id = $1`, w.tenant)
	if got, err := sf.svc.SoleTenant(ctx, sf.env.Admin); err != nil || got != sf.tenant {
		t.Errorf("SoleTenant with one tenant = %v, %v", got, err)
	}
}
