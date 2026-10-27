//go:build integration

package events_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func (f *alarmFixture) newEvaluationWorker() *events.RuleEvaluationWorker {
	return events.NewRuleEvaluationWorker(f.env.Store, rules.NewService(f.env.Store, nopPublisher{}, pgtest.Discard()), pgtest.Discard())
}

// jobStates counts evaluation jobs by status.
func (f *alarmFixture) jobStates() map[string]int {
	f.t.Helper()
	out := map[string]int{}
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT status, count(*) FROM rule_evaluation_jobs GROUP BY status`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var status string
			var n int
			if err := rows.Scan(&status, &n); err != nil {
				return err
			}
			out[status] = n
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// Several replicas drain one queue: SKIP LOCKED plus the lease token must hand each job to a
// single replica, so every matching event notifies exactly once (one rule per event, see below).
func TestRuleEvaluationWorkersShareOneQueueWithoutDuplicateEffects(t *testing.T) {
	const eventCount, replicas = 12, 4
	f := newAlarmFixture(t)
	f.enableAt(time.Now().Add(time.Hour))
	// One rule and label per event: a rule's cooldown collapses repeated matches into a single
	// notification by design, so a shared rule could not tell a duplicate from a suppressed event.
	for i := 0; i < eventCount; i++ {
		label := fmt.Sprintf("replica-label-%02d", i)
		f.addRule(map[string]any{"labels": []string{label}}, map[string]any{"notify_in_app": true})
		f.putLabelled(fmt.Sprintf("replica-event-%02d", i), label, time.Now().Add(-time.Minute))
	}
	f.sync()
	if got := f.jobStates()["pending"]; got < eventCount {
		t.Fatalf("pending jobs after sync = %d, want at least %d", got, eventCount)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	errs := make(chan error, replicas)
	var wg sync.WaitGroup
	for r := 0; r < replicas; r++ {
		worker := f.newEvaluationWorker()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				if err := worker.RunOnce(ctx); err != nil {
					errs <- err
					return
				}
				states := f.jobStates()
				if states["pending"] == 0 && states["processing"] == 0 {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("replica failed: %v", err)
	}

	states := f.jobStates()
	if states["pending"] != 0 || states["processing"] != 0 || states["failed"] != 0 {
		t.Fatalf("job states after draining = %v, want nothing pending, processing or failed", states)
	}
	if notifications, _ := f.counts(); notifications != eventCount {
		t.Fatalf("notifications = %d for %d matching events across %d replicas, want exactly one each", notifications, eventCount, replicas)
	}
}

// A replica that is still holding a lease owns its job; another replica must leave it alone
// until the lease expires, and then finish it exactly once.
func TestRuleEvaluationWorkerRespectsAnActiveLeaseThenRecoversAfterExpiry(t *testing.T) {
	f := newAlarmFixture(t)
	f.enableAt(time.Now().Add(time.Hour))
	f.addRule(map[string]any{"labels": []string{fixtureLabel}}, map[string]any{"notify_in_app": true})
	f.putLabelled("leased-event", fixtureLabel, time.Now().Add(-time.Minute))
	f.sync()
	eventID := f.eventID("leased-event")
	ctx := context.Background()
	setLease := func(interval string) {
		t.Helper()
		err := f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE rule_evaluation_jobs SET status='completed' WHERE event_id<>$1 AND status='pending'`, eventID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE rule_evaluation_jobs SET status='processing', attempts=1,
				lease_until=now()+interval '%s', lease_token=gen_random_uuid() WHERE event_id=$1`, interval), eventID)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	jobStatus := func() string {
		t.Helper()
		var status string
		err := f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT status FROM rule_evaluation_jobs WHERE event_id=$1`, eventID).Scan(&status)
		})
		if err != nil {
			t.Fatal(err)
		}
		return status
	}

	setLease("1 hour")
	worker := f.newEvaluationWorker()
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := jobStatus(); got != "processing" {
		t.Fatalf("job status with an active lease = %q, want it left processing", got)
	}
	if notifications, _ := f.counts(); notifications != 0 {
		t.Fatalf("notifications while another replica holds the lease = %d, want 0", notifications)
	}

	setLease("-1 second")
	for i := 0; i < 2; i++ {
		if err := worker.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := jobStatus(); got != "completed" {
		t.Fatalf("job status after the lease expired = %q, want completed", got)
	}
	if notifications, _ := f.counts(); notifications != 1 {
		t.Fatalf("notifications after recovery and a rerun = %d, want exactly one", notifications)
	}
}

// Records how long one worker takes to process queued evaluations once it claims them: the
// per-job figure feeds docs/operations/alarm-evaluation-latency.md. The bound is deliberately
// loose, it only catches a severe regression; the numbers are for people, not for the assertion.
func TestRuleEvaluationWorkerProcessingTimePerJob(t *testing.T) {
	f := newAlarmFixture(t)
	f.enableAt(time.Now().Add(time.Hour))
	f.addRule(map[string]any{"labels": []string{fixtureLabel}}, map[string]any{"notify_in_app": true})
	for i := 0; i < 50; i++ {
		f.putLabelled(fmt.Sprintf("latency-event-%02d", i), fixtureLabel, time.Now().Add(-time.Minute))
	}
	f.sync()
	queued := f.jobStates()["pending"]
	if queued < 50 {
		t.Fatalf("pending jobs = %d, want at least 50", queued)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	worker := f.newEvaluationWorker()
	started := time.Now()
	for ctx.Err() == nil {
		if err := worker.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		if states := f.jobStates(); states["pending"] == 0 && states["processing"] == 0 {
			break
		}
	}
	drain := time.Since(started)
	perJob := drain / time.Duration(queued)
	t.Logf("PROCESSING jobs=%d drain=%s per_job=%s", queued, drain.Round(time.Millisecond), perJob.Round(100*time.Microsecond))
	if perJob > 500*time.Millisecond {
		t.Fatalf("processing time per job = %s, want well under 500ms", perJob)
	}
}
