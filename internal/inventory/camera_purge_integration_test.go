//go:build integration

package inventory_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// seedCameraHistory inserts one row per table the purge looks at, for one camera. Unlike
// seedServerData it adds nothing keyed by server, so several cameras of a server can be seeded.
func seedCameraHistory(t *testing.T, env *demofix.Env, cam db.GetCameraRow, tag string) {
	t.Helper()
	ctx := context.Background()
	evID, readID, jobID, ruleID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		stmts := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO events (id, tenant_id, site_id, server_id, camera_id, remote_id, severity, start_time, thumb_key, preview_key)
			  VALUES ($1, $2, $3, $4, $5, 'r-' || $6, 'alert', now(), 'thumb/' || $6, 'preview/' || $6)`,
				[]any{evID, cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, tag}},
			{`INSERT INTO lpr_reads (id, tenant_id, site_id, server_id, camera_id, remote_event_id, plate, plate_normalized, seen_at)
			  VALUES ($1, $2, $3, $4, $5, 'r-' || $6, 'AB123CD', 'AB123CD', now())`,
				[]any{readID, cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, tag}},
			{`INSERT INTO alarms (tenant_id, site_id, camera_id, event_id, source, status) VALUES ($1, $2, $3, $4, 'event', 'open')`,
				[]any{cam.TenantID, cam.SiteID, cam.ID, evID}},
			{`INSERT INTO exports (tenant_id, site_id, server_id, camera_id, requested_by, name, start_time, end_time)
			  VALUES ($1, $2, $3, $4, $5, 'exp', now(), now())`,
				[]any{cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, env.Admin.UserID}},
			{`INSERT INTO clip_watermark_jobs (tenant_id, site_id, server_id, camera_id, lpr_read_id, remote_event_id, requested_by, watermark_text, output_key)
			  VALUES ($1, $2, $3, $4, $5, 'r-' || $6, $7, 'wm', 'clip/' || $6)`,
				[]any{cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, readID, tag, env.Admin.UserID}},
			{`INSERT INTO object_tracks (server_id, tenant_id, camera_id, remote_object_id, start_time) VALUES ($1, $2, $3, 'trk-' || $4, now())`,
				[]any{cam.ServerID, cam.TenantID, cam.ID, tag}},
			{`INSERT INTO export_jobs (id, tenant_id, requested_by, name, start_time, end_time) VALUES ($1, $2, $3, 'job-' || $4, now(), now())`,
				[]any{jobID, cam.TenantID, env.Admin.UserID, tag}},
			{`INSERT INTO export_job_items (job_id, tenant_id, camera_id, server_id) VALUES ($1, $2, $3, $4)`,
				[]any{jobID, cam.TenantID, cam.ID, cam.ServerID}},
			{`INSERT INTO rules (id, tenant_id, name, trigger_type) VALUES ($1, $2, 'r-' || $3, 'event')`, []any{ruleID, cam.TenantID, tag}},
			{`INSERT INTO rule_firings (rule_id, resource_id, tenant_id, fired_at) VALUES ($1, $2, $3, now())`, []any{ruleID, cam.ID, cam.TenantID}},
		}
		for _, s := range stmts {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed %s: %v", tag, err)
	}
}

// ageDeletion makes the camera's tombstone look as if it was deleted `days` days ago.
func ageDeletion(t *testing.T, env *demofix.Env, cameraID any, days int) {
	t.Helper()
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE camera_tombstones SET deleted_at = now() - make_interval(days => $2) WHERE camera_id = $1`, cameraID, days)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func historyRows(t *testing.T, env *demofix.Env, cameraID any) map[string]int {
	t.Helper()
	q := map[string]string{
		"events":              `SELECT count(*) FROM events WHERE camera_id = $1`,
		"lpr_reads":           `SELECT count(*) FROM lpr_reads WHERE camera_id = $1`,
		"alarms":              `SELECT count(*) FROM alarms WHERE camera_id = $1`,
		"clip_watermark_jobs": `SELECT count(*) FROM clip_watermark_jobs WHERE camera_id = $1`,
		"object_tracks":       `SELECT count(*) FROM object_tracks WHERE camera_id = $1`,
		"rule_firings":        `SELECT count(*) FROM rule_firings WHERE resource_id = $1`,
		"exports":             `SELECT count(*) FROM exports WHERE camera_id = $1`,
		"export_job_items":    `SELECT count(*) FROM export_job_items WHERE camera_id = $1`,
	}
	out := map[string]int{}
	for name, sql := range q {
		out[name] = count(t, env, sql, cameraID)
	}
	return out
}

