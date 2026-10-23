//go:build integration

package events_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func TestRuleEvaluationWorkerProcessesEventAndFaceUpdateJobs(t *testing.T) {
	f := newAlarmFixture(t)
	f.enableAt(time.Now().Add(time.Hour))
	f.addRule(map[string]any{"labels": []string{fixtureLabel}}, map[string]any{"notify_in_app": true})
	f.putLabelled("worker-event-face-update", fixtureLabel, time.Now().Add(-time.Minute))
	f.sync()
	eventID := f.eventID("worker-event-face-update")
	ctx := context.Background()
	var eventJobID uuid.UUID
	var payload []byte
	err := f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE rule_evaluation_jobs SET status='completed' WHERE event_id<>$1 AND status='pending'`, eventID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id, payload FROM rule_evaluation_jobs WHERE event_id=$1 AND evaluation_kind='event'`, eventID).Scan(&eventJobID, &payload); err != nil {
			return err
		}
		var ev rules.EventContext
		if err := json.Unmarshal(payload, &ev); err != nil {
			return err
		}
		ev.FaceUpdate = true
		ev.SubLabels = []string{"new_face"}
		facePayload, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE rule_evaluation_jobs SET status='processing', attempts=1,
			lease_until=now()-interval '1 second', lease_token=gen_random_uuid() WHERE id=$1`, eventJobID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO rule_evaluation_jobs (event_id, evaluation_kind, face_names, payload, status)
			VALUES ($1, 'face_update', ARRAY['new_face'], $2, 'pending')`, eventID, facePayload)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := events.NewRuleEvaluationWorker(f.env.Store, rules.NewService(f.env.Store, nopPublisher{}, pgtest.Discard()), pgtest.Discard())
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var statuses []string
	err = f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT status FROM rule_evaluation_jobs WHERE event_id=$1 ORDER BY evaluation_kind`, eventID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var status string
			if err := rows.Scan(&status); err != nil {
				return err
			}
			statuses = append(statuses, status)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0] != "completed" || statuses[1] != "completed" {
		t.Fatalf("event and face-update statuses = %v, want both completed", statuses)
	}
	var notifications int
	if err := f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM notifications`).Scan(&notifications)
	}); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 {
		t.Fatalf("event and face-update notification effects = %d, want one event-only notification", notifications)
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM notifications`).Scan(&notifications)
	}); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 {
		t.Fatalf("notifications after rerunning completed jobs = %d, want no duplicate", notifications)
	}
}

func TestRuleEvaluationWorkerRollsBackEffectsAndFailsAfterBoundedRetries(t *testing.T) {
	f := newAlarmFixture(t)
	f.enableAt(time.Now().Add(time.Hour))
	f.addRule(map[string]any{"labels": []string{fixtureLabel}}, map[string]any{"create_alarm": true})
	f.putLabelled("worker-retry-rollback", fixtureLabel, time.Now().Add(-time.Minute))
	f.sync()
	eventID := f.eventID("worker-retry-rollback")
	ctx := context.Background()
	var jobID uuid.UUID
	var invalidCameraID uuid.UUID
	err := f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE rule_evaluation_jobs SET status='completed' WHERE event_id<>$1 AND status='pending'`, eventID); err != nil {
			return err
		}
		var payload []byte
		if err := tx.QueryRow(ctx, `SELECT id, payload FROM rule_evaluation_jobs WHERE event_id=$1 AND evaluation_kind='event'`, eventID).Scan(&jobID, &payload); err != nil {
			return err
		}
		var ev rules.EventContext
		if err := json.Unmarshal(payload, &ev); err != nil {
			return err
		}
		invalidCameraID = uuid.New()
		ev.CameraID = invalidCameraID // correlating the alarm fails after the rule firing was tentatively claimed.
		encoded, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE rule_evaluation_jobs SET payload=$2, status='pending' WHERE id=$1`, jobID, encoded)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := events.NewRuleEvaluationWorker(f.env.Store, rules.NewService(f.env.Store, nopPublisher{}, pgtest.Discard()), pgtest.Discard())
	var initialPublications int
	if err := f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM realtime_publication_outbox`).Scan(&initialPublications)
	}); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 5; attempt++ {
		if err := worker.RunOnce(ctx); err != nil {
			t.Fatalf("run %d: %v", attempt, err)
		}
		var status string
		var lastError string
		var firings int
		var publications int
		err := f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE rule_evaluation_jobs SET available_at=now() WHERE id=$1 AND status='pending'`, jobID); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT status, coalesce(last_error, '') FROM rule_evaluation_jobs WHERE id=$1`, jobID).Scan(&status, &lastError); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM rule_firings WHERE resource_id=$1`, invalidCameraID).Scan(&firings); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM realtime_publication_outbox`).Scan(&publications)
		})
		if err != nil {
			t.Fatal(err)
		}
		if attempt < 5 && status != "pending" {
			t.Fatalf("attempt %d status = %q, want pending retry", attempt, status)
		}
		if attempt == 5 && status != "failed" {
			t.Fatalf("attempt %d status = %q, want observable terminal failure", attempt, status)
		}
		if attempt == 5 && lastError == "" {
			t.Fatal("terminal failed job has no observable last_error")
		}
		if firings != 0 {
			t.Fatalf("attempt %d left %d rule firings after rollback, want 0", attempt, firings)
		}
		if publications != initialPublications {
			t.Fatalf("attempt %d changed realtime publications from %d to %d after rollback", attempt, initialPublications, publications)
		}
	}
}
