//go:build integration

package events_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// firstZoneOnCamera returns a zone carried by an event of camID, as seen with full access.
// acceso_norte and ruta_1 are both seeded with a zone on every review (demofix.go), so this
// never returns empty for those cameras.
func firstZoneOnCamera(t *testing.T, env *demofix.Env, svc *events.Service, camID uuid.UUID) string {
	t.Helper()
	page, err := svc.ListEvents(context.Background(), env.Admin, events.Filter{CameraIDs: []uuid.UUID{camID}, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Items {
		if len(e.Zones) > 0 {
			return e.Zones[0]
		}
	}
	t.Fatalf("camera %s has no event carrying a zone", camID)
	return ""
}

// firstSubLabelOnCamera returns a sub_label carried by an event of camID, as seen with full
// access. The demo Frigates enrich every review of an LPR-tagged camera with a recognized-plate
// sub_label (internal/frigatemock/generator.go, enrich), so this never returns empty for camA/camB.
func firstSubLabelOnCamera(t *testing.T, env *demofix.Env, svc *events.Service, camID uuid.UUID) string {
	t.Helper()
	page, err := svc.ListEvents(context.Background(), env.Admin, events.Filter{CameraIDs: []uuid.UUID{camID}, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Items {
		if len(e.SubLabels) > 0 {
			return e.SubLabels[0]
		}
	}
	t.Fatalf("camera %s has no event carrying a sub_label", camID)
	return ""
}

// TestZoneAndSubLabelFilterCorrectness proves the zone and sub_label filters on ListEvents
// (PRD §44) find matching events, find nothing for values that do not exist, and never leak
// events of a camera the actor was not granted events.search on (PRD §31).
func TestZoneAndSubLabelFilterCorrectness(t *testing.T) {
	env, syncer, svc := setup(t)
	ctx := context.Background()
	syncer.SyncAll(ctx)

	camA := env.Cameras["frigate-h01/acceso_norte"] // zones: entrada, salida; LPR-enabled
	camB := env.Cameras["frigate-c01/ruta_1"]        // zone: ingreso; LPR-enabled, different server

	zoneA := firstZoneOnCamera(t, env, svc, camA.ID)
	zoneB := firstZoneOnCamera(t, env, svc, camB.ID)
	subLabelA := firstSubLabelOnCamera(t, env, svc, camA.ID)
	subLabelB := firstSubLabelOnCamera(t, env, svc, camB.ID)

	t.Run("zone match finds events on a permitted camera", func(t *testing.T) {
		page, err := svc.ListEvents(ctx, env.Admin, events.Filter{CameraIDs: []uuid.UUID{camA.ID}, Zones: []string{zoneA}})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 0 {
			t.Fatalf("zone %q found no events on camera %s", zoneA, camA.RemoteName)
		}
		for _, e := range page.Items {
			hit := false
			for _, z := range e.Zones {
				hit = hit || z == zoneA
			}
			if !hit {
				t.Errorf("event %s zones %v does not contain %q", e.ID, e.Zones, zoneA)
			}
		}
	})

	t.Run("a zone that does not exist finds nothing", func(t *testing.T) {
		page, err := svc.ListEvents(ctx, env.Admin, events.Filter{Zones: []string{"zona-inexistente-xyz"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 {
			t.Errorf("nonexistent zone matched %d events, want 0", len(page.Items))
		}
	})

	t.Run("sub_label match finds events on a permitted camera", func(t *testing.T) {
		page, err := svc.ListEvents(ctx, env.Admin, events.Filter{CameraIDs: []uuid.UUID{camA.ID}, SubLabels: []string{subLabelA}})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 0 {
			t.Fatalf("sub_label %q found no events on camera %s", subLabelA, camA.RemoteName)
		}
		for _, e := range page.Items {
			hit := false
			for _, s := range e.SubLabels {
				hit = hit || s == subLabelA
			}
			if !hit {
				t.Errorf("event %s sub_labels %v does not contain %q", e.ID, e.SubLabels, subLabelA)
			}
		}
	})

	t.Run("a sub_label that does not exist finds nothing", func(t *testing.T) {
		page, err := svc.ListEvents(ctx, env.Admin, events.Filter{SubLabels: []string{"NOSUCHSUBLABELXYZ999"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 {
			t.Errorf("nonexistent sub_label matched %d events, want 0", len(page.Items))
		}
	})

	t.Run("denied camera stays hidden even when its zone and sub_label are guessed", func(t *testing.T) {
		u, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "zone-sublabel-scoped")
		if err != nil {
			t.Fatal(err)
		}
		grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camA.ID, authz.EventsSearch)
		scoped := u

		// Permitted camera: the scoped actor finds its own zone and sub_label.
		ownZone, err := svc.ListEvents(ctx, scoped, events.Filter{Zones: []string{zoneA}})
		if err != nil {
			t.Fatal(err)
		}
		if len(ownZone.Items) == 0 {
			t.Fatalf("scoped actor with events.search on camera %s found no event for its own zone %s", camA.RemoteName, zoneA)
		}
		for _, e := range ownZone.Items {
			if e.CameraID != camA.ID {
				t.Errorf("scoped actor's zone result carries camera %s, want only %s", e.CameraID, camA.ID)
			}
		}
		ownSubLabel, err := svc.ListEvents(ctx, scoped, events.Filter{SubLabels: []string{subLabelA}})
		if err != nil {
			t.Fatal(err)
		}
		if len(ownSubLabel.Items) == 0 {
			t.Fatalf("scoped actor with events.search on camera %s found no event for its own sub_label %s", camA.RemoteName, subLabelA)
		}

		// Denied camera (no grant at all on camB): the scoped actor gets zero results even
		// though the zone/sub_label genuinely exist there.
		deniedZone, err := svc.ListEvents(ctx, scoped, events.Filter{Zones: []string{zoneB}})
		if err != nil {
			t.Fatal(err)
		}
		if len(deniedZone.Items) != 0 {
			t.Errorf("scoped actor without access to camera %s found %d events for its zone %s, want 0",
				camB.RemoteName, len(deniedZone.Items), zoneB)
		}
		deniedSubLabel, err := svc.ListEvents(ctx, scoped, events.Filter{SubLabels: []string{subLabelB}})
		if err != nil {
			t.Fatal(err)
		}
		if len(deniedSubLabel.Items) != 0 {
			t.Errorf("scoped actor without access to camera %s found %d events for its sub_label %s, want 0",
				camB.RemoteName, len(deniedSubLabel.Items), subLabelB)
		}
	})
}
