//go:build integration

package events_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/store"
)

func TestSetReviewedBulk(t *testing.T) {
	env, syncer, svc := setup(t)
	ctx := context.Background()
	syncer.SyncAll(ctx)
	all, err := svc.ListEvents(ctx, env.Admin, events.Filter{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	allowedCam := env.Cameras["frigate-h01/acceso_norte"]
	var allowed, other uuid.UUID
	for _, e := range all.Items {
		switch {
		case e.CameraID == allowedCam.ID && allowed == uuid.Nil:
			allowed = e.ID
		case e.CameraID != allowedCam.ID && other == uuid.Nil:
			other = e.ID
		}
	}
	if allowed == uuid.Nil || other == uuid.Nil {
		t.Fatal("fixture lacks events on two cameras")
	}
	reviewed := func(id uuid.UUID) bool {
		e, err := svc.GetEvent(ctx, env.Admin, id)
		if err != nil {
			t.Fatal(err)
		}
		return e.Reviewed
	}

	t.Run("admin reviews several events in one call", func(t *testing.T) {
		out, err := svc.SetReviewedBulk(ctx, env.Admin, []uuid.UUID{allowed, other}, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 2 || !out[0].Reviewed || !out[1].Reviewed || !reviewed(allowed) || !reviewed(other) {
			t.Fatalf("bulk review not applied: %+v", out)
		}
		if _, err := svc.SetReviewedBulk(ctx, env.Admin, []uuid.UUID{allowed, other}, false); err != nil {
			t.Fatal(err)
		}
		if reviewed(allowed) || reviewed(other) {
			t.Fatal("bulk un-review not applied")
		}
	})

	t.Run("one unauthorized id rejects the whole batch", func(t *testing.T) {
		op := env.Actor(t, "operador")
		grantCameraPermission(t, env, *op.TenantID, op.UserID, allowedCam.ID, authz.EventsReview)
		if _, err := svc.SetReviewedBulk(ctx, op, []uuid.UUID{allowed}, true); err != nil {
			t.Fatalf("authorized camera: %v", err)
		}
		if !reviewed(allowed) {
			t.Fatal("authorized bulk did not apply")
		}
		if _, err := svc.SetReviewed(ctx, env.Admin, allowed, false); err != nil {
			t.Fatal(err)
		}
		_, err := svc.SetReviewedBulk(ctx, op, []uuid.UUID{allowed, other}, true)
		if !errors.Is(err, access.ErrForbidden) {
			t.Fatalf("mixed batch error = %v, want forbidden", err)
		}
		if reviewed(allowed) || reviewed(other) {
			t.Fatal("a rejected batch left partial changes")
		}
	})

	t.Run("an unknown id rejects the whole batch with not found", func(t *testing.T) {
		_, err := svc.SetReviewedBulk(ctx, env.Admin, []uuid.UUID{allowed, uuid.New()}, true)
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("error = %v, want not found", err)
		}
		if reviewed(allowed) {
			t.Fatal("a rejected batch left partial changes")
		}
	})

	t.Run("size bounds and duplicates", func(t *testing.T) {
		if _, err := svc.SetReviewedBulk(ctx, env.Admin, nil, true); err == nil {
			t.Fatal("empty batch accepted")
		}
		if _, err := svc.SetReviewedBulk(ctx, env.Admin, make([]uuid.UUID, 201), true); err == nil {
			t.Fatal("201 ids accepted")
		}
		out, err := svc.SetReviewedBulk(ctx, env.Admin, []uuid.UUID{allowed, allowed}, true)
		if err != nil || len(out) != 1 {
			t.Fatalf("duplicates: %v, %d results, want 1", err, len(out))
		}
	})
}
