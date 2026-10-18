//go:build integration

package faces_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/faces"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func (sf *syncFixture) grantCamera(userID, cameraID uuid.UUID, p authz.Permission) {
	sf.t.Helper()
	if _, err := sf.env.Svc.CreateGrant(context.Background(), sf.env.Admin, inventory.GrantInput{
		SubjectType: "user", SubjectID: userID, Permission: p, Effect: authz.Allow, ScopeType: authz.ScopeCamera, ScopeID: &cameraID,
	}); err != nil {
		sf.t.Fatal(err)
	}
}

func (sf *syncFixture) grantTenant(userID uuid.UUID, p authz.Permission) {
	sf.t.Helper()
	if _, err := sf.env.Svc.CreateGrant(context.Background(), sf.env.Admin, inventory.GrantInput{
		SubjectType: "user", SubjectID: userID, Permission: p, Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &sf.tenant,
	}); err != nil {
		sf.t.Fatal(err)
	}
}

func (sf *syncFixture) event(cameraKey, remote string, start time.Time, labels ...string) uuid.UUID {
	sf.t.Helper()
	cam := sf.env.Cameras[cameraKey]
	var id uuid.UUID
	sf.queryRow(`INSERT INTO events (tenant_id, site_id, server_id, camera_id, remote_id, severity, sub_labels, start_time)
	             VALUES ($1, $2, $3, $4, $5, 'alert', $6, $7) RETURNING id`, &id,
		cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, remote, labels, start)
	return id
}

func TestReadsAreAuditedAndListCarriesSyncSummary(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	sf.setCapability(sf.h01ID, sf.h01, true)
	ana := sf.person("Ana", 1)
	sf.run()

	list, err := sf.svc.ListPersons(ctx, sf.manager, sf.tenant)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
	// Two servers in the demo tenant: h01 synced, c01 face-disabled.
	got := list[0].Sync
	if got.Total != 2 || got.Synced != 1 || got.Disabled != 1 || got.Failed != 0 {
		t.Fatalf("sync summary = %+v, want total 2 / synced 1 / disabled 1", got)
	}
	if _, err := sf.svc.GetPerson(ctx, sf.manager, sf.tenant, ana.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := sf.svc.ListImages(ctx, sf.manager, sf.tenant, ana.ID); err != nil {
		t.Fatal(err)
	}
	if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1`, faces.ActionPersonRead); n != 3 {
		t.Fatalf("person read audit rows = %d, want 3 (list, get, images)", n)
	}
	if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1 AND details::text LIKE '%Ana%'`, faces.ActionPersonRead); n != 0 {
		t.Fatal("audit details carry the person's name")
	}
}

