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

// grantCameraPermission grants a single camera-scoped permission to userID in tenantID, acting
// as the platform admin (who already holds every permission everywhere).
func grantCameraPermission(t *testing.T, env *demofix.Env, tenantID, userID, cameraID uuid.UUID, p authz.Permission) {
	t.Helper()
	ctx := context.Background()
	if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
		SubjectType: "user", SubjectID: userID, Permission: p,
		Effect: authz.Allow, ScopeType: authz.ScopeCamera, ScopeID: &cameraID,
	}); err != nil {
		t.Fatalf("grant %s on camera %s: %v", p, cameraID, err)
	}
}

// firstPlateOnCamera returns a plate carried by an event of camID, as seen with full access.
// The demo Frigates always enrich every review of an LPR-tagged camera with a recognized plate
// (internal/frigatemock/generator.go, enrich), so this never returns empty for an LPR camera.
func firstPlateOnCamera(t *testing.T, env *demofix.Env, svc *events.Service, camID uuid.UUID) string {
	t.Helper()
	page, err := svc.ListEvents(context.Background(), env.Admin, events.Filter{CameraIDs: []uuid.UUID{camID}, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Items {
		if len(e.Plates) > 0 {
			return e.Plates[0]
		}
	}
	t.Fatalf("camera %s has no event carrying a plate", camID)
	return ""
}

// TestPlateFilterCorrectness proves the plate filter on ListEvents keeps its
// camera-permission-scoped, partial-substring semantics: it finds events by
// a partial plate on a permitted camera, finds none for a plate that does not exist, and never
// leaks plates of a camera the actor was not granted lpr.search on (PRD §31/§45).
func TestPlateFilterCorrectness(t *testing.T) {
	env, syncer, svc := setup(t)
	ctx := context.Background()
	syncer.SyncAll(ctx)

	camA := env.Cameras["frigate-h01/acceso_norte"] // LPR-enabled
	camB := env.Cameras["frigate-c01/ruta_1"]       // LPR-enabled, different server

	plateA := firstPlateOnCamera(t, env, svc, camA.ID)
	plateB := firstPlateOnCamera(t, env, svc, camB.ID)

	t.Run("partial match finds events on a permitted camera", func(t *testing.T) {
		sub := plateA[1 : len(plateA)-1]
		page, err := svc.ListEvents(ctx, env.Admin, events.Filter{Plate: sub})
		if err != nil {
			t.Fatal(err)
		}
		hit := false
		for _, e := range page.Items {
			for _, p := range e.Plates {
				hit = hit || p == plateA
			}
		}
		if !hit {
			t.Errorf("partial search %q did not find plate %s on camera %s", sub, plateA, camA.RemoteName)
		}
	})

	t.Run("a plate that does not exist finds nothing", func(t *testing.T) {
		page, err := svc.ListEvents(ctx, env.Admin, events.Filter{Plate: "NOSUCHPLATEXYZ999"})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 {
			t.Errorf("nonexistent plate matched %d events, want 0", len(page.Items))
		}
	})

	t.Run("denied camera plates stay hidden even under a scoped grant", func(t *testing.T) {
		u, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "plate-scoped")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []authz.Permission{authz.EventsSearch, authz.LPRSearch, authz.LPRView} {
			grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camA.ID, p)
		}
		scoped := u

		// Permitted camera: the scoped actor finds its own plate.
		own, err := svc.ListEvents(ctx, scoped, events.Filter{Plate: plateA})
		if err != nil {
			t.Fatal(err)
		}
		if len(own.Items) == 0 {
			t.Fatalf("scoped actor with lpr.search on camera %s found no event for its own plate %s", camA.RemoteName, plateA)
		}
		for _, e := range own.Items {
			if e.CameraID != camA.ID {
				t.Errorf("scoped actor's result carries camera %s, want only %s", e.CameraID, camA.ID)
			}
		}

		// Denied camera (no grant at all on camB): the scoped actor gets zero results even
		// though the plate genuinely exists, and even though the search is a partial LIKE.
		denied, err := svc.ListEvents(ctx, scoped, events.Filter{Plate: plateB})
		if err != nil {
			t.Fatal(err)
		}
		if len(denied.Items) != 0 {
			t.Errorf("scoped actor without access to camera %s found %d events for its plate %s, want 0",
				camB.RemoteName, len(denied.Items), plateB)
		}
	})
}
