//go:build integration

package events_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// setSubLabelOnOneEvent directly sets sub_labels on the newest event of camID, bypassing the
// normal Frigate ingestion path: frigatemock only ever writes sub_label/plate text on LPR-tagged
// cameras (internal/frigatemock/generator.go, enrich). This simulates the ordinary, non-plate use
// of sub_label on a non-LPR camera (e.g. a Frigate face-recognition name).
func setSubLabelOnOneEvent(t *testing.T, env *demofix.Env, camID uuid.UUID, subLabel string) {
	t.Helper()
	ctx := context.Background()
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx, `
UPDATE events SET sub_labels = ARRAY[$1::text]
WHERE id = (SELECT id FROM events WHERE camera_id = $2 ORDER BY start_time DESC LIMIT 1)`, subLabel, camID)
		if err != nil {
			return err
		}
		if ct.RowsAffected() != 1 {
			return fmt.Errorf("expected to update 1 event on camera %s, updated %d", camID, ct.RowsAffected())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("set sub_label on camera %s: %v", camID, err)
	}
}

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
	camB := env.Cameras["frigate-c01/ruta_1"]       // zone: ingreso; LPR-enabled, different server

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
		// lpr.search is required to observe an own-camera sub_label match (see the
		// "sub_label filter requires lpr.search" test below for the case without it).
		for _, p := range []authz.Permission{authz.EventsSearch, authz.LPRSearch} {
			grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camA.ID, p)
		}
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
			t.Fatalf("scoped actor with events.search+lpr.search on camera %s found no event for its own sub_label %s", camA.RemoteName, subLabelA)
		}
		for _, e := range ownSubLabel.Items {
			if e.CameraID != camA.ID {
				t.Errorf("scoped actor's sub_label result carries camera %s, want only %s", e.CameraID, camA.ID)
			}
		}

		// Denied camera (no grant at all on camB): the scoped actor never gets an event that
		// belongs to camB back, even though the zone/sub_label genuinely exist there.
		deniedZone, err := svc.ListEvents(ctx, scoped, events.Filter{Zones: []string{zoneB}})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range deniedZone.Items {
			if e.CameraID == camB.ID {
				t.Errorf("scoped actor without access to camera %s got back event %s for its zone %s", camB.RemoteName, e.ID, zoneB)
			}
		}
		deniedSubLabel, err := svc.ListEvents(ctx, scoped, events.Filter{SubLabels: []string{subLabelB}})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range deniedSubLabel.Items {
			if e.CameraID == camB.ID {
				t.Errorf("scoped actor without access to camera %s got back event %s for its sub_label %s", camB.RemoteName, e.ID, subLabelB)
			}
		}
	})

	// TestZoneAndSubLabelFilterCorrectness/sub_label_filter_requires_lpr.search proves the fix for
	// the review finding on commit 195f76a: sub_label carries recognized plate text on LPR
	// cameras (internal/frigatemock/generator.go, enrich), so filtering by it must be gated the
	// same way the plate filter already is — events.search alone is not enough.
	t.Run("sub_label filter requires lpr.search, not just events.search", func(t *testing.T) {
		u, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "sublabel-no-lpr-search")
		if err != nil {
			t.Fatal(err)
		}
		grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camA.ID, authz.EventsSearch)
		scoped := u

		page, err := svc.ListEvents(ctx, scoped, events.Filter{SubLabels: []string{subLabelA}})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 {
			t.Errorf("actor with events.search but not lpr.search on camera %s probed sub_label %q and got %d events, want 0",
				camA.RemoteName, subLabelA, len(page.Items))
		}
	})

	// sub_labels can carry plate text on LPR cameras (same sensitivity as the plates field), so
	// they must stay redacted for an actor who can search them but was not granted lpr.view,
	// exactly like plates already are (internal/events/service.go, ListEvents/getEvent).
	t.Run("sub_labels are redacted from the response without lpr.view", func(t *testing.T) {
		u, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "sublabel-no-lpr-view")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []authz.Permission{authz.EventsSearch, authz.LPRSearch} {
			grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camA.ID, p)
		}
		scoped := u

		page, err := svc.ListEvents(ctx, scoped, events.Filter{SubLabels: []string{subLabelA}})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 0 {
			t.Fatalf("actor with lpr.search on camera %s found no event for sub_label %s", camA.RemoteName, subLabelA)
		}
		for _, e := range page.Items {
			if len(e.SubLabels) != 0 {
				t.Errorf("event %s exposed sub_labels %v to an actor without lpr.view, want redacted", e.ID, e.SubLabels)
			}
		}
	})

	// Proves the events.search escalation at service.go ListEvents (using zone/sub_label bumps
	// the required permission from events.view to events.search) actually blocks a view-only
	// actor, not just skips the assertion.
	t.Run("events.view alone cannot use the zone or sub_label filters", func(t *testing.T) {
		u, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "viewonly-zone-sublabel")
		if err != nil {
			t.Fatal(err)
		}
		grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camA.ID, authz.EventsView)
		scoped := u

		byZone, err := svc.ListEvents(ctx, scoped, events.Filter{CameraIDs: []uuid.UUID{camA.ID}, Zones: []string{zoneA}})
		if err != nil {
			t.Fatal(err)
		}
		if len(byZone.Items) != 0 {
			t.Errorf("actor with only events.view filtered by zone %q and got %d events, want 0", zoneA, len(byZone.Items))
		}
		bySubLabel, err := svc.ListEvents(ctx, scoped, events.Filter{CameraIDs: []uuid.UUID{camA.ID}, SubLabels: []string{subLabelA}})
		if err != nil {
			t.Fatal(err)
		}
		if len(bySubLabel.Items) != 0 {
			t.Errorf("actor with only events.view filtered by sub_label %q and got %d events, want 0", subLabelA, len(bySubLabel.Items))
		}
	})

	// User decision (2026-09-28): sub_label LPR gating applies only to LPR-capable cameras. On a
	// non-LPR camera, sub_label is an ordinary event field (e.g. a Frigate face-recognition name)
	// and follows normal events.view/events.search permissions, with no LPR grant required at all.
	t.Run("sub_label on a non-LPR camera follows normal events permissions, not LPR", func(t *testing.T) {
		camNonLPR := env.Cameras["frigate-h01/plaza"] // no LPR capability
		faceLabel := "face-juan-perez"
		setSubLabelOnOneEvent(t, env, camNonLPR.ID, faceLabel)

		searchOnly, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "sublabel-nonlpr-search-only")
		if err != nil {
			t.Fatal(err)
		}
		// Only events.search - no lpr.search/lpr.view grant at all.
		grantCameraPermission(t, env, env.Demo.TenantID, searchOnly.UserID, camNonLPR.ID, authz.EventsSearch)

		page, err := svc.ListEvents(ctx, searchOnly, events.Filter{SubLabels: []string{faceLabel}})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 0 {
			t.Fatalf("actor with events.search (no LPR grant) on non-LPR camera %s could not filter by sub_label %q", camNonLPR.RemoteName, faceLabel)
		}
		for _, e := range page.Items {
			if e.CameraID != camNonLPR.ID {
				t.Errorf("event %s carries camera %s, want only %s", e.ID, e.CameraID, camNonLPR.ID)
			}
			if len(e.SubLabels) == 0 {
				t.Errorf("event %s sub_labels redacted on a non-LPR camera without any LPR grant, want visible under events.view", e.ID)
			}
		}

		viewOnly, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "sublabel-nonlpr-view-only")
		if err != nil {
			t.Fatal(err)
		}
		grantCameraPermission(t, env, env.Demo.TenantID, viewOnly.UserID, camNonLPR.ID, authz.EventsView)

		// events.view alone cannot use the sub_label filter (same escalation as zone), but the
		// field itself must still be visible on an unfiltered list, unredacted, since it is not
		// plate data on this camera.
		unfiltered, err := svc.ListEvents(ctx, viewOnly, events.Filter{CameraIDs: []uuid.UUID{camNonLPR.ID}})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range unfiltered.Items {
			if e.ID == page.Items[0].ID {
				found = true
				if len(e.SubLabels) == 0 || e.SubLabels[0] != faceLabel {
					t.Errorf("event %s sub_labels %v, want visible %q under events.view alone on a non-LPR camera", e.ID, e.SubLabels, faceLabel)
				}
			}
		}
		if !found {
			t.Fatalf("actor with events.view on non-LPR camera %s did not see the event carrying sub_label %q", camNonLPR.RemoteName, faceLabel)
		}
	})
}
