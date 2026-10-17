//go:build integration

package inventory_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func TestCameraZoneSyncPersistsAndConverges(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	input := inventory.CameraZoneInput{Name: "entry", Points: []inventory.CameraZonePoint{{X: 0.1, Y: 0.1}, {X: 0.9, Y: 0.1}, {X: 0.5, Y: 0.9}}, Settings: json.RawMessage(`{"objects":["person"]}`)}
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	var revision int64
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT sync_status, sync_revision FROM camera_zones WHERE id = $1`, zone.ID).Scan(&status, &revision)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || revision < 1 {
		t.Fatalf("initial sync state = %q revision %d", status, revision)
	}
	var visible int
	if err := env.Svc.Store.TxRaw(ctx, store.TenantScope{TenantID: cam.TenantID}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM camera_zone_sync_jobs WHERE zone_id=$1`, zone.ID).Scan(&visible)
	}); err != nil {
		t.Fatal(err)
	}
	if visible != 1 {
		t.Fatalf("own-tenant sync job visibility = %d, want 1", visible)
	}
	if err := env.Svc.Store.TxRaw(ctx, store.TenantScope{TenantID: uuid.New()}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM camera_zone_sync_jobs WHERE zone_id=$1`, zone.ID).Scan(&visible)
	}); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("cross-tenant sync job visibility = %d, want 0", visible)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT sync_status FROM camera_zones WHERE id = $1`, zone.ID).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "synced" {
		var e *string
		_ = env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT last_error FROM camera_zone_sync_jobs WHERE zone_id=$1`, zone.ID).Scan(&e)
		})
		t.Fatalf("sync state after worker = %q, want synced; error=%v", status, derefErr(e))
	}
	calls := env.Mocks["frigate-h01"].Server.SetCalls()
	if len(calls) != 1 || calls[0].UpdateTopic != "config/cameras/acceso_norte/zones" {
		t.Fatalf("managed zone sync calls = %+v", calls)
	}
}

func TestCameraZoneSyncRetriesOfflineAndSuppressesStaleDesiredState(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	input := inventory.CameraZoneInput{Name: "retry_zone", Points: []inventory.CameraZonePoint{{X: 0.1, Y: 0.1}, {X: 0.9, Y: 0.1}, {X: 0.5, Y: 0.9}}}
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Points[0].X = 0.2
	if _, err := env.Svc.UpdateCameraZone(ctx, env.Admin, cam.ID, zone.ID, input); err != nil {
		t.Fatal(err)
	}
	mock := env.Mocks["frigate-h01"].Server
	mock.Offline.Store(true)
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	mock.Offline.Store(false)
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE camera_zone_sync_jobs SET next_attempt_at = now()`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	calls := mock.SetCalls()
	if len(calls) != 1 {
		t.Fatalf("stale desired version was applied or retry did not converge: %+v", calls)
	}
	config := calls[0].ConfigData["cameras"].(map[string]any)["acceso_norte"].(map[string]any)["zones"].(map[string]any)["ovms_retry_zone"].(map[string]any)
	if config["coordinates"] == nil || config["coordinates"] == "0.1,0.1,0.9,0.1,0.5,0.9" {
		t.Fatalf("worker applied stale zone state: %#v", config)
	}
}

func TestCameraZoneSyncConflictsWithUntrackedRemoteZoneWithoutOverwrite(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	mock := env.Mocks["frigate-h01"].Server
	mock.SetCameraZoneForTest("acceso_norte", "ovms_taken", map[string]any{"coordinates": "100,100,200,100,100,200"})
	cam := env.Cameras["frigate-h01/acceso_norte"]
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, inventory.CameraZoneInput{Name: "taken", Points: []inventory.CameraZonePoint{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .5, Y: .9}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT sync_status FROM camera_zones WHERE id=$1`, zone.ID).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "conflict" {
		t.Fatalf("zone status = %q, want conflict", status)
	}
	if calls := mock.SetCalls(); len(calls) != 0 {
		t.Fatalf("untracked remote zone was overwritten: %+v", calls)
	}
}

func TestCameraZoneSyncDetectsDriftAgainstLastAppliedWithoutOverwrite(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	mock := env.Mocks["frigate-h01"].Server
	input := inventory.CameraZoneInput{Name: "drifted", Points: []inventory.CameraZonePoint{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .5, Y: .9}}}
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	mock.SetCameraZoneForTest("acceso_norte", "ovms_drifted", map[string]any{"coordinates": "10,10,20,10,10,20"})
	input.Points[0].X = .2
	if _, err := env.Svc.UpdateCameraZone(ctx, env.Admin, cam.ID, zone.ID, input); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT sync_status FROM camera_zones WHERE id=$1`, zone.ID).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "conflict" {
		t.Fatalf("zone status = %q, want conflict", status)
	}
	if calls := mock.SetCalls(); len(calls) != 1 {
		t.Fatalf("drift was overwritten instead of conflicted: %+v", calls)
	}
}

