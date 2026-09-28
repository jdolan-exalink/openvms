//go:build integration

package events_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// setCameraLPR directly flips cameras.lpr, standing in for the camera being reconfigured in
// Frigate (LPR capability turned on or off) after events were already ingested.
func setCameraLPR(t *testing.T, env *demofix.Env, camID uuid.UUID, lpr bool) {
	t.Helper()
	ctx := context.Background()
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE cameras SET lpr = $2 WHERE id = $1`, camID, lpr)
		return err
	})
	if err != nil {
		t.Fatalf("set cameras.lpr on %s: %v", camID, err)
	}
}

// eventWithSubLabel returns the id and sub_label of an event of camID that carries one, as seen
// with full access.
func eventWithSubLabel(t *testing.T, env *demofix.Env, svc *events.Service, camID uuid.UUID) (uuid.UUID, string) {
	t.Helper()
	page, err := svc.ListEvents(context.Background(), env.Admin, events.Filter{CameraIDs: []uuid.UUID{camID}, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Items {
		if len(e.SubLabels) > 0 {
			return e.ID, e.SubLabels[0]
		}
	}
	t.Fatalf("camera %s has no event carrying a sub_label", camID)
	return uuid.Nil, ""
}

// TestSubLabelLPRGatingSurvivesCameraToggle proves the fix for the SECURITY review finding on
// commit 7658e50: sub_label redaction (getEvent) and the sub_label filter (ListEvents) used to
// key only on cameras.lpr, the camera's CURRENT LPR flag. Switching a camera off LPR after
// events were ingested made their historical plate-text sub_labels visible and searchable to an
// actor without lpr.view/lpr.search. events.lpr now records the camera's LPR capability at
// ingestion time and gating requires either flag, so this must stay gated after the toggle.
func TestSubLabelLPRGatingSurvivesCameraToggle(t *testing.T) {
	env, syncer, svc := setup(t)
	ctx := context.Background()
	syncer.SyncAll(ctx)

	camLPR := env.Cameras["frigate-h01/acceso_norte"] // LPR-enabled at ingestion time
	eventID, subLabel := eventWithSubLabel(t, env, svc, camLPR.ID)

	// The camera is later reconfigured off LPR in Frigate.
	setCameraLPR(t, env, camLPR.ID, false)

	u, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "toggle-no-lpr")
	if err != nil {
		t.Fatal(err)
	}
	grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camLPR.ID, authz.EventsView)
	grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camLPR.ID, authz.EventsSearch)

	t.Run("getEvent keeps sub_labels redacted after the camera is switched off LPR", func(t *testing.T) {
		got, err := svc.GetEvent(ctx, u, eventID)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.SubLabels) != 0 {
			t.Errorf("sub_labels = %v after camera toggled off LPR without lpr.view, want redacted", got.SubLabels)
		}
	})

	t.Run("the sub_label filter keeps requiring lpr.search after the camera is switched off LPR", func(t *testing.T) {
		page, err := svc.ListEvents(ctx, u, events.Filter{CameraIDs: []uuid.UUID{camLPR.ID}, SubLabels: []string{subLabel}})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 {
			t.Errorf("sub_label %q searchable after camera toggled off LPR without lpr.search, got %d events, want 0", subLabel, len(page.Items))
		}
	})

	// Once granted lpr.view/lpr.search, the actor sees and can search the historical sub_label
	// again, proving the toggle did not destroy the data or over-gate it permanently.
	t.Run("lpr.view/lpr.search still reveal it", func(t *testing.T) {
		grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camLPR.ID, authz.LPRView)
		grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camLPR.ID, authz.LPRSearch)

		got, err := svc.GetEvent(ctx, u, eventID)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.SubLabels) == 0 {
			t.Errorf("sub_labels redacted even with lpr.view, want %q visible", subLabel)
		}
	})
}

// TestGetEventSubLabelRedaction covers getEvent (single-event read) directly, mirroring the
// ListEvents coverage in zone_sub_label_filter_integration_test.go: sub_labels are redacted
// without lpr.view on an LPR-capable camera, and stay visible under plain events.view on a
// non-LPR camera (user decision 2026-09-28).
func TestGetEventSubLabelRedaction(t *testing.T) {
	env, syncer, svc := setup(t)
	ctx := context.Background()
	syncer.SyncAll(ctx)

	camLPR := env.Cameras["frigate-h01/acceso_norte"]
	camNonLPR := env.Cameras["frigate-h01/plaza"]

	lprEventID, subLabel := eventWithSubLabel(t, env, svc, camLPR.ID)
	setSubLabelOnOneEvent(t, env, camNonLPR.ID, "face-jane-doe")
	nonLPREventID, _ := eventWithSubLabel(t, env, svc, camNonLPR.ID)

	t.Run("redacted on an LPR camera without lpr.view", func(t *testing.T) {
		u, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "getevent-lpr-noview")
		if err != nil {
			t.Fatal(err)
		}
		grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camLPR.ID, authz.EventsView)

		got, err := svc.GetEvent(ctx, u, lprEventID)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.SubLabels) != 0 {
			t.Errorf("sub_labels = %v on LPR camera without lpr.view, want redacted (had %q)", got.SubLabels, subLabel)
		}
	})

	t.Run("visible on a non-LPR camera under plain events.view", func(t *testing.T) {
		u, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "getevent-nonlpr-view")
		if err != nil {
			t.Fatal(err)
		}
		grantCameraPermission(t, env, env.Demo.TenantID, u.UserID, camNonLPR.ID, authz.EventsView)

		got, err := svc.GetEvent(ctx, u, nonLPREventID)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.SubLabels) == 0 {
			t.Errorf("sub_labels redacted on a non-LPR camera under plain events.view, want visible")
		}
	})
}
