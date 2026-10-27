//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

type deletedState struct {
	rows       int
	deletedAt  bool
	deletedBy  *uuid.UUID
	tombstones int
	retainDays *int32
}

func deletedStateOf(t *testing.T, env *demofix.Env, id uuid.UUID) deletedState {
	t.Helper()
	var st deletedState
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		if err := tx.QueryRow(context.Background(),
			`SELECT count(*), coalesce(bool_or(deleted_at IS NOT NULL), false), (array_agg(deleted_by))[1] FROM cameras WHERE id = $1`, id).
			Scan(&st.rows, &st.deletedAt, &st.deletedBy); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(),
			`SELECT count(*), max(retain_days) FROM camera_tombstones WHERE camera_id = $1`, id).Scan(&st.tombstones, &st.retainDays)
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// insertLiveCamera adds a live camera row, as something other than the sync would.
func insertLiveCamera(t *testing.T, env *demofix.Env, tenantID, siteID, serverID uuid.UUID, remoteName string) {
	t.Helper()
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO cameras (tenant_id, site_id, server_id, remote_name, display_name) VALUES ($1, $2, $3, $4, 'other')`,
			tenantID, siteID, serverID, remoteName)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteCameraKeepsTheRowAndLeavesATombstone(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	retain := int32(7)

	if err := env.Svc.DeleteCamera(ctx, env.Admin, cam.ID, &retain, nil); err != nil {
		t.Fatal(err)
	}

	st := deletedStateOf(t, env, cam.ID)
	if st.rows != 1 || !st.deletedAt || st.deletedBy == nil || *st.deletedBy != env.Admin.UserID {
		t.Fatalf("camera row after delete = %+v, want the same row kept, soft-deleted by the admin", st)
	}
	if st.tombstones != 1 || st.retainDays == nil || *st.retainDays != 7 {
		t.Fatalf("tombstone after delete = %+v, want one with retain_days 7", st)
	}
	if _, err := env.Svc.GetCamera(ctx, env.Admin, cam.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetCamera of a deleted camera = %v, want not found", err)
	}
	if got := auditCount(t, env, "CAMERA_REMOVED", cam.ID); got != 1 {
		t.Fatalf("CAMERA_REMOVED audit entries = %d, want 1", got)
	}
	// The sync must not bring it back while Frigate still lists it.
	res, err := env.Svc.SyncServer(ctx, env.Admin, cam.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 || res.Skipped != 1 {
		t.Fatalf("sync after delete: added=%d skipped=%d, want 0 and 1", res.Added, res.Skipped)
	}
}

func TestDeleteCameraWithoutARetentionWindowStoresNone(t *testing.T) {
	env := demofix.Setup(t)
	cam := env.Cameras["frigate-h01/plaza"]
	if err := env.Svc.DeleteCamera(context.Background(), env.Admin, cam.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if st := deletedStateOf(t, env, cam.ID); st.tombstones != 1 || st.retainDays != nil {
		t.Fatalf("tombstone = %+v, want one with an unknown retention window", st)
	}
}

func TestDeleteCameraNeedsServersManageAndAnExistingCamera(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]

	operator := env.Actor(t, "operador")
	if err := env.Svc.DeleteCamera(ctx, operator, cam.ID, nil, nil); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("delete by an operator without servers.manage = %v, want forbidden", err)
	}
	if st := deletedStateOf(t, env, cam.ID); st.deletedAt || st.tombstones != 0 {
		t.Fatalf("a refused delete changed the camera: %+v", st)
	}
	if err := env.Svc.DeleteCamera(ctx, env.Admin, uuid.New(), nil, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete of an unknown camera = %v, want not found", err)
	}
	if err := env.Svc.DeleteCamera(ctx, env.Admin, cam.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.DeleteCamera(ctx, env.Admin, cam.ID, nil, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete of the same camera = %v, want not found", err)
	}
	if got := auditCount(t, env, "CAMERA_REMOVED", cam.ID); got != 1 {
		t.Fatalf("CAMERA_REMOVED audit entries = %d, want 1 (refused and repeated deletes record nothing)", got)
	}
}

func TestDeleteCameraEmptiesItsSavedViewCells(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	gone := env.Cameras["frigate-h01/plaza"]
	kept := env.Cameras["frigate-h01/acceso_norte"]
	var viewID uuid.UUID
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO views (tenant_id, owner_id, name, layout) VALUES ($1, $2, 'test', jsonb_build_object('columns', 2, 'cells',
		jsonb_build_array(jsonb_build_object('camera_id', $3::text), jsonb_build_object('camera_id', $4::text)))) RETURNING id`,
			gone.TenantID, env.Admin.UserID, gone.ID.String(), kept.ID.String()).Scan(&viewID)
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := env.Svc.DeleteCamera(ctx, env.Admin, gone.ID, nil, nil); err != nil {
		t.Fatal(err)
	}

	var first *string
	var second string
	err = env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT layout->'cells'->0->>'camera_id', layout->'cells'->1->>'camera_id' FROM views WHERE id = $1`, viewID).Scan(&first, &second)
	})
	if err != nil {
		t.Fatal(err)
	}
	if first != nil || second != kept.ID.String() {
		t.Fatalf("view cells after delete = (%v, %q), want the deleted camera's cell empty and the other kept", first, second)
	}
}

func TestRestoreCameraReturnsTheSameRowAndLiftsTheTombstone(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	if err := env.Svc.DeleteCamera(ctx, env.Admin, cam.ID, nil, nil); err != nil {
		t.Fatal(err)
	}

	restored, err := env.Svc.RestoreCamera(ctx, env.Admin, cam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID != cam.ID || restored.RemoteName != cam.RemoteName {
		t.Fatalf("restored camera = %s/%s, want the original %s/%s (same id keeps the history attached)", restored.ID, restored.RemoteName, cam.ID, cam.RemoteName)
	}
	if st := deletedStateOf(t, env, cam.ID); st.deletedAt || st.deletedBy != nil || st.tombstones != 0 {
		t.Fatalf("state after restore = %+v, want live with no tombstone", st)
	}
	if got := auditCount(t, env, "CAMERA_RESTORED", cam.ID); got != 1 {
		t.Fatalf("CAMERA_RESTORED audit entries = %d, want 1", got)
	}
	// After a restore the sync updates the camera instead of adding a second one.
	res, err := env.Svc.SyncServer(ctx, env.Admin, cam.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 || res.Skipped != 0 {
		t.Fatalf("sync after restore: added=%d skipped=%d, want 0 and 0", res.Added, res.Skipped)
	}
	if got := liveCamerasNamed(t, env, cam.ServerID, cam.RemoteName); got != 1 {
		t.Fatalf("live cameras named %q after restore and sync = %d, want 1", cam.RemoteName, got)
	}
}

func TestRestoreCameraRefusesWhenALiveCameraHasTheName(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	if err := env.Svc.DeleteCamera(ctx, env.Admin, cam.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	// Something created a live camera under the same name in the meantime.
	insertLiveCamera(t, env, cam.TenantID, cam.SiteID, cam.ServerID, cam.RemoteName)

	if _, err := env.Svc.RestoreCamera(ctx, env.Admin, cam.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("restore over a live camera of the same name = %v, want conflict", err)
	}
	if st := deletedStateOf(t, env, cam.ID); !st.deletedAt || st.tombstones != 1 {
		t.Fatalf("a refused restore changed the camera: %+v, want it still deleted with its tombstone", st)
	}
}

func TestRestoreCameraNeedsServersManageAndADeletedCamera(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]

	if _, err := env.Svc.RestoreCamera(ctx, env.Admin, cam.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("restore of a camera that is not deleted = %v, want not found", err)
	}
	if err := env.Svc.DeleteCamera(ctx, env.Admin, cam.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	operator := env.Actor(t, "operador")
	if _, err := env.Svc.RestoreCamera(ctx, operator, cam.ID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("restore by an operator without servers.manage = %v, want forbidden", err)
	}
	if st := deletedStateOf(t, env, cam.ID); !st.deletedAt || st.tombstones != 1 {
		t.Fatalf("a refused restore changed the camera: %+v", st)
	}
}
