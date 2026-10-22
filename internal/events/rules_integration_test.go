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

	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/frigatemock"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type nopPublisher struct{}

func (nopPublisher) Publish(context.Context, string, []byte) error { return nil }

func (f *alarmFixture) addRule(conditions, actions map[string]any) {
	f.t.Helper()
	c, _ := json.Marshal(conditions)
	a, _ := json.Marshal(actions)
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `
INSERT INTO rules (tenant_id, name, trigger_type, conditions, actions) VALUES ($1, 'Regla', 'event', $2, $3)`,
			f.env.Demo.TenantID, c, a)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

// putLabelled stores a recent open alert review on h01/plaza whose only object is label.
func (f *alarmFixture) putLabelled(id, label string, start time.Time) {
	ts := float64(start.UnixMicro()) / 1e6
	f.env.Mocks["frigate-h01"].Server.Store.Put(frigatemock.Review{
		ID: id, Camera: "plaza", StartTime: ts, Severity: "alert",
		ThumbPath: "/media/frigate/clips/review/thumb-plaza-" + id + ".webp",
		Data: frigatemock.ReviewData{
			Detections: []string{fmt.Sprintf("%f-%s", ts, id)}, Objects: []string{label},
			SubLabels: []string{}, Zones: []string{}, Audio: []string{},
		},
	})
}

func (f *alarmFixture) counts() (notifications, ruleAlarms int) {
	f.t.Helper()
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM notifications`).Scan(&notifications); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM alarms WHERE source = 'rule'`).Scan(&ruleAlarms)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return notifications, ruleAlarms
}

func syncAndEvaluateRuleJobs(f *alarmFixture, svc *rules.Service) {
	f.t.Helper()
	f.sync()
	worker := events.NewRuleEvaluationWorker(f.env.Store, svc, nil)
	for i := 0; i < 10; i++ {
		var pending int
		if err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), `SELECT count(*) FROM rule_evaluation_jobs WHERE status='pending' AND available_at <= now()`).Scan(&pending)
		}); err != nil {
			f.t.Fatal(err)
		}
		if pending == 0 {
			return
		}
		if err := worker.RunOnce(context.Background()); err != nil {
			f.t.Fatal(err)
		}
	}
	f.t.Fatal("rule evaluation jobs remained available after draining ten batches")
}

func waitForRuleJobStatus(t *testing.T, f *alarmFixture, eventID uuid.UUID, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), `SELECT status FROM rule_evaluation_jobs WHERE event_id=$1 AND evaluation_kind='event'`, eventID).Scan(&status)
		})
		if err != nil {
			t.Fatal(err)
		}
		if status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("rule evaluation job did not reach %q before timeout", want)
}

func TestSyncerEvaluatesRulesOnNewEventsOnly(t *testing.T) {
	f := newAlarmFixture(t)
	ruleService := rules.NewService(f.env.Store, nopPublisher{}, pgtest.Discard())
	f.enableAt(time.Now().Add(time.Hour)) // the built-in alarm stays out of the way
	f.addRule(map[string]any{"labels": []string{"zebra"}}, map[string]any{"create_alarm": true, "notify_in_app": true})

	f.putLabelled("rule-1", "zebra", time.Now().Add(-time.Minute))
	f.sync()
	eventID := f.eventID("rule-1")
	var status string
	if err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT status FROM rule_evaluation_jobs WHERE event_id=$1 AND evaluation_kind='event'`, eventID).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("sync-created rule job status = %q, want pending", status)
	}
	if n, a := f.counts(); n != 0 || a != 0 {
		t.Fatalf("sync evaluated rules inline: %d notifications, %d alarms, want 0 and 0", n, a)
	}
	worker := events.NewRuleEvaluationWorker(f.env.Store, ruleService, pgtest.Discard())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(ctx) }()
	waitForRuleJobStatus(t, f, eventID, "completed")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("rule evaluation worker did not stop after cancellation")
	}

	if n, a := f.counts(); n != 1 || a != 1 {
		t.Fatalf("after a new event and an update: %d notifications, %d rule alarms, want 1 and 1", n, a)
	}
}

func TestSyncerRuleNotMatchingCreatesNothing(t *testing.T) {
	f := newAlarmFixture(t)
	worker := events.NewRuleEvaluationWorker(f.env.Store, rules.NewService(f.env.Store, nopPublisher{}, pgtest.Discard()), pgtest.Discard())
	f.addRule(map[string]any{"labels": []string{"giraffe"}}, map[string]any{"create_alarm": true, "notify_in_app": true})

	f.putLabelled("rule-2", "zebra", time.Now().Add(-time.Minute))
	f.sync()
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	if n, a := f.counts(); n != 0 || a != 0 {
		t.Fatalf("non-matching event: %d notifications, %d rule alarms, want 0 and 0", n, a)
	}
}