func TestCameraZoneSyncDeletesOnlyPreviouslyAppliedZone(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, inventory.CameraZoneInput{Name: "remove_me", Points: []inventory.CameraZonePoint{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .5, Y: .9}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.DeleteCameraZone(ctx, env.Admin, cam.ID, zone.ID); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(env.Mocks["frigate-h01"].Server.ConfigYAML(), "ovms_remove_me") {
		t.Fatal("previously applied managed zone still exists after delete")
	}
	if calls := env.Mocks["frigate-h01"].Server.SetCalls(); len(calls) != 2 {
		t.Fatalf("expected one upsert and one verified delete, got %+v", calls)
	}
}

func TestCameraZoneSyncRevalidatesServerConfigPermissionBeforeWrite(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, inventory.CameraZoneInput{Name: "revoked", Points: []inventory.CameraZonePoint{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .5, Y: .9}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM permission_grants WHERE permission='servers.config'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT sync_status FROM camera_zones WHERE id=$1`, zone.ID).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("sync state = %q, want terminal failed after permission revocation", status)
	}
	if calls := env.Mocks["frigate-h01"].Server.SetCalls(); len(calls) != 0 {
		t.Fatalf("worker wrote after permission revocation: %+v", calls)
	}
}

func derefErr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func TestCameraZoneSyncReconcilesExactRemoteStateAfterLostUpsertCommit(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, inventory.CameraZoneInput{Name: "replay", Points: []inventory.CameraZonePoint{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .5, Y: .9}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE camera_zones SET last_applied = NULL WHERE id=$1`, zone.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE camera_zone_sync_jobs SET last_applied = NULL, status='pending', next_attempt_at=now() WHERE zone_id=$1`, zone.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT sync_status FROM camera_zones WHERE id=$1`, zone.ID).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "synced" {
		t.Fatalf("exact already-applied remote state status = %q, want synced", status)
	}
}

func TestCameraZoneSyncCanRetryExpiredExhaustedLease(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, inventory.CameraZoneInput{Name: "lease", Points: []inventory.CameraZonePoint{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .5, Y: .9}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE camera_zone_sync_jobs SET status='processing', attempts=8, lease_until=now()-interval '1 second' WHERE zone_id=$1`, zone.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.RetryCameraZoneSync(ctx, env.Admin, cam.ID, zone.ID); err != nil {
		t.Fatalf("retry expired lease: %v", err)
	}
	var status string
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM camera_zone_sync_jobs WHERE zone_id=$1`, zone.ID).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("expired exhausted lease status = %q, want pending", status)
	}
}

func TestCameraZoneSyncDoesNotStealActiveExhaustedLease(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, inventory.CameraZoneInput{Name: "active_lease", Points: []inventory.CameraZonePoint{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .5, Y: .9}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE camera_zone_sync_jobs SET status='processing', attempts=8, lease_until=now()+interval '1 minute' WHERE zone_id=$1`, zone.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.RetryCameraZoneSync(ctx, env.Admin, cam.ID, zone.ID); err == nil {
		t.Fatal("manual retry stole an active lease")
	}
	var status string
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM camera_zone_sync_jobs WHERE zone_id=$1`, zone.ID).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "processing" {
		t.Fatalf("active lease status = %q, want processing", status)
	}
}

