//go:build integration

package events_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// createCameraGroup creates a camera group with the given members, acting as the platform admin
// (who already holds cameras.manage everywhere).
func createCameraGroup(t *testing.T, env *demofix.Env, tenantID uuid.UUID, name string, cameraIDs []uuid.UUID) uuid.UUID {
	t.Helper()
	g, err := env.Svc.CreateCameraGroup(context.Background(), env.Admin, inventory.CameraGroupInput{
		TenantID: tenantID, Name: name, CameraIDs: cameraIDs,
	})
	if err != nil {
		t.Fatalf("create camera group %q: %v", name, err)
	}
	return g.ID
}

// TestCameraGroupFilterCorrectness proves the camera_group filter on ListEvents and ListPlates
// (PRD §44) narrows results to cameras belonging to the given group(s), stays intersected with
// (never widens) the actor's permitted cameras, and never leaks another tenant's group.
func TestCameraGroupFilterCorrectness(t *testing.T) {
	env, syncer, svc := setup(t)
	ctx := context.Background()
	syncer.SyncAll(ctx)

	camA := env.Cameras["frigate-h01/acceso_norte"] // LPR-enabled
	camB := env.Cameras["frigate-c01/ruta_1"]        // LPR-enabled, different server
	camC := env.Cameras["frigate-h01/plaza"]         // member of no test group

	groupA := createCameraGroup(t, env, env.Demo.TenantID, "group-a", []uuid.UUID{camA.ID})
	groupAB := createCameraGroup(t, env, env.Demo.TenantID, "group-ab", []uuid.UUID{camA.ID, camB.ID})

	t.Run("events: camera_group filter finds only events of cameras in the group", func(t *testing.T) {
		page, err := svc.ListEvents(ctx, env.Admin, events.Filter{CameraGroupIDs: []uuid.UUID{groupA}, Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 0 {
			t.Fatalf("camera_group %s found no events", groupA)
		}
		for _, e := range page.Items {
			if e.CameraID != camA.ID {
				t.Errorf("event %s carries camera %s, want only %s (group has one member)", e.ID, e.CameraID, camA.ID)
			}
		}
	})

	t.Run("events: a group excludes cameras that are not its members even when the actor can see them", func(t *testing.T) {
		// groupA only has camA as a member: even though the admin can see camB too, filtering by
		// groupA must never return camB's events.
		page, err := svc.ListEvents(ctx, env.Admin, events.Filter{CameraGroupIDs: []uuid.UUID{groupA}, Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Items {
			if e.CameraID == camB.ID {
				t.Errorf("group %s (camA only) leaked an event of camB %s", groupA, e.ID)
			}
		}
	})

	t.Run("events: a group containing a denied camera never leaks it (never widens access)", func(t *testing.T) {
		u, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "group-scoped-camA-only")
		if err != nil {
			t.Fatal(err)
		}
		grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camA.ID, authz.EventsView)
		scoped := u

		// groupAB contains both camA (permitted) and camB (denied to this actor).
		page, err := svc.ListEvents(ctx, scoped, events.Filter{CameraGroupIDs: []uuid.UUID{groupAB}, Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 0 {
			t.Fatalf("scoped actor with events.view on camA found no events for group %s", groupAB)
		}
		for _, e := range page.Items {
			if e.CameraID != camA.ID {
				t.Errorf("scoped actor without access to camB got event %s of camera %s via group %s, want only camA %s",
					e.ID, e.CameraID, groupAB, camA.ID)
			}
		}
	})

	t.Run("events: a camera outside the requested group never matches", func(t *testing.T) {
		page, err := svc.ListEvents(ctx, env.Admin, events.Filter{CameraIDs: []uuid.UUID{camC.ID}, CameraGroupIDs: []uuid.UUID{groupA}, Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 {
			t.Errorf("camera %s outside group %s matched %d events, want 0", camC.RemoteName, groupA, len(page.Items))
		}
	})

	t.Run("events: another tenant's camera_group id yields nothing", func(t *testing.T) {
		tenantB, err := env.Svc.CreateTenant(ctx, env.Admin, "camera-group-cross-tenant-events", "Camera Group Cross Tenant Events")
		if err != nil {
			t.Fatal(err)
		}
		// What matters is that tenant A cannot use tenant B's group id to find or widen
		// anything through it; the group's own membership is irrelevant to that.
		foreignGroup := createCameraGroup(t, env, tenantB.ID, "foreign-group", nil)

		page, err := svc.ListEvents(ctx, env.Admin, events.Filter{CameraGroupIDs: []uuid.UUID{foreignGroup}, Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 {
			t.Errorf("cross-tenant camera_group %s matched %d events, want 0", foreignGroup, len(page.Items))
		}
	})

	t.Run("plates: camera_group filter finds only reads of cameras in the group", func(t *testing.T) {
		page, err := svc.ListPlates(ctx, env.Admin, events.PlateFilter{CameraGroupIDs: []uuid.UUID{groupA}, Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 0 {
			t.Fatalf("camera_group %s found no plate reads", groupA)
		}
		for _, r := range page.Items {
			if r.CameraID != camA.ID {
				t.Errorf("plate read %s carries camera %s, want only %s", r.ID, r.CameraID, camA.ID)
			}
		}
	})

	t.Run("plates: another tenant's camera_group id yields nothing", func(t *testing.T) {
		tenantB, err := env.Svc.CreateTenant(ctx, env.Admin, "camera-group-cross-tenant-plates", "Camera Group Cross Tenant Plates")
		if err != nil {
			t.Fatal(err)
		}
		foreignGroup := createCameraGroup(t, env, tenantB.ID, "foreign-group-plates", nil)

		page, err := svc.ListPlates(ctx, env.Admin, events.PlateFilter{CameraGroupIDs: []uuid.UUID{foreignGroup}, Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 {
			t.Errorf("cross-tenant camera_group %s matched %d plate reads, want 0", foreignGroup, len(page.Items))
		}
	})
}
