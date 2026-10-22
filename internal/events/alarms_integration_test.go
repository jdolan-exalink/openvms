//go:build integration

package events_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/frigatemock"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// alarmFixture drives one mock Frigate review through the real syncer.
type alarmFixture struct {
	t      *testing.T
	env    *demofix.Env
	syncer *events.Syncer
	mu     sync.Mutex
	opened []events.OpenedAlarm
}

func newAlarmFixture(t *testing.T) *alarmFixture {
	t.Helper()
	env, syncer, _ := setup(t)
	f := &alarmFixture{t: t, env: env, syncer: syncer}
	for name, m := range env.Mocks {
		for _, r := range m.Server.Store.List(frigatemock.Query{}) {
			for _, o := range r.Data.Objects {
				if o == fixtureLabel {
					t.Fatalf("demo seed review %s on %s uses fixtureLabel %q; pick a label the seed never produces", r.ID, name, fixtureLabel)
				}
			}
		}
	}
	syncer.OnAlarm = func(_ context.Context, alarms []events.OpenedAlarm) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.opened = append(f.opened, alarms...)
	}
	return f
}

// enableAt moves the "alarms enabled since" instant the syncer compares event start times with.
func (f *alarmFixture) enableAt(at time.Time) {
	f.t.Helper()
	if _, err := f.env.Pool.Exec(context.Background(), `UPDATE alarm_settings SET enabled_at = $1`, at); err != nil {
		f.t.Fatal(err)
	}
}

// fixtureLabel is a label the demo seed never produces. Seeded alerts open alarms at random
// instants relative to "now" on the first sync; an event sharing their camera and label could merge
// into one of them (CorrelationKey) instead of opening its own alarm, which made these tests flaky.
const fixtureLabel = "drone"

// put stores (or replaces) a recent open review with the given severity on h01/plaza.
func (f *alarmFixture) put(id, severity string, start time.Time) {
	ts := float64(start.UnixMicro()) / 1e6
	f.env.Mocks["frigate-h01"].Server.Store.Put(frigatemock.Review{
		ID: id, Camera: "plaza", StartTime: ts, Severity: severity,
		ThumbPath: "/media/frigate/clips/review/thumb-plaza-" + id + ".webp",
		Data: frigatemock.ReviewData{
			Detections: []string{fmt.Sprintf("%f-%s", ts, id)}, Objects: []string{fixtureLabel},
			SubLabels: []string{}, Zones: []string{}, Audio: []string{},
		},
	})
}

func (f *alarmFixture) sync() { f.syncer.SyncAll(context.Background()) }

