//go:build integration

package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// alarmStats returns the number of alarms of a source holding events of label, and the largest
// event_count / linked-event rows among them.
func (f *alarmFixture) alarmStats(source, label string) (alarms, eventCount, linked int) {
	f.t.Helper()
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `
SELECT count(DISTINCT a.id), coalesce(max(a.event_count), 0), count(ae.event_id)
FROM alarms a
JOIN alarm_events ae ON ae.alarm_id = a.id
JOIN events e ON e.id = ae.event_id
WHERE a.source = $1 AND $2 = ANY(e.labels)`, source, label).Scan(&alarms, &eventCount, &linked)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return alarms, eventCount, linked
}

func TestSyncerCorrelatesAlertsOfOneSituationIntoOneAlarm(t *testing.T) {
	f := newAlarmFixture(t)
	var merged []events.OpenedAlarm
	f.syncer.OnAlarmUpdated = func(_ context.Context, alarms []events.OpenedAlarm) { merged = append(merged, alarms...) }
	f.enableAt(time.Now().Add(-time.Hour))

	now := time.Now()
	f.putLabelled("corr-1", "zebra", now.Add(-3*time.Minute))
	f.putLabelled("corr-2", "zebra", now.Add(-2*time.Minute))
	f.putLabelled("corr-3", "zebra", now.Add(-time.Minute))
	f.sync()
	f.sync() // re-reading the same reviews changes nothing

	alarms, count, linked := f.alarmStats("event", "zebra")
	if alarms != 1 || count != 3 || linked != 3 {
		t.Fatalf("alarms=%d event_count=%d alarm_events=%d, want 1, 3, 3", alarms, count, linked)
	}
	f.mu.Lock()
	opened := len(f.opened)
	f.mu.Unlock()
	if opened < 1 || len(merged) < 1 {
		t.Fatalf("opened=%d merged=%d, want one alarm.opened and an update for the merged events", opened, len(merged))
	}
	for _, a := range merged {
		if a.Status == "" || a.ID.String() == "" {
			t.Fatalf("merged payload incomplete: %+v", a)
		}
	}
}

func TestSyncerKeepsDifferentLabelsApart(t *testing.T) {
	f := newAlarmFixture(t)
	f.enableAt(time.Now().Add(-time.Hour))
	now := time.Now()
	f.putLabelled("corr-lbl-1", "zebra", now.Add(-2*time.Minute))
	f.putLabelled("corr-lbl-2", "giraffe", now.Add(-time.Minute))
	f.sync()

	if a, _, _ := f.alarmStats("event", "zebra"); a != 1 {
		t.Fatalf("zebra alarms = %d, want 1", a)
	}
	if a, _, _ := f.alarmStats("event", "giraffe"); a != 1 {
		t.Fatalf("giraffe alarms = %d, want 1 (different label)", a)
	}
}

func TestRuleAlarmsCorrelateAndStaySeparateFromEventAlarms(t *testing.T) {
	f := newAlarmFixture(t)
	svc := rules.NewService(f.env.Store, nopPublisher{}, pgtest.Discard())
	svc.Debounce = time.Millisecond // let every event through the rule's firing debounce
	f.syncer.Rules = svc
	f.enableAt(time.Now().Add(-time.Hour))
	f.addRule(map[string]any{"labels": []string{"zebra"}}, map[string]any{"create_alarm": true})

	now := time.Now()
	f.putLabelled("corr-rule-1", "zebra", now.Add(-3*time.Minute))
	f.sync()
	time.Sleep(10 * time.Millisecond)
	f.putLabelled("corr-rule-2", "zebra", now.Add(-2*time.Minute))
	f.sync()

	if a, count, linked := f.alarmStats("rule", "zebra"); a != 1 || count != 2 || linked != 2 {
		t.Fatalf("rule alarms=%d event_count=%d alarm_events=%d, want 1, 2, 2", a, count, linked)
	}
	if a, count, _ := f.alarmStats("event", "zebra"); a != 1 || count != 2 {
		t.Fatalf("event alarms=%d event_count=%d, want a separate alarm of its own (1, 2)", a, count)
	}
}

func TestServerEventAlarmModeMigrationAndDefaults(t *testing.T) {
	f := newAlarmFixture(t)
	ctx := context.Background()

	// Demo servers simulate existing servers and use all_alerts; a newly registered server via
	// CreateServer defaults to rules_only and can be toggled with UpdateServer.
	srv, err := f.env.Svc.CreateServer(ctx, f.env.Admin, inventory.CreateServerInput{
		SiteID:        f.env.Cameras["frigate-h01/plaza"].SiteID,
		Name:          "frigate-new-default",
		ImportCameras: false,
		Conn: inventory.ConnInput{
			BaseURL:  f.env.Mocks["frigate-h01"].URL,
			Username: "admin",
			Password: "h-pass",
		},
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if srv.EventAlarmMode != "rules_only" {
		t.Fatalf("new server EventAlarmMode = %q, want rules_only", srv.EventAlarmMode)
	}

	mode := "all_alerts"
	updated, err := f.env.Svc.UpdateServer(ctx, f.env.Admin, srv.ID, inventory.UpdateServerInput{
		EventAlarmMode: &mode,
	})
	if err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}
	if updated.EventAlarmMode != "all_alerts" {
		t.Fatalf("updated server EventAlarmMode = %q, want all_alerts", updated.EventAlarmMode)
	}
}

func TestSyncerHonorsServerEventAlarmModeRulesOnly(t *testing.T) {
	f := newAlarmFixture(t)
	ctx := context.Background()
	svc := rules.NewService(f.env.Store, nopPublisher{}, pgtest.Discard())
	svc.Debounce = time.Millisecond
	f.syncer.Rules = svc
	f.enableAt(time.Now().Add(-time.Hour))
	f.addRule(map[string]any{"labels": []string{"zebra"}}, map[string]any{"create_alarm": true})

	h01ID := f.env.Cameras["frigate-h01/plaza"].ServerID
	mode := "rules_only"
	if _, err := f.env.Svc.UpdateServer(ctx, f.env.Admin, h01ID, inventory.UpdateServerInput{
		EventAlarmMode: &mode,
	}); err != nil {
		t.Fatalf("UpdateServer to rules_only: %v", err)
	}

	now := time.Now()
	f.putLabelled("mode-ro-1", "zebra", now.Add(-2*time.Minute))
	f.putLabelled("mode-ro-2", "giraffe", now.Add(-time.Minute))
	f.sync()

	if a, _, _ := f.alarmStats("event", "zebra"); a != 0 {
		t.Fatalf("event alarms for zebra in rules_only mode = %d, want 0", a)
	}
	if a, _, _ := f.alarmStats("event", "giraffe"); a != 0 {
		t.Fatalf("event alarms for giraffe in rules_only mode = %d, want 0", a)
	}
	if a, count, linked := f.alarmStats("rule", "zebra"); a != 1 || count != 1 || linked != 1 {
		t.Fatalf("rule alarms for zebra in rules_only mode = (%d, %d, %d), want (1, 1, 1)", a, count, linked)
	}
}

