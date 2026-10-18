//go:build integration

package events_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/faces"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func grantTenantPermission(t *testing.T, env *demofix.Env, tenantID, userID uuid.UUID, p authz.Permission) {
	t.Helper()
	if _, err := env.Svc.CreateGrant(context.Background(), env.Admin, inventory.GrantInput{
		SubjectType: "user", SubjectID: userID, Permission: p, Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenantID,
	}); err != nil {
		t.Fatalf("grant %s: %v", p, err)
	}
}

func setSubLabels(t *testing.T, env *demofix.Env, camID uuid.UUID, labels ...string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `
UPDATE events SET sub_labels = $1 WHERE id = (SELECT id FROM events WHERE camera_id = $2 ORDER BY start_time DESC LIMIT 1) RETURNING id`,
			labels, camID).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func hasLabel(items []events.Event, id uuid.UUID, label string) (found, present bool) {
	for _, e := range items {
		if e.ID == id {
			return true, slices.Contains(e.SubLabels, label)
		}
	}
	return false, false
}

// Without faces.view a registered person's name is redacted from sub_labels (list, detail and
// the sub_label filter), other labels stay, and registrations of another tenant never redact.
func TestFaceNamesAreRedactedWithoutFacesView(t *testing.T) {
	env, syncer, svc := setup(t)
	ctx := context.Background()
	syncer.SyncAll(ctx)
	tenant := env.Demo.TenantID
	cam := env.Cameras["frigate-h01/plaza"]

	mgr, _, err := bootstrap.TenantUser(ctx, env.Store, tenant, "face-mgr")
	if err != nil {
		t.Fatal(err)
	}
	grantTenantPermission(t, env, tenant, mgr.UserID, authz.FacesManage)
	fsvc := &faces.Service{Store: env.Store, Log: pgtest.Discard()}
	ana, err := fsvc.CreatePerson(ctx, mgr, tenant, faces.PersonInput{DisplayName: "Ana Perez"})
	if err != nil {
		t.Fatal(err)
	}
	evID := setSubLabels(t, env, cam.ID, ana.FrigateName, "face-jane-doe")

	viewer, _, err := bootstrap.TenantUser(ctx, env.Store, tenant, "ev-viewer")
	if err != nil {
		t.Fatal(err)
	}
	grantCameraPermission(t, env, tenant, viewer.UserID, cam.ID, authz.EventsView)
	grantCameraPermission(t, env, tenant, viewer.UserID, cam.ID, authz.EventsSearch)

	t.Run("detail redacts the name and keeps other labels", func(t *testing.T) {
		got, err := svc.GetEvent(ctx, viewer, evID)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.SubLabels, []string{"face-jane-doe"}) {
			t.Fatalf("sub_labels = %v, want only the unrelated label", got.SubLabels)
		}
	})
	t.Run("list redacts the name", func(t *testing.T) {
		page, err := svc.ListEvents(ctx, viewer, events.Filter{CameraIDs: []uuid.UUID{cam.ID}, Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		found, present := hasLabel(page.Items, evID, ana.FrigateName)
		if !found || present {
			t.Fatalf("event listed=%v name visible=%v, want listed with the name redacted", found, present)
		}
	})
	t.Run("the sub_label filter cannot probe the name", func(t *testing.T) {
		for _, filter := range [][]string{{ana.FrigateName}, {ana.FrigateName, "no-such-label"}} {
			page, err := svc.ListEvents(ctx, viewer, events.Filter{CameraIDs: []uuid.UUID{cam.ID}, SubLabels: filter, Limit: 500})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 0 {
				t.Fatalf("filter %v matched %d events without faces.view", filter, len(page.Items))
			}
		}
		page, err := svc.ListEvents(ctx, viewer, events.Filter{CameraIDs: []uuid.UUID{cam.ID}, SubLabels: []string{"face-jane-doe", ana.FrigateName}, Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if found, _ := hasLabel(page.Items, evID, ""); !found {
			t.Fatal("the unrelated label in the same filter stopped matching")
		}
	})

	t.Run("faces.view reveals and searches the name", func(t *testing.T) {
		grantTenantPermission(t, env, tenant, viewer.UserID, authz.FacesView)
		got, err := svc.GetEvent(ctx, viewer, evID)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(got.SubLabels, ana.FrigateName) {
			t.Fatalf("sub_labels = %v, want the name visible with faces.view", got.SubLabels)
		}
		page, err := svc.ListEvents(ctx, viewer, events.Filter{CameraIDs: []uuid.UUID{cam.ID}, SubLabels: []string{ana.FrigateName}, Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if found, present := hasLabel(page.Items, evID, ana.FrigateName); !found || !present {
			t.Fatal("name not searchable with faces.view")
		}
	})
}

func TestFaceNameOfAnotherTenantDoesNotRedact(t *testing.T) {
	env, syncer, svc := setup(t)
	ctx := context.Background()
	syncer.SyncAll(ctx)
	cam := env.Cameras["frigate-h01/plaza"]

	var other uuid.UUID
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO tenants (slug, name) VALUES ('other-face', 'o') RETURNING id`).Scan(&other); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO face_persons (tenant_id, display_name, frigate_name) VALUES ($1, 'Ana', 'ana')`, other)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	evID := setSubLabels(t, env, cam.ID, "ana")
	viewer, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "ev-viewer2")
	if err != nil {
		t.Fatal(err)
	}
	grantCameraPermission(t, env, env.Demo.TenantID, viewer.UserID, cam.ID, authz.EventsView)
	got, err := svc.GetEvent(ctx, viewer, evID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.SubLabels, []string{"ana"}) {
		t.Fatalf("sub_labels = %v, want the label kept (the person belongs to another tenant)", got.SubLabels)
	}
}
