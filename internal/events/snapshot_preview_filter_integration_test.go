//go:build integration

package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/frigatemock"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func boolPtr(b bool) *bool { return &b }

// seedAlertReview injects a finished, alert-severity review directly into a mock Frigate's
// store, on the given camera, with one detection. frigatemock's random severity roll for a
// non-LPR camera (internal/frigatemock/generator.go buildReview: 1/3 chance) makes relying on
// the randomly seeded pool alone for "at least one alert-severity, has_snapshot=true event on a
// non-LPR camera" flaky; this makes that case deterministic.
func seedAlertReview(t *testing.T, env *demofix.Env, server, camera, id string) {
	t.Helper()
	start := float64(time.Now().Add(-time.Minute).Unix())
	end := start + 30
	env.Mocks[server].Server.Store.Put(frigatemock.Review{
		ID: id, Camera: camera, StartTime: start, EndTime: &end, Severity: "alert",
		Data: frigatemock.ReviewData{Detections: []string{id}, Objects: []string{"person"}, SubLabels: []string{}, Zones: []string{}, Audio: []string{}},
	})
}

// setPreviewKey directly sets preview_key on the newest event of camID, standing in for the
// (not yet built) central preview-copy job: no ingestion path populates preview_key today
// (user decision 2026-09-28), so has_preview is always false in practice until that job exists.
func setPreviewKey(t *testing.T, env *demofix.Env, camID uuid.UUID, key string) {
	t.Helper()
	ctx := context.Background()
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
UPDATE events SET preview_key = $1
WHERE id = (SELECT id FROM events WHERE camera_id = $2 ORDER BY start_time DESC LIMIT 1)`, key, camID)
		return err
	})
	if err != nil {
		t.Fatalf("set preview_key on camera %s: %v", camID, err)
	}
}

// TestSnapshotAndPreviewFilterCorrectness proves the has_snapshot / has_preview flags and
// filters on ListEvents (PRD §44). has_snapshot is synced from Frigate tracked objects for
// every camera, not only LPR ones (frigatemock.media.go objects: HasSnapshot is true for
// alert-severity reviews' objects, false for detection-severity ones); has_preview is an
// OpenVMS-internal signal (preview_key populated), never probed from Frigate.
func TestSnapshotAndPreviewFilterCorrectness(t *testing.T) {
	env, syncer, svc := setup(t)
	ctx := context.Background()

	camLPR := env.Cameras["frigate-h01/acceso_norte"] // LPR-enabled
	camPlain := env.Cameras["frigate-h01/plaza"]      // no LPR capability

	seedAlertReview(t, env, "frigate-h01", camPlain.RemoteName, "manual-plaza-alert")
	syncer.SyncAll(ctx)

	t.Run("has_snapshot is synced for a non-LPR camera, not only LPR ones", func(t *testing.T) {
		alerts, err := svc.ListEvents(ctx, env.Admin, events.Filter{CameraIDs: []uuid.UUID{camPlain.ID}, Severity: "alert", Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(alerts.Items) == 0 {
			t.Fatalf("no alert-severity events seeded on non-LPR camera %s to prove has_snapshot syncs there", camPlain.RemoteName)
		}
		for _, e := range alerts.Items {
			if !e.HasSnapshot {
				t.Errorf("alert event %s on non-LPR camera %s has_snapshot=false, want true (frigatemock marks alert objects has_snapshot=true)", e.ID, camPlain.RemoteName)
			}
		}
	})

	t.Run("has_snapshot filter finds only matching events", func(t *testing.T) {
		withSnapshot, err := svc.ListEvents(ctx, env.Admin, events.Filter{HasSnapshot: boolPtr(true), Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(withSnapshot.Items) == 0 {
			t.Fatal("has_snapshot=true matched no events")
		}
		for _, e := range withSnapshot.Items {
			if !e.HasSnapshot {
				t.Errorf("event %s returned by has_snapshot=true has HasSnapshot=false", e.ID)
			}
		}

		withoutSnapshot, err := svc.ListEvents(ctx, env.Admin, events.Filter{HasSnapshot: boolPtr(false), Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(withoutSnapshot.Items) == 0 {
			t.Fatal("has_snapshot=false matched no events")
		}
		for _, e := range withoutSnapshot.Items {
			if e.HasSnapshot {
				t.Errorf("event %s returned by has_snapshot=false has HasSnapshot=true", e.ID)
			}
		}
	})

	t.Run("has_preview defaults to false and the filter follows preview_key", func(t *testing.T) {
		none, err := svc.ListEvents(ctx, env.Admin, events.Filter{CameraIDs: []uuid.UUID{camLPR.ID}, Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(none.Items) == 0 {
			t.Fatal("no events on camLPR to check the has_preview default")
		}
		for _, e := range none.Items {
			if e.HasPreview {
				t.Errorf("event %s has_preview=true before any preview_key was ever set", e.ID)
			}
		}

		setPreviewKey(t, env, camLPR.ID, "tenant/x/previews/srv/ev")

		withPreview, err := svc.ListEvents(ctx, env.Admin, events.Filter{CameraIDs: []uuid.UUID{camLPR.ID}, HasPreview: boolPtr(true), Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(withPreview.Items) != 1 {
			t.Fatalf("has_preview=true on camLPR matched %d events, want 1", len(withPreview.Items))
		}

		withoutPreview, err := svc.ListEvents(ctx, env.Admin, events.Filter{CameraIDs: []uuid.UUID{camLPR.ID}, HasPreview: boolPtr(false), Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range withoutPreview.Items {
			if e.ID == withPreview.Items[0].ID {
				t.Errorf("event %s returned by has_preview=false right after its preview_key was set", e.ID)
			}
		}
	})

	t.Run("events.view alone cannot use the has_snapshot or has_preview filters", func(t *testing.T) {
		u, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "viewonly-snapshot-preview")
		if err != nil {
			t.Fatal(err)
		}
		grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camLPR.ID, authz.EventsView)

		bySnapshot, err := svc.ListEvents(ctx, u, events.Filter{CameraIDs: []uuid.UUID{camLPR.ID}, HasSnapshot: boolPtr(true)})
		if err != nil {
			t.Fatal(err)
		}
		if len(bySnapshot.Items) != 0 {
			t.Errorf("actor with only events.view filtered by has_snapshot and got %d events, want 0", len(bySnapshot.Items))
		}
		byPreview, err := svc.ListEvents(ctx, u, events.Filter{CameraIDs: []uuid.UUID{camLPR.ID}, HasPreview: boolPtr(false)})
		if err != nil {
			t.Fatal(err)
		}
		if len(byPreview.Items) != 0 {
			t.Errorf("actor with only events.view filtered by has_preview and got %d events, want 0", len(byPreview.Items))
		}
	})
}