func TestPurgeRemovesAFinishedWindowsHistoryAndKeepsEvidenceAndOtherCameras(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	blobs := &recordingBlobs{}
	env.Svc.Blobs = blobs
	doomed := env.Cameras["frigate-h01/plaza"]
	kept := env.Cameras["frigate-h01/acceso_norte"]
	seedCameraHistory(t, env, doomed, "doomed")
	seedCameraHistory(t, env, kept, "kept")
	if err := env.Svc.RemoveCamera(ctx, env.Admin, doomed.ID); err != nil {
		t.Fatal(err)
	}
	ageDeletion(t, env, doomed.ID, 8) // the camera had 7 recording days

	purged, err := env.Svc.PurgeDeletedCameraHistory(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if purged != 1 {
		t.Fatalf("purged cameras = %d, want 1", purged)
	}

	for table, n := range historyRows(t, env, doomed.ID) {
		want := 0
		if table == "exports" || table == "export_job_items" {
			want = 1 // evidence people made is kept
		}
		if n != want {
			t.Errorf("%s rows of the deleted camera after the purge = %d, want %d", table, n, want)
		}
	}
	for table, n := range historyRows(t, env, kept.ID) {
		if n != 1 {
			t.Errorf("%s rows of another camera = %d, want 1 (untouched)", table, n)
		}
	}
	if got := deletedStateOf(t, env, doomed.ID); got.rows != 1 || !got.deletedAt || got.tombstones != 1 {
		t.Fatalf("camera after the purge = %+v, want the soft-deleted row and its tombstone kept", got)
	}
	if got := count(t, env, `SELECT count(*) FROM camera_tombstones WHERE camera_id = $1 AND purged_at IS NOT NULL`, doomed.ID); got != 1 {
		t.Fatalf("tombstones marked purged = %d, want 1", got)
	}
	if got := auditCount(t, env, "CAMERA_HISTORY_PURGED", doomed.ID); got != 1 {
		t.Fatalf("CAMERA_HISTORY_PURGED audit entries = %d, want 1", got)
	}
	blobs.mu.Lock()
	deleted := append([]string(nil), blobs.deleted...)
	blobs.mu.Unlock()
	want := map[string]bool{"thumb/doomed": true, "preview/doomed": true, "clip/doomed": true}
	for _, k := range deleted {
		if !want[k] {
			t.Errorf("deleted stored object %q, which does not belong to the purged camera", k)
		}
		delete(want, k)
	}
	if len(want) != 0 {
		t.Errorf("stored objects not deleted: %v", want)
	}

	// Frigate no longer lists the camera, so the sync adds nothing, and a second run has nothing to do.
	res, err := env.Svc.SyncServer(ctx, env.Admin, doomed.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 {
		t.Fatalf("sync after the purge added %d cameras, want 0", res.Added)
	}
	if again, err := env.Svc.PurgeDeletedCameraHistory(ctx, time.Now()); err != nil || again != 0 {
		t.Fatalf("second purge = (%d, %v), want nothing to do", again, err)
	}
}

func TestPurgeLeavesHistoryAloneBeforeTheWindowEndsWithoutAWindowOrAfterARestore(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	inWindow := env.Cameras["frigate-h01/plaza"]
	unknown := env.Cameras["frigate-h01/acceso_norte"]
	restored := env.Cameras["frigate-h01/cementerio"]
	seedCameraHistory(t, env, inWindow, "in-window")
	seedCameraHistory(t, env, unknown, "unknown")
	seedCameraHistory(t, env, restored, "restored")
	retain := int32(7)
	if err := env.Svc.DeleteCamera(ctx, env.Admin, inWindow.ID, &retain, nil); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.DeleteCamera(ctx, env.Admin, unknown.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.DeleteCamera(ctx, env.Admin, restored.ID, &retain, nil); err != nil {
		t.Fatal(err)
	}
	ageDeletion(t, env, inWindow.ID, 3)  // 7 days of window, 3 elapsed
	ageDeletion(t, env, unknown.ID, 400) // long ago, but no window recorded
	ageDeletion(t, env, restored.ID, 30)
	if _, err := env.Svc.RestoreCamera(ctx, env.Admin, restored.ID); err != nil {
		t.Fatal(err)
	}

	purged, err := env.Svc.PurgeDeletedCameraHistory(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if purged != 0 {
		t.Fatalf("purged cameras = %d, want 0", purged)
	}
	for name, cam := range map[string]any{"in window": inWindow.ID, "no known window": unknown.ID, "restored": restored.ID} {
		for table, n := range historyRows(t, env, cam) {
			if n != 1 {
				t.Errorf("%s: %s rows = %d, want 1 (history must stay)", name, table, n)
			}
		}
	}
}
