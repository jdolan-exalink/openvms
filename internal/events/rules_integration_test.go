//go:build integration

package events_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

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

func TestSyncerEvaluatesRulesOnNewEventsOnly(t *testing.T) {
	f := newAlarmFixture(t)
	f.syncer.Rules = rules.NewService(f.env.Store, nopPublisher{}, pgtest.Discard())
	f.enableAt(time.Now().Add(time.Hour)) // the built-in alarm stays out of the way
	f.addRule(map[string]any{"labels": []string{"zebra"}}, map[string]any{"create_alarm": true, "notify_in_app": true})

	f.putLabelled("rule-1", "zebra", time.Now().Add(-time.Minute))
	f.sync()
	// Frigate reports the same review again with more data: an update, not a new event.
	f.putLabelled("rule-1", "zebra", time.Now().Add(-time.Minute))
	f.sync()

	if n, a := f.counts(); n != 1 || a != 1 {
		t.Fatalf("after a new event and an update: %d notifications, %d rule alarms, want 1 and 1", n, a)
	}
}

func TestSyncerRuleNotMatchingCreatesNothing(t *testing.T) {
	f := newAlarmFixture(t)
	f.syncer.Rules = rules.NewService(f.env.Store, nopPublisher{}, pgtest.Discard())
	f.addRule(map[string]any{"labels": []string{"giraffe"}}, map[string]any{"create_alarm": true, "notify_in_app": true})

	f.putLabelled("rule-2", "zebra", time.Now().Add(-time.Minute))
	f.sync()

	if n, a := f.counts(); n != 0 || a != 0 {
		t.Fatalf("non-matching event: %d notifications, %d rule alarms, want 0 and 0", n, a)
	}
}
