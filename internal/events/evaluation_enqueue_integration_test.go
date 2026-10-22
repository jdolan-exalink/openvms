//go:build integration

package events_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/frigatemock"
	"github.com/jdolan-exalink/openvms/internal/store"
	db "github.com/jdolan-exalink/openvms/internal/store/db"
)

func TestSyncerEnqueuesDurableRuleEvaluation(t *testing.T) {
	env, syncer, _ := setup(t)
	ctx := context.Background()
	srv := serverByName(t, ctx, env.Store, "frigate-h01")
	cam := env.Cameras["frigate-h01/plaza"]
	start := time.Now().Add(-30 * time.Second)
	put := func(names ...string) {
		ts := float64(start.UnixMicro()) / 1e6
		env.Mocks["frigate-h01"].Server.Store.Put(frigatemock.Review{
			ID: "durable-evaluation", Camera: cam.RemoteName, StartTime: ts, Severity: "alert",
			Data: frigatemock.ReviewData{Detections: []string{"durable-detection"}, Objects: []string{"person"}, SubLabels: names},
		})
	}
	put()
	if err := syncer.SyncServer(ctx, srv); err != nil {
		t.Fatal(err)
	}
	var eventID uuid.UUID
	var rows int
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id FROM events WHERE server_id=$1 AND remote_id=$2`, srv.ID, "durable-evaluation").Scan(&eventID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM rule_evaluation_jobs WHERE event_id=$1`, eventID).Scan(&rows)
	})
	if err != nil || rows != 1 {
		t.Fatalf("initial event jobs = %d, err=%v; want one", rows, err)
	}
	if err := syncer.SyncServer(ctx, srv); err != nil {
		t.Fatal(err)
	}
	put("zaira_face")
	if err := syncer.SyncServer(ctx, srv); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	var facePayload []byte
	err = env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT evaluation_kind, payload FROM rule_evaluation_jobs WHERE event_id=$1 ORDER BY evaluation_kind`, eventID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var kind string
			var payload []byte
			if err := rows.Scan(&kind, &payload); err != nil {
				return err
			}
			kinds = append(kinds, kind)
			if kind == "face_update" {
				facePayload = payload
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(kinds) != "[event face_update]" {
		t.Fatalf("evaluation kinds = %v, want one event and one face_update", kinds)
	}
	var payload struct {
		SubLabels []string
	}
	if err := json.Unmarshal(facePayload, &payload); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(payload.SubLabels) != "[zaira_face]" {
		t.Fatalf("face update payload names = %v, want only gained name", payload.SubLabels)
	}
	put("zaira_face", "maria_face")
	if err := syncer.SyncServer(ctx, srv); err != nil {
		t.Fatal(err)
	}
	var laterFacePayload []byte
	err = env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload FROM rule_evaluation_jobs WHERE event_id=$1 AND evaluation_kind='face_update' AND face_names=ARRAY['maria_face']`, eventID).Scan(&laterFacePayload)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(laterFacePayload, &payload); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(payload.SubLabels) != "[maria_face]" {
		t.Fatalf("later face update payload names = %v, want only newly gained name", payload.SubLabels)
	}
	if err := syncer.SyncServer(ctx, srv); err != nil {
		t.Fatal(err)
	}
	if err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM rule_evaluation_jobs WHERE event_id=$1`, eventID).Scan(&rows)
	}); err != nil || rows != 3 {
		t.Fatalf("jobs after duplicate sync = %d, err=%v; want one event and two face batches", rows, err)
	}
}

func TestRuleEvaluationJobLifecycle(t *testing.T) {
	env, syncer, _ := setup(t)
	ctx := context.Background()
	srv := serverByName(t, ctx, env.Store, "frigate-h01")
	cam := env.Cameras["frigate-h01/plaza"]
	ts := float64(time.Now().Add(-30*time.Second).UnixMicro()) / 1e6
	env.Mocks["frigate-h01"].Server.Store.Put(frigatemock.Review{
		ID: "job-lifecycle", Camera: cam.RemoteName, StartTime: ts, Severity: "alert",
		Data: frigatemock.ReviewData{Detections: []string{"lifecycle-detection"}, Objects: []string{"person"}},
	})
	if err := syncer.SyncServer(ctx, srv); err != nil {
		t.Fatal(err)
	}
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		queries := db.New(tx)
		var jobID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM rule_evaluation_jobs WHERE event_id=(SELECT id FROM events WHERE server_id=$1 AND remote_id='job-lifecycle')`, srv.ID).Scan(&jobID); err != nil {
			return err
		}
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM rule_evaluation_jobs WHERE id=$1`, jobID).Scan(&status); err != nil {
			return err
		}
		if status != "completed" {
			return fmt.Errorf("new synchronous-era job status = %q, want completed", status)
		}
		var legacyID uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO rule_evaluation_jobs (event_id, evaluation_kind, face_names, payload)
			SELECT event_id, 'face_update', ARRAY['legacy'], payload FROM rule_evaluation_jobs WHERE id=$1 RETURNING id`, jobID).Scan(&legacyID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT status FROM rule_evaluation_jobs WHERE id=$1`, legacyID).Scan(&status); err != nil {
			return err
		}
		if status != "completed" {
			return fmt.Errorf("legacy insert default status = %q, want completed", status)
		}
		claimed, err := queries.ClaimRuleEvaluationJobs(ctx, db.ClaimRuleEvaluationJobsParams{
			MaxRows: 1, LeaseSeconds: 60, MaxAttempts: 3,
		})
		if err != nil || len(claimed) != 0 {
			return fmt.Errorf("claim completed job = %d rows, err=%v; want none", len(claimed), err)
		}
		if _, err := tx.Exec(ctx, `UPDATE rule_evaluation_jobs SET status='pending' WHERE id=$1`, jobID); err != nil {
			return err
		}
		claimed, err = queries.ClaimRuleEvaluationJobs(ctx, db.ClaimRuleEvaluationJobsParams{
			MaxRows: 1, LeaseSeconds: 60, MaxAttempts: 3,
		})
		if err != nil || len(claimed) != 1 || claimed[0].Attempts != 1 || claimed[0].Status != "processing" {
			return fmt.Errorf("first claim = %#v, err=%v; want processing attempt 1", claimed, err)
		}
		transient := "transient"
		if rows, err := queries.RetryRuleEvaluationJob(ctx, db.RetryRuleEvaluationJobParams{
			ID: jobID, LeaseToken: claimed[0].LeaseToken, MaxAttempts: 3, RetrySeconds: 60, LastError: &transient,
		}); err != nil || rows != 1 {
			return fmt.Errorf("retry rows=%d, err=%v; want 1", rows, err)
		}
		claimed, err = queries.ClaimRuleEvaluationJobs(ctx, db.ClaimRuleEvaluationJobsParams{
			MaxRows: 1, LeaseSeconds: 60, MaxAttempts: 3,
		})
		if err != nil || len(claimed) != 0 {
			return fmt.Errorf("claim during retry backoff = %#v, err=%v; want none", claimed, err)
		}
		if _, err := tx.Exec(ctx, `UPDATE rule_evaluation_jobs SET available_at=now() WHERE id=$1`, jobID); err != nil {
			return err
		}
		claimed, err = queries.ClaimRuleEvaluationJobs(ctx, db.ClaimRuleEvaluationJobsParams{
			MaxRows: 1, LeaseSeconds: 60, MaxAttempts: 3,
		})
		if err != nil || len(claimed) != 1 || claimed[0].Attempts != 2 {
			return fmt.Errorf("retry claim = %#v, err=%v; want attempt 2", claimed, err)
		}
		if _, err := tx.Exec(ctx, `UPDATE rule_evaluation_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, jobID); err != nil {
			return err
		}
		claimed, err = queries.ClaimRuleEvaluationJobs(ctx, db.ClaimRuleEvaluationJobsParams{
			MaxRows: 1, LeaseSeconds: 60, MaxAttempts: 2,
		})
		if err != nil || len(claimed) != 0 {
			return fmt.Errorf("claim exhausted expired lease = %#v, err=%v; want terminalize without reclaim", claimed, err)
		}
		var terminal string
		if err := tx.QueryRow(ctx, `SELECT status FROM rule_evaluation_jobs WHERE id=$1`, jobID).Scan(&terminal); err != nil {
			return err
		}
		if terminal != "failed" {
			return fmt.Errorf("exhausted job status = %q, want failed", terminal)
		}
		if _, err := tx.Exec(ctx, `UPDATE rule_evaluation_jobs SET status='pending', attempts=0, available_at=now() WHERE id=$1`, jobID); err != nil {
			return err
		}
		claimed, err = queries.ClaimRuleEvaluationJobs(ctx, db.ClaimRuleEvaluationJobsParams{
			MaxRows: 1, LeaseSeconds: 60, MaxAttempts: 3,
		})
		if err != nil || len(claimed) != 1 {
			return fmt.Errorf("claim for ack = %#v, err=%v; want one job", claimed, err)
		}
		staleLease := claimed[0].LeaseToken
		if _, err := tx.Exec(ctx, `UPDATE rule_evaluation_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, jobID); err != nil {
			return err
		}
		claimed, err = queries.ClaimRuleEvaluationJobs(ctx, db.ClaimRuleEvaluationJobsParams{
			MaxRows: 1, LeaseSeconds: 60, MaxAttempts: 3,
		})
		if err != nil || len(claimed) != 1 || claimed[0].LeaseToken == staleLease {
			return fmt.Errorf("reclaimed lease = %#v, err=%v; want a new token", claimed, err)
		}
		if rows, err := queries.CompleteRuleEvaluationJob(ctx, db.CompleteRuleEvaluationJobParams{
			ID: jobID, LeaseToken: staleLease,
		}); err != nil || rows != 0 {
			return fmt.Errorf("stale lease ack rows=%d, err=%v; want 0", rows, err)
		}
		if rows, err := queries.CompleteRuleEvaluationJob(ctx, db.CompleteRuleEvaluationJobParams{
			ID: jobID, LeaseToken: claimed[0].LeaseToken,
		}); err != nil || rows != 1 {
			return fmt.Errorf("current lease ack rows=%d, err=%v; want 1", rows, err)
		}
		var completedAt *time.Time
		if err := tx.QueryRow(ctx, `SELECT completed_at FROM rule_evaluation_jobs WHERE id=$1`, jobID).Scan(&completedAt); err != nil {
			return err
		}
		if completedAt == nil {
			return fmt.Errorf("ack completed_at is nil")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRuleEvaluationEventAndCursorWritesRollbackTogether(t *testing.T) {
	env, syncer, _ := setup(t)
	ctx := context.Background()
	srv := serverByName(t, ctx, env.Store, "frigate-h01")
	cam := env.Cameras["frigate-h01/plaza"]
	if err := syncer.SyncServer(ctx, srv); err != nil {
		t.Fatal(err)
	}
	var beforeCursor *time.Time
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT review_cursor FROM event_sync_state WHERE server_id=$1`, srv.ID).Scan(&beforeCursor)
	})
	if err != nil {
		t.Fatal(err)
	}
	var enabledAt time.Time
	err = env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `DELETE FROM alarm_settings RETURNING enabled_at`).Scan(&enabledAt)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), `INSERT INTO alarm_settings (singleton, enabled_at) VALUES (true, $1) ON CONFLICT (singleton) DO UPDATE SET enabled_at=excluded.enabled_at`, enabledAt)
			return err
		})
	})
	start := time.Now().Add(time.Second)
	ts := float64(start.UnixMicro()) / 1e6
	env.Mocks["frigate-h01"].Server.Store.Put(frigatemock.Review{
		ID: "rollback-ingestion", Camera: cam.RemoteName, StartTime: ts, Severity: "alert",
		Data: frigatemock.ReviewData{Detections: []string{"rollback-detection"}, Objects: []string{"person"}},
	})
	if err := syncer.SyncServer(ctx, srv); err == nil {
		t.Fatal("sync succeeded despite missing alarm settings")
	}
	var events, jobs int
	var afterCursor *time.Time
	err = env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM events WHERE server_id=$1 AND remote_id='rollback-ingestion'`, srv.ID).Scan(&events); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM rule_evaluation_jobs WHERE event_id IN (SELECT id FROM events WHERE server_id=$1 AND remote_id='rollback-ingestion')`, srv.ID).Scan(&jobs); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT review_cursor FROM event_sync_state WHERE server_id=$1`, srv.ID).Scan(&afterCursor)
	})
	if err != nil || events != 0 || jobs != 0 || beforeCursor == nil || afterCursor == nil || !beforeCursor.Equal(*afterCursor) {
		t.Fatalf("after failed transaction: events=%d jobs=%d cursor=%v (before %v) err=%v; want all unchanged", events, jobs, afterCursor, beforeCursor, err)
	}
}