func (f *alarmFixture) alarmCount(remoteID string) int {
	f.t.Helper()
	var n int
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT count(*) FROM alarms a JOIN events e ON e.id = a.event_id WHERE e.remote_id = $1`, remoteID).Scan(&n)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *alarmFixture) openedFor(eventID uuid.UUID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.opened {
		if a.EventID == eventID {
			n++
		}
	}
	return n
}

func (f *alarmFixture) eventID(remoteID string) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT id FROM events WHERE remote_id = $1`, remoteID).Scan(&id)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func TestAlertEventOpensOneAlarmAcrossSyncs(t *testing.T) {
	f := newAlarmFixture(t)
	f.enableAt(time.Now().Add(-2 * time.Hour))
	f.put("alarm-alert-1", "alert", time.Now().Add(-30*time.Second))

	f.sync()
	f.sync()

	if n := f.alarmCount("alarm-alert-1"); n != 1 {
		t.Fatalf("alarms for the alert event = %d, want exactly 1 after two syncs", n)
	}
	eventID := f.eventID("alarm-alert-1")
	if n := f.openedFor(eventID); n != 1 {
		t.Fatalf("OnAlarm reported %d openings for the event, want 1 (a re-upsert must not re-announce)", n)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.opened {
		if a.EventID == eventID {
			if a.Status != "open" || a.TenantID != f.env.Demo.TenantID || a.ID == uuid.Nil || a.CameraID == uuid.Nil || a.SiteID == uuid.Nil || a.CreatedAt.IsZero() {
				t.Fatalf("opened alarm payload incomplete: %+v", a)
			}
		}
	}
}

func TestAlertSyncPersistsRealtimeAnnouncementsExactlyOnce(t *testing.T) {
	f := newAlarmFixture(t)
	f.enableAt(time.Now().Add(-2 * time.Hour))
	f.put("outbox-alert-1", "alert", time.Now().Add(-30*time.Second))
	f.sync()
	firstEventID := f.eventID("outbox-alert-1")
	f.put("outbox-alert-2", "alert", time.Now().Add(-20*time.Second))
	f.sync()
	secondEventID := f.eventID("outbox-alert-2")
	f.sync()

	type announcement struct {
		id        uuid.UUID
		subject   string
		payload   json.RawMessage
		aggregate *string
		sequence  *int64
	}
	var messages []announcement
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT id, subject, payload, aggregate_key, aggregate_sequence
			FROM realtime_publication_outbox
			WHERE payload->>'id' IN ($1, $2) OR payload->>'event_id' IN ($1, $2)
			ORDER BY aggregate_key NULLS FIRST, aggregate_sequence NULLS FIRST`, firstEventID.String(), secondEventID.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row announcement
			if err := rows.Scan(&row.id, &row.subject, &row.payload, &row.aggregate, &row.sequence); err != nil {
				return err
			}
			messages = append(messages, row)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 {
		t.Fatalf("outbox announcements for both events = %d, want two event, one opened and one updated", len(messages))
	}
	eventCount, openedCount, updatedCount := 0, 0, 0
	var alarmKey string
	for _, message := range messages {
		var payload map[string]any
		if err := json.Unmarshal(message.payload, &payload); err != nil {
			t.Fatalf("decode %s payload: %v", message.subject, err)
		}
		switch message.subject {
		case "frigate.event.new." + f.env.Demo.TenantID.String():
			eventID, ok := payload["id"].(string)
			if !ok || (eventID != firstEventID.String() && eventID != secondEventID.String()) || message.aggregate != nil || message.sequence != nil {
				t.Fatalf("event announcement metadata/payload = %+v %+v", message, payload)
			}
			eventCount++
		case "alarm.opened." + f.env.Demo.TenantID.String():
			id, ok := payload["id"].(string)
			if !ok || payload["event_id"] != firstEventID.String() || message.aggregate == nil || *message.aggregate != "alarm/"+id || message.sequence == nil || *message.sequence != 1 {
				t.Fatalf("alarm opened metadata/payload = %+v %+v", message, payload)
			}
			alarmKey = *message.aggregate
			openedCount++
		case "alarm.updated." + f.env.Demo.TenantID.String():
			if payload["event_id"] != secondEventID.String() || message.aggregate == nil || *message.aggregate != alarmKey || message.sequence == nil || *message.sequence != 2 {
				t.Fatalf("alarm updated metadata/payload = %+v %+v", message, payload)
			}
			updatedCount++
		default:
			t.Fatalf("unexpected announcement subject %q", message.subject)
		}
	}
	if eventCount != 2 || openedCount != 1 || updatedCount != 1 {
		t.Fatalf("outbox announcements: events=%d opened=%d updated=%d", eventCount, openedCount, updatedCount)
	}
}

func TestDetectionEventOpensNoAlarm(t *testing.T) {
	f := newAlarmFixture(t)
	f.enableAt(time.Now().Add(-2 * time.Hour))
	f.put("alarm-detection-1", "detection", time.Now().Add(-30*time.Second))

	f.sync()

	if n := f.alarmCount("alarm-detection-1"); n != 0 {
		t.Fatalf("alarms for a detection event = %d, want 0", n)
	}
	if n := f.openedFor(f.eventID("alarm-detection-1")); n != 0 {
		t.Fatalf("OnAlarm reported %d openings for a detection, want 0", n)
	}
}

func TestDetectionPromotedToAlertOpensAlarm(t *testing.T) {
	f := newAlarmFixture(t)
	f.enableAt(time.Now().Add(-2 * time.Hour))
	start := time.Now().Add(-30 * time.Second)
	f.put("alarm-promote-1", "detection", start)
	f.sync()
	if n := f.alarmCount("alarm-promote-1"); n != 0 {
		t.Fatalf("alarms before promotion = %d, want 0", n)
	}

	f.put("alarm-promote-1", "alert", start)
	f.sync()

	if n := f.alarmCount("alarm-promote-1"); n != 1 {
		t.Fatalf("alarms after promotion = %d, want 1", n)
	}
	if n := f.openedFor(f.eventID("alarm-promote-1")); n != 1 {
		t.Fatalf("OnAlarm reported %d openings after promotion, want 1", n)
	}
}

func TestAlertDowngradedToDetectionKeepsAlarm(t *testing.T) {
	f := newAlarmFixture(t)
	f.enableAt(time.Now().Add(-2 * time.Hour))
	start := time.Now().Add(-30 * time.Second)
	f.put("alarm-downgrade-1", "alert", start)
	f.sync()

	f.put("alarm-downgrade-1", "detection", start)
	f.sync()

	if n := f.alarmCount("alarm-downgrade-1"); n != 1 {
		t.Fatalf("alarms after downgrade = %d, want the existing one kept", n)
	}
	var sev string
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT severity FROM events WHERE remote_id = 'alarm-downgrade-1'`).Scan(&sev)
	})
	if err != nil || sev != "detection" {
		t.Fatalf("event severity = %q (%v), want the downgrade to have been applied", sev, err)
	}
}

func TestEventStartedBeforeEnablementOpensNoAlarm(t *testing.T) {
	f := newAlarmFixture(t)
	f.enableAt(time.Now())
	f.put("alarm-old-1", "alert", time.Now().Add(-30*time.Second))

	f.sync()

	if n := f.alarmCount("alarm-old-1"); n != 0 {
		t.Fatalf("alarms for an event that started before enablement = %d, want 0 (no backfill)", n)
	}
	if n := f.openedFor(f.eventID("alarm-old-1")); n != 0 {
		t.Fatalf("OnAlarm reported %d openings before enablement, want 0", n)
	}
}

func TestMigrationCreatesNoAlarms(t *testing.T) {
	f := newAlarmFixture(t)
	var n int
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM alarms`).Scan(&n)
	})
	if err != nil || n != 0 {
		t.Fatalf("alarms before any sync = %d (%v), want 0", n, err)
	}
}

func TestAlarmsAreTenantIsolated(t *testing.T) {
	f := newAlarmFixture(t)
	f.enableAt(time.Now().Add(-2 * time.Hour))
	f.put("alarm-rls-1", "alert", time.Now().Add(-30*time.Second))
	f.sync()

	count := func(scope store.TenantScope) int {
		var n int
		err := f.env.Store.TxRaw(context.Background(), scope, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), `SELECT count(*) FROM alarms`).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(store.TenantScope{TenantID: f.env.Demo.TenantID}); n == 0 {
		t.Fatal("the owning tenant sees no alarms")
	}
	if n := count(store.TenantScope{TenantID: uuid.New()}); n != 0 {
		t.Fatalf("another tenant sees %d alarms, want 0", n)
	}
}