func TestCameraZoneSyncDeleteReconcilesLostRemoteDelete(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	mock := env.Mocks["frigate-h01"].Server
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, inventory.CameraZoneInput{Name: "delete_replay", Points: []inventory.CameraZonePoint{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .5, Y: .9}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.DeleteCameraZone(ctx, env.Admin, cam.ID, zone.ID); err != nil {
		t.Fatal(err)
	}
	mock.DeleteCameraZoneForTest(cam.RemoteName, "ovms_delete_replay")
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM camera_zone_sync_jobs WHERE zone_id=$1`, zone.ID).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "synced" {
		t.Fatalf("missing already-deleted remote zone status = %q, want synced", status)
	}
}

func TestCameraZoneSyncRenameRemovesOldRemoteKey(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	input := inventory.CameraZoneInput{Name: "before", Points: []inventory.CameraZonePoint{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .5, Y: .9}}}
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	input.Name = "middle"
	if _, err := env.Svc.UpdateCameraZone(ctx, env.Admin, cam.ID, zone.ID, input); err != nil {
		t.Fatal(err)
	}
	input.Name = "after"
	if _, err := env.Svc.UpdateCameraZone(ctx, env.Admin, cam.ID, zone.ID, input); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	yaml := env.Mocks["frigate-h01"].Server.ConfigYAML()
	if strings.Contains(yaml, "ovms_before") || !strings.Contains(yaml, "ovms_after") {
		t.Fatalf("rename did not converge both keys safely: %s", yaml)
	}
}

func TestCameraZoneSyncRenameConflictsWithOccupiedDestination(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	mock := env.Mocks["frigate-h01"].Server
	input := inventory.CameraZoneInput{Name: "rename_source", Points: []inventory.CameraZonePoint{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .5, Y: .9}}}
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	foreign := map[string]any{"coordinates": "1,1,2,1,1,2"}
	mock.SetCameraZoneForTest(cam.RemoteName, "ovms_rename_target", foreign)
	input.Name = "rename_target"
	if _, err := env.Svc.UpdateCameraZone(ctx, env.Admin, cam.ID, zone.ID, input); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT sync_status FROM camera_zones WHERE id=$1`, zone.ID).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "conflict" {
		t.Fatalf("rename into occupied destination status = %q, want conflict", status)
	}
	if !strings.Contains(mock.ConfigYAML(), "ovms_rename_source") || !strings.Contains(mock.ConfigYAML(), "coordinates: 1,1,2,1,1,2") {
		t.Fatal("rename overwrote destination or deleted the old managed zone")
	}
}

func TestCameraZoneSyncDeleteAfterUnappliedRenameRemovesOldRemoteKey(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	input := inventory.CameraZoneInput{Name: "old_key", Points: []inventory.CameraZonePoint{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .5, Y: .9}}}
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	input.Name = "new_key"
	if _, err := env.Svc.UpdateCameraZone(ctx, env.Admin, cam.ID, zone.ID, input); err != nil {
		t.Fatal(err)
	}
	// The rename was never applied remotely when the zone is deleted.
	if err := env.Svc.DeleteCameraZone(ctx, env.Admin, cam.ID, zone.ID); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.ProcessCameraZoneSyncJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if yaml := env.Mocks["frigate-h01"].Server.ConfigYAML(); strings.Contains(yaml, "ovms_old_key") || strings.Contains(yaml, "ovms_new_key") {
		t.Fatalf("deleted zone left an orphaned managed key behind: %s", yaml)
	}
	var status string
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT sync_status FROM camera_zones WHERE id=$1`, zone.ID).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "synced" {
		t.Fatalf("delete status = %q, want synced", status)
	}
}

func TestRetryCameraZoneSyncWaitsForPerZoneAdvisoryLock(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	zone, err := env.Svc.CreateCameraZone(ctx, env.Admin, cam.ID, inventory.CameraZoneInput{Name: "lock_order", Points: []inventory.CameraZonePoint{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .5, Y: .9}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE camera_zone_sync_jobs SET status='failed' WHERE zone_id=$1`, zone.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE camera_zones SET sync_status='failed' WHERE id=$1`, zone.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	err = env.Svc.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		// Writers and workers take this lock before touching the zone and job rows. A retry
		// that locks rows in the opposite order can deadlock against them, so it must queue.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, zone.ID.String()); err != nil {
			return err
		}
		go func() { done <- env.Svc.RetryCameraZoneSync(ctx, env.Admin, cam.ID, zone.ID) }()
		select {
		case err := <-done:
			t.Errorf("retry completed while the zone lock was held (err=%v)", err)
			done <- err
		case <-time.After(500 * time.Millisecond):
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("retry after lock release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("retry did not complete after the zone lock was released")
	}
}