func TestRecognitionsTimelineAuthzIsolationAndPaging(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	sf.grantTenant(sf.manager.UserID, authz.EventsView)
	ana := sf.person("Ana")
	beto := sf.person("Beto")
	now := time.Now().UTC().Truncate(time.Millisecond)
	e1 := sf.event("frigate-h01/acceso_norte", "r1", now.Add(-3*time.Minute), ana.FrigateName)
	_ = sf.event("frigate-h01/plaza", "r2", now.Add(-2*time.Minute), ana.FrigateName, beto.FrigateName)
	e3 := sf.event("frigate-c01/muelle", "r3", now.Add(-time.Minute), beto.FrigateName)
	sf.event("frigate-h01/plaza", "r4", now, "unrelated")

	all, err := sf.svc.ListRecognitions(ctx, sf.manager, sf.tenant, faces.RecognitionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	// e2 yields one row per recognized person.
	if len(all.Items) != 4 || all.Items[0].EventID != e3 {
		t.Fatalf("recognitions = %d rows (first %v), want 4 newest first", len(all.Items), all.Items[0].EventID)
	}
	if all.Items[0].PersonName != "Beto" || all.Items[0].CameraName == "" || all.Items[0].SiteName == "" {
		t.Fatalf("row not enriched: %+v", all.Items[0])
	}

	onlyAna, err := sf.svc.ListRecognitions(ctx, sf.manager, sf.tenant, faces.RecognitionFilter{PersonIDs: []uuid.UUID{ana.ID}})
	if err != nil || len(onlyAna.Items) != 2 || onlyAna.Items[0].EventID == e1 || onlyAna.Items[1].EventID != e1 {
		t.Fatalf("per-person timeline = %+v, %v", onlyAna.Items, err)
	}
	cam := sf.env.Cameras["frigate-h01/plaza"].ID
	byCam, err := sf.svc.ListRecognitions(ctx, sf.manager, sf.tenant, faces.RecognitionFilter{CameraIDs: []uuid.UUID{cam}})
	if err != nil || len(byCam.Items) != 2 {
		t.Fatalf("camera filter = %+v, %v", byCam.Items, err)
	}
	from := now.Add(-150 * time.Second)
	byTime, err := sf.svc.ListRecognitions(ctx, sf.manager, sf.tenant, faces.RecognitionFilter{From: &from})
	if err != nil || len(byTime.Items) != 3 {
		t.Fatalf("time filter = %+v, %v", byTime.Items, err)
	}

	// Keyset paging walks every row exactly once, including the two rows of one event.
	var seen []string
	cursor := ""
	for i := 0; i < 6; i++ {
		page, err := sf.svc.ListRecognitions(ctx, sf.manager, sf.tenant, faces.RecognitionFilter{Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range page.Items {
			seen = append(seen, it.EventID.String()+it.PersonID.String())
		}
		if cursor = page.Next; cursor == "" {
			break
		}
	}
	if len(seen) != 4 {
		t.Fatalf("paging returned %d rows, want 4", len(seen))
	}
	if _, err := sf.svc.ListRecognitions(ctx, sf.manager, sf.tenant, faces.RecognitionFilter{Cursor: "garbage"}); !errors.Is(err, faces.ErrInvalidCursor) {
		t.Fatalf("bad cursor = %v", err)
	}

	// Camera authz: faces.view alone shows nothing; events.view on one camera shows only that camera.
	viewer := sf.user(sf.tenant, "viewer", authz.FacesView)
	none, err := sf.svc.ListRecognitions(ctx, viewer, sf.tenant, faces.RecognitionFilter{})
	if err != nil || len(none.Items) != 0 {
		t.Fatalf("no camera access = %+v, %v", none.Items, err)
	}
	sf.grantCamera(viewer.UserID, sf.env.Cameras["frigate-c01/muelle"].ID, authz.EventsView)
	one, err := sf.svc.ListRecognitions(ctx, viewer, sf.tenant, faces.RecognitionFilter{})
	if err != nil || len(one.Items) != 1 || one.Items[0].EventID != e3 {
		t.Fatalf("camera-scoped viewer = %+v, %v", one.Items, err)
	}
	// Without faces.view the timeline is forbidden, and another tenant sees nothing of this one.
	blind := sf.user(sf.tenant, "blind")
	if _, err := sf.svc.ListRecognitions(ctx, blind, sf.tenant, faces.RecognitionFilter{}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("without faces.view = %v, want forbidden", err)
	}
	other := sf.otherTenant()
	stranger := sf.user(other, "stranger", authz.FacesView)
	if _, err := sf.svc.ListRecognitions(ctx, stranger, sf.tenant, faces.RecognitionFilter{}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross tenant = %v, want not found", err)
	}
	mine, err := sf.svc.ListRecognitions(ctx, stranger, other, faces.RecognitionFilter{})
	if err != nil || len(mine.Items) != 0 {
		t.Fatalf("other tenant sees %+v, %v", mine.Items, err)
	}
	if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1`, faces.ActionRecognitionsRead); n == 0 {
		t.Fatal("recognition reads were not audited")
	}
}

const trainFile = "1700000000.1-abc123-1700000100-unknown-0.41.webp"

func TestTrainAttemptsListImageAndAssign(t *testing.T) {
	sf := newSyncFixture(t)
	sf.svc.Adapters = inventory.NewAdapters(sf.env.Svc)
	ctx := context.Background()
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.h01.AddFaceTrainImage(trainFile, pngOf(t, 9))
	sf.h01.AddFaceTrainImage("1700000000.2-def456-1700000200-ana-0.55.webp", pngOf(t, 10))
	// A server without face recognition is not asked at all.
	sf.c01.AddFaceTrainImage("ignored.webp", pngOf(t, 11))

	list, err := sf.svc.ListTrainAttempts(ctx, sf.manager, sf.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Attempts) != 2 || len(list.Servers) != 1 || list.Servers[0].ServerID != sf.h01ID || list.Servers[0].Error != "" {
		t.Fatalf("train list = %+v", list)
	}
	first := list.Attempts[0]
	if first.File != "1700000000.2-def456-1700000200-ana-0.55.webp" || first.Score == nil || *first.Score != 0.55 || first.SeenAt == nil || first.SeenAt.Unix() != 1700000200 {
		t.Fatalf("newest attempt not parsed: %+v", first)
	}

	img, ct, err := sf.svc.TrainAttemptImage(ctx, sf.manager, sf.tenant, sf.h01ID, trainFile)
	if err != nil || ct == "" || len(img) == 0 {
		t.Fatalf("train image = %d bytes %q %v", len(img), ct, err)
	}
	if _, _, err := sf.svc.TrainAttemptImage(ctx, sf.manager, sf.tenant, sf.h01ID, "../secret"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if _, _, err := sf.svc.TrainAttemptImage(ctx, sf.manager, sf.tenant, sf.h01ID, "nope.webp"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing attempt = %v", err)
	}
	stranger := sf.user(sf.otherTenant(), "stranger", authz.FacesView, authz.FacesManage)
	if _, _, err := sf.svc.TrainAttemptImage(ctx, stranger, sf.tenant, sf.h01ID, trainFile); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross tenant image = %v", err)
	}
	viewer := sf.user(sf.tenant, "viewer", authz.FacesView)
	if _, err := sf.svc.AssignTrainAttempt(ctx, viewer, sf.tenant, faces.AssignInput{ServerID: sf.h01ID, File: trainFile, NewPersonName: "Carla"}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("assign without faces.manage = %v", err)
	}
	if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1`, faces.ActionTrainRead); n < 2 {
		t.Fatalf("train reads audited = %d, want list and image", n)
	}

	// Assigning to a new person adds the attempt centrally and distributes it by the sync.
	res, err := sf.svc.AssignTrainAttempt(ctx, sf.manager, sf.tenant, faces.AssignInput{ServerID: sf.h01ID, File: trainFile, NewPersonName: "Carla"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Person.DisplayName != "Carla" || res.Image.SizeBytes == 0 || !res.AttemptRemoved {
		t.Fatalf("assign result = %+v", res)
	}
	stored, _, err := sf.svc.ReadImage(ctx, sf.manager, sf.tenant, res.Person.ID, res.Image.ID)
	if err != nil || string(stored) != string(pngOf(t, 9)) {
		t.Fatalf("central copy differs from the attempt (%v)", err)
	}
	sf.run()
	sameBytes(t, libraryBytes(sf.h01, res.Person.FrigateName), pngOf(t, 9))
	list, _ = sf.svc.ListTrainAttempts(ctx, sf.manager, sf.tenant)
	if len(list.Attempts) != 1 {
		t.Fatalf("attempts after assign = %d, want 1", len(list.Attempts))
	}
	if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1 AND details::text NOT LIKE '%abc123%' AND details::text NOT LIKE '%Carla%'`, faces.ActionTrainAssigned); n != 1 {
		t.Fatalf("assign audit rows without file or name = %d, want 1", n)
	}

	// Assigning to an existing person; an attempt that is not an image changes nothing.
	ana := sf.person("Ana")
	sf.h01.AddFaceTrainImage("bad.webp", []byte("not an image"))
	if _, err := sf.svc.AssignTrainAttempt(ctx, sf.manager, sf.tenant, faces.AssignInput{ServerID: sf.h01ID, File: "bad.webp", PersonID: &ana.ID}); err == nil {
		t.Fatal("non-image attempt accepted")
	} else {
		var ve *faces.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("non-image attempt = %v, want validation error", err)
		}
	}
	if imgs, _ := sf.svc.ListImages(ctx, sf.manager, sf.tenant, ana.ID); len(imgs) != 0 {
		t.Fatal("a rejected attempt left an image behind")
	}
	if _, err := sf.svc.AssignTrainAttempt(ctx, sf.manager, sf.tenant, faces.AssignInput{ServerID: sf.h01ID, File: "1700000000.2-def456-1700000200-ana-0.55.webp", PersonID: &ana.ID}); err != nil {
		t.Fatal(err)
	}
	if imgs, _ := sf.svc.ListImages(ctx, sf.manager, sf.tenant, ana.ID); len(imgs) != 1 {
		t.Fatalf("images of Ana = %d", len(imgs))
	}
	// Exactly one target is required.
	if _, err := sf.svc.AssignTrainAttempt(ctx, sf.manager, sf.tenant, faces.AssignInput{ServerID: sf.h01ID, File: "bad.webp"}); err == nil {
		t.Fatal("assign without a target accepted")
	}
}

type failingSource struct {
	inner faces.AdapterSource
	fail  uuid.UUID
}

func (f failingSource) Get(ctx context.Context, srv db.FrigateServer) (frigate.Adapter, error) {
	if srv.ID == f.fail {
		return nil, errors.New("dial tcp 10.0.0.9:5000: connection refused")
	}
	return f.inner.Get(ctx, srv)
}

func TestTrainListReportsUnreachableServersWithoutFailing(t *testing.T) {
	sf := newSyncFixture(t)
	sf.svc.Adapters = failingSource{inner: inventory.NewAdapters(sf.env.Svc), fail: sf.c01ID}
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.setCapability(sf.c01ID, sf.c01, true)
	sf.h01.AddFaceTrainImage(trainFile, pngOf(t, 9))
	list, err := sf.svc.ListTrainAttempts(context.Background(), sf.manager, sf.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Attempts) != 1 || len(list.Servers) != 2 {
		t.Fatalf("list = %+v", list)
	}
	for _, s := range list.Servers {
		switch {
		case s.ServerID == sf.c01ID && s.Error == "":
			t.Error("unreachable server reported no error")
		case s.ServerID == sf.h01ID && s.Error != "":
			t.Errorf("healthy server reported %q", s.Error)
		case strings.Contains(s.Error, "10.0.0.9"):
			t.Errorf("error leaks the address: %q", s.Error)
		}
	}
}

// A new person created for an attempt must not outlive a failed image add: no orphan person.
func TestAssignToNewPersonLeavesNoOrphanWhenStorageFails(t *testing.T) {
	sf := newSyncFixture(t)
	sf.svc.Adapters = inventory.NewAdapters(sf.env.Svc)
	ctx := context.Background()
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.h01.AddFaceTrainImage(trainFile, pngOf(t, 9))
	sf.blobs.failPut = true
	if _, err := sf.svc.AssignTrainAttempt(ctx, sf.manager, sf.tenant, faces.AssignInput{ServerID: sf.h01ID, File: trainFile, NewPersonName: "Dora"}); err == nil {
		t.Fatal("assign succeeded although the image could not be stored")
	}
	sf.blobs.failPut = false
	persons, err := sf.svc.ListPersons(ctx, sf.manager, sf.tenant)
	if err != nil || len(persons) != 0 {
		t.Fatalf("persons after a failed assign = %+v, %v; want none", persons, err)
	}
	// The attempt is untouched and the name is free again.
	if _, err := sf.svc.AssignTrainAttempt(ctx, sf.manager, sf.tenant, faces.AssignInput{ServerID: sf.h01ID, File: trainFile, NewPersonName: "Dora"}); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
}
