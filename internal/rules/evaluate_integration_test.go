//go:build integration

package rules_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type recordingPublisher struct {
	mu       sync.Mutex
	subjects []string
}

func (p *recordingPublisher) Publish(_ context.Context, subject string, _ []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subjects = append(p.subjects, subject)
	return nil
}

func (p *recordingPublisher) count(prefix string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, s := range p.subjects {
		if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}

type fixture struct {
	t   *testing.T
	env *demofix.Env
	pub *recordingPublisher
	svc *rules.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	env := demofix.Setup(t)
	pub := &recordingPublisher{}
	return &fixture{t: t, env: env, pub: pub, svc: rules.NewService(env.Store, pub, pgtest.Discard())}
}

// exec and scan go through TxRaw because the tables are row-level-secured for the pool user.
func (f *fixture) exec(sql string, args ...any) error {
	return f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), sql, args...)
		return err
	})
}

func (f *fixture) scan(dst any, sql string, args ...any) error {
	return f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), sql, args...).Scan(dst)
	})
}

func (f *fixture) addRule(name string, trigger rules.TriggerType, cond rules.Conditions, act rules.Actions, enabled bool) uuid.UUID {
	f.t.Helper()
	c, _ := json.Marshal(cond)
	a, _ := json.Marshal(act)
	var id uuid.UUID
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `
INSERT INTO rules (tenant_id, name, trigger_type, conditions, actions, enabled)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			f.env.Demo.TenantID, name, string(trigger), c, a, enabled).Scan(&id)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fixture) notifications(ruleID uuid.UUID) int {
	f.t.Helper()
	var n int
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM notifications WHERE rule_id = $1`, ruleID).Scan(&n)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

type publication struct {
	subject      string
	payload      []byte
	aggregateKey *string
}

func (f *fixture) publications() []publication {
	f.t.Helper()
	var rows []publication
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		result, err := tx.Query(context.Background(), `SELECT subject, payload, aggregate_key FROM realtime_publication_outbox ORDER BY created_at, id`)
		if err != nil {
			return err
		}
		defer result.Close()
		for result.Next() {
			var row publication
			if err := result.Scan(&row.subject, &row.payload, &row.aggregateKey); err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return result.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return rows
}

func (f *fixture) eventCtx(labels ...string) rules.EventContext {
	cam := f.env.Cameras["frigate-h01/acceso_norte"]
	return rules.EventContext{
		ID: uuid.New(), TenantID: cam.TenantID, SiteID: cam.SiteID, ServerID: cam.ServerID, CameraID: cam.ID,
		CameraName: cam.DisplayName, Severity: "alert", Labels: labels, Start: time.Now(),
	}
}

func TestEvaluateEventNotifiesOnMatchOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.addRule("Personas", rules.TriggerEvent, rules.Conditions{Labels: []string{"person"}}, rules.Actions{NotifyInApp: true}, true)

	if err := f.svc.EvaluateEvent(ctx, f.eventCtx("car")); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(id); n != 0 {
		t.Fatalf("non-matching event created %d notifications, want 0", n)
	}
	if err := f.svc.EvaluateEvent(ctx, f.eventCtx("person")); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(id); n != 1 {
		t.Fatalf("matching event created %d notifications, want 1", n)
	}
	if n := f.pub.count("notification.created."); n != 0 {
		t.Fatalf("directly published %d notification.created messages, want none", n)
	}
	rows := f.publications()
	if len(rows) != 1 || rows[0].subject != "notification.created."+f.env.Demo.TenantID.String() {
		t.Fatalf("outbox publications = %+v, want one notification.created row", rows)
	}
	var payload rules.NotificationCreatedPayload
	if err := json.Unmarshal(rows[0].payload, &payload); err != nil {
		t.Fatalf("decode notification payload: %v", err)
	}
	if payload.TenantID != f.env.Demo.TenantID || payload.ID == uuid.Nil || payload.Title != "Personas" {
		t.Fatalf("notification payload = %+v, want persisted notification", payload)
	}
}

func TestEvaluateEventTxUsesCallerTransaction(t *testing.T) {
	f := newFixture(t)
	id := f.addRule("Personas", rules.TriggerEvent, rules.Conditions{Labels: []string{"person"}}, rules.Actions{NotifyInApp: true}, true)
	ev := f.eventCtx("person")
	rollback := errors.New("rollback caller transaction")
	err := f.env.Store.TxRaw(context.Background(), store.ScopeFor(authz.Actor{TenantID: &ev.TenantID}), func(tx pgx.Tx) error {
		if err := f.svc.EvaluateEventTx(context.Background(), tx, ev); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("TxRaw error = %v, want caller rollback", err)
	}
	if n := f.notifications(id); n != 0 {
		t.Fatalf("caller rollback left %d notifications, want 0", n)
	}
	if rows := f.publications(); len(rows) != 0 {
		t.Fatalf("caller rollback left %d realtime publications, want 0", len(rows))
	}
}

func TestEvaluateEventAlarmOutboxUsesBoundEvent(t *testing.T) {
	f := newFixture(t)
	f.addRule("Alarma", rules.TriggerEvent, rules.Conditions{Labels: []string{"person"}}, rules.Actions{CreateAlarm: true}, true)
	ev := f.eventCtx("person")
	err := f.env.Store.TxRaw(context.Background(), store.ScopeFor(authz.Actor{TenantID: &ev.TenantID}), func(tx pgx.Tx) error {
		if err := tx.QueryRow(context.Background(), `
INSERT INTO events (tenant_id, site_id, server_id, camera_id, remote_id, severity, labels, start_time)
VALUES ($1, $2, $3, $4, $5, 'alert', $6, $7) RETURNING id`,
			ev.TenantID, ev.SiteID, ev.ServerID, ev.CameraID, "rules-"+uuid.NewString(), ev.Labels, ev.Start).Scan(&ev.ID); err != nil {
			return err
		}
		return f.svc.EvaluateEventTx(context.Background(), tx, ev)
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := f.pub.count("alarm."); n != 0 {
		t.Fatalf("directly published %d alarm announcements, want none", n)
	}
	rows := f.publications()
	if len(rows) != 1 || rows[0].subject != "alarm.opened."+ev.TenantID.String() {
		t.Fatalf("outbox publications = %+v, want one alarm.opened row", rows)
	}
	wantAggregate := "alarm/"
	var payload map[string]any
	if err := json.Unmarshal(rows[0].payload, &payload); err != nil {
		t.Fatalf("decode alarm announcement: %v", err)
	}
	wantAggregate += payload["id"].(string)
	if rows[0].aggregateKey == nil || *rows[0].aggregateKey != wantAggregate {
		t.Fatalf("alarm aggregate key = %v, want %q", rows[0].aggregateKey, wantAggregate)
	}
	if got := payload["event_id"]; got != ev.ID.String() {
		t.Fatalf("announcement event_id = %v, want bound event %s", got, ev.ID)
	}
	if got := payload["camera_id"]; got != ev.CameraID.String() {
		t.Fatalf("announcement camera_id = %v, want bound camera %s", got, ev.CameraID)
	}
}

func TestEvaluateEventOutboxPersistsAlarmLifecycle(t *testing.T) {
	f := newFixture(t)
	ruleID := f.addRule("Alarma", rules.TriggerEvent, rules.Conditions{Labels: []string{"person"}}, rules.Actions{CreateAlarm: true}, true)
	ctx := context.Background()
	run := func(at time.Time) uuid.UUID {
		t.Helper()
		ev := f.eventCtx("person")
		ev.Start = at
		err := f.env.Store.TxRaw(ctx, store.ScopeFor(authz.Actor{TenantID: &ev.TenantID}), func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `INSERT INTO events (tenant_id, site_id, server_id, camera_id, remote_id, severity, labels, start_time)
VALUES ($1, $2, $3, $4, $5, 'alert', $6, $7) RETURNING id`, ev.TenantID, ev.SiteID, ev.ServerID, ev.CameraID,
				"lifecycle-"+uuid.NewString(), ev.Labels, ev.Start).Scan(&ev.ID); err != nil {
				return err
			}
			return f.svc.EvaluateEventTx(ctx, tx, ev)
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.exec(`DELETE FROM rule_firings WHERE rule_id=$1`, ruleID); err != nil {
			t.Fatal(err)
		}
		return ev.ID
	}

	firstEvent := run(time.Now())
	run(time.Now().Add(time.Minute))
	run(time.Now().Add(3 * time.Hour)) // closes the stale alarm and opens a new correlation.
	if n := f.pub.count("alarm."); n != 0 {
		t.Fatalf("directly published %d alarm announcements, want none", n)
	}
	rows := f.publications()
	if len(rows) != 4 {
		t.Fatalf("outbox rows = %d, want opened, updated, closed, opened", len(rows))
	}
	counts := map[string]int{}
	var updated, closed int
	for _, row := range rows {
		counts[row.subject]++
		if row.aggregateKey == nil || len(*row.aggregateKey) < len("alarm/") || (*row.aggregateKey)[:len("alarm/")] != "alarm/" {
			t.Fatalf("alarm publication aggregate key = %v, want alarm/<id>", row.aggregateKey)
		}
		var payload map[string]any
		if err := json.Unmarshal(row.payload, &payload); err != nil {
			t.Fatal(err)
		}
		alarmID := payload["id"].(string)
		if row.subject == "alarm.updated."+f.env.Demo.TenantID.String() {
			updated++
		}
		if row.subject == "alarm.closed."+f.env.Demo.TenantID.String() {
			closed++
			if payload["event_id"] != firstEvent.String() {
				t.Fatalf("closed alarm event_id = %v, want originating event %s", payload["event_id"], firstEvent)
			}
		}
		if *row.aggregateKey != "alarm/"+alarmID {
			t.Fatalf("aggregate key %q does not match alarm id %s", *row.aggregateKey, alarmID)
		}
	}
	if counts["alarm.opened."+f.env.Demo.TenantID.String()] != 2 || updated != 1 || closed != 1 {
		t.Fatalf("alarm subjects = %v, want two opened, one updated, one closed", counts)
	}
}

func TestEvaluateEventDisabledRuleDoesNothing(t *testing.T) {
	f := newFixture(t)
	id := f.addRule("Apagada", rules.TriggerEvent, rules.Conditions{}, rules.Actions{NotifyInApp: true}, false)
	if err := f.svc.EvaluateEvent(context.Background(), f.eventCtx("person")); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(id); n != 0 {
		t.Fatalf("disabled rule created %d notifications, want 0", n)
	}
}

func TestEvaluateEventDebouncesPerRuleAndCamera(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.addRule("Personas", rules.TriggerEvent, rules.Conditions{}, rules.Actions{NotifyInApp: true}, true)

	for i := 0; i < 2; i++ {
		if err := f.svc.EvaluateEvent(ctx, f.eventCtx("person")); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.notifications(id); n != 1 {
		t.Fatalf("two matches inside the window created %d notifications, want 1", n)
	}

	// Another camera is not debounced by the first one.
	other := f.eventCtx("person")
	cam := f.env.Cameras["frigate-h01/plaza"]
	other.CameraID, other.CameraName = cam.ID, cam.DisplayName
	if err := f.svc.EvaluateEvent(ctx, other); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(id); n != 2 {
		t.Fatalf("other camera created %d notifications in total, want 2", n)
	}

	// Once the window has passed the rule fires again.
	f.svc.Debounce = time.Nanosecond
	time.Sleep(5 * time.Millisecond)
	if err := f.svc.EvaluateEvent(ctx, f.eventCtx("person")); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(id); n != 3 {
		t.Fatalf("match after the window created %d notifications in total, want 3", n)
	}
}

func TestOfflineDetectorNotifiesCameraOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	cam := f.env.Cameras["frigate-h01/plaza"]
	id := f.addRule("Camara caida", rules.TriggerCameraOffline,
		rules.Conditions{CameraIDs: []uuid.UUID{cam.ID}, DurationSeconds: 600}, rules.Actions{NotifyInApp: true}, true)
	det := &rules.OfflineDetector{Store: f.env.Store, Rules: f.svc, Log: pgtest.Discard()}

	if err := f.exec(`UPDATE cameras SET status = 'offline' WHERE id = $1`, cam.ID); err != nil {
		t.Fatal(err)
	}
	det.Tick(ctx) // first sight of the outage: shorter than the rule threshold
	if n := f.notifications(id); n != 0 {
		t.Fatalf("fresh outage created %d notifications, want 0", n)
	}
	if err := f.exec(`UPDATE camera_outages SET since = now() - interval '11 minutes' WHERE camera_id = $1`, cam.ID); err != nil {
		t.Fatal(err)
	}
	det.Tick(ctx)
	det.Tick(ctx)
	if n := f.notifications(id); n != 1 {
		t.Fatalf("long outage over two ticks created %d notifications, want 1", n)
	}

	// Recovery clears the outage; a new one starts from scratch.
	if err := f.exec(`UPDATE cameras SET status = 'online' WHERE id = $1`, cam.ID); err != nil {
		t.Fatal(err)
	}
	det.Tick(ctx)
	var left int
	if err := f.scan(&left, `SELECT count(*) FROM camera_outages WHERE camera_id = $1`, cam.ID); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("camera_outages keeps %d rows for a recovered camera, want 0", left)
	}
}

func TestOfflineDetectorNotifiesServerOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	srv := f.env.Cameras["frigate-h01/plaza"].ServerID
	id := f.addRule("Servidor caido", rules.TriggerServerOffline,
		rules.Conditions{ServerIDs: []uuid.UUID{srv}, DurationSeconds: 300}, rules.Actions{NotifyInApp: true}, true)
	det := &rules.OfflineDetector{Store: f.env.Store, Rules: f.svc, Log: pgtest.Discard()}

	if err := f.exec(`UPDATE frigate_servers SET status = 'offline', last_seen_at = now() - interval '2 minutes' WHERE id = $1`, srv); err != nil {
		t.Fatal(err)
	}
	det.Tick(ctx)
	if n := f.notifications(id); n != 0 {
		t.Fatalf("server offline for 2 min created %d notifications, want 0", n)
	}
	if err := f.exec(`UPDATE frigate_servers SET last_seen_at = now() - interval '10 minutes' WHERE id = $1`, srv); err != nil {
		t.Fatal(err)
	}
	det.Tick(ctx)
	det.Tick(ctx)
	if n := f.notifications(id); n != 1 {
		t.Fatalf("server offline for 10 min over two ticks created %d notifications, want 1", n)
	}
}

func TestEvaluateEventSiteFilter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ev := f.eventCtx("person")
	id := f.addRule("Otro sitio", rules.TriggerEvent, rules.Conditions{SiteIDs: []uuid.UUID{uuid.New()}}, rules.Actions{NotifyInApp: true}, true)
	same := f.addRule("Mismo sitio", rules.TriggerEvent, rules.Conditions{SiteIDs: []uuid.UUID{ev.SiteID}}, rules.Actions{NotifyInApp: true}, true)

	if err := f.svc.EvaluateEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(id); n != 0 {
		t.Fatalf("rule scoped to another site created %d notifications, want 0", n)
	}
	if n := f.notifications(same); n != 1 {
		t.Fatalf("rule scoped to the event site created %d notifications, want 1", n)
	}
}

func TestOfflineDetectorSiteFilter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	cam := f.env.Cameras["frigate-h01/plaza"]
	other := f.addRule("Otro sitio", rules.TriggerCameraOffline, rules.Conditions{SiteIDs: []uuid.UUID{uuid.New()}}, rules.Actions{NotifyInApp: true}, true)
	same := f.addRule("Mismo sitio", rules.TriggerCameraOffline, rules.Conditions{SiteIDs: []uuid.UUID{cam.SiteID}}, rules.Actions{NotifyInApp: true}, true)
	det := &rules.OfflineDetector{Store: f.env.Store, Rules: f.svc, Log: pgtest.Discard()}

	if err := f.exec(`UPDATE cameras SET status = 'offline' WHERE id = $1`, cam.ID); err != nil {
		t.Fatal(err)
	}
	det.Tick(ctx)
	if n := f.notifications(other); n != 0 {
		t.Fatalf("rule scoped to another site created %d notifications, want 0", n)
	}
	if n := f.notifications(same); n != 1 {
		t.Fatalf("rule scoped to the camera site created %d notifications, want 1", n)
	}

	srv := cam.ServerID
	srvOther := f.addRule("Servidor otro sitio", rules.TriggerServerOffline, rules.Conditions{SiteIDs: []uuid.UUID{uuid.New()}}, rules.Actions{NotifyInApp: true}, true)
	srvSame := f.addRule("Servidor mismo sitio", rules.TriggerServerOffline, rules.Conditions{SiteIDs: []uuid.UUID{cam.SiteID}}, rules.Actions{NotifyInApp: true}, true)
	if err := f.exec(`UPDATE frigate_servers SET status = 'offline' WHERE id = $1`, srv); err != nil {
		t.Fatal(err)
	}
	det.Tick(ctx)
	if n := f.notifications(srvOther); n != 0 {
		t.Fatalf("server rule scoped to another site created %d notifications, want 0", n)
	}
	if n := f.notifications(srvSame); n != 1 {
		t.Fatalf("server rule scoped to its site created %d notifications, want 1", n)
	}
}

// The outage start comes from the database, so a fresh detector (worker restart) sees the same
// outage and must not notify again; a later outage is a new one.
func TestOfflineDetectorRestartDoesNotRefire(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	cam := f.env.Cameras["frigate-h01/plaza"]
	id := f.addRule("Camara caida", rules.TriggerCameraOffline,
		rules.Conditions{CameraIDs: []uuid.UUID{cam.ID}}, rules.Actions{NotifyInApp: true}, true)
	newDetector := func() *rules.OfflineDetector {
		return &rules.OfflineDetector{Store: f.env.Store, Rules: f.svc, Log: pgtest.Discard()}
	}
	outageSince := func() time.Time {
		var since time.Time
		if err := f.scan(&since, `SELECT since FROM camera_outages WHERE camera_id = $1`, cam.ID); err != nil {
			t.Fatal(err)
		}
		return since
	}

	if err := f.exec(`UPDATE cameras SET status = 'offline' WHERE id = $1`, cam.ID); err != nil {
		t.Fatal(err)
	}
	newDetector().Tick(ctx)
	first := outageSince()
	if n := f.notifications(id); n != 1 {
		t.Fatalf("first tick created %d notifications, want 1", n)
	}

	// Worker restarts: new detector instances, several ticks with delays in between.
	for i := 0; i < 3; i++ {
		time.Sleep(20 * time.Millisecond)
		newDetector().Tick(ctx)
	}
	if got := outageSince(); !got.Equal(first) {
		t.Fatalf("outage start moved from %v to %v across restarts", first, got)
	}
	if n := f.notifications(id); n != 1 {
		t.Fatalf("ticks after a restart created %d notifications in total, want 1", n)
	}

	// Recovery and a later outage notify again.
	if err := f.exec(`UPDATE cameras SET status = 'online' WHERE id = $1`, cam.ID); err != nil {
		t.Fatal(err)
	}
	newDetector().Tick(ctx)
	if err := f.exec(`UPDATE rule_firings SET fired_at = now() - interval '30 minutes' WHERE rule_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := f.exec(`UPDATE cameras SET status = 'offline' WHERE id = $1`, cam.ID); err != nil {
		t.Fatal(err)
	}
	newDetector().Tick(ctx)
	newDetector().Tick(ctx)
	if n := f.notifications(id); n != 2 {
		t.Fatalf("second outage created %d notifications in total, want 2", n)
	}
}

// EvaluateOffline compares the rule's last firing with the outage start it is given, never with
// a cutoff derived from the caller's clock.
func TestEvaluateOfflineClaimsAgainstOutageStart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	cam := f.env.Cameras["frigate-h01/plaza"]
	id := f.addRule("Camara caida", rules.TriggerCameraOffline, rules.Conditions{}, rules.Actions{NotifyInApp: true}, true)
	since := time.Now().Add(-time.Minute)
	o := rules.Outage{TenantID: cam.TenantID, ResourceID: cam.ID, SiteID: cam.SiteID, Name: cam.DisplayName, Since: since, Duration: time.Minute}

	for i := 0; i < 3; i++ {
		// Whatever duration the caller computed, the same outage start never fires twice.
		o.Duration = time.Duration(i) * time.Hour
		if err := f.svc.EvaluateOffline(ctx, rules.TriggerCameraOffline, o); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.notifications(id); n != 1 {
		t.Fatalf("same outage created %d notifications, want 1", n)
	}

	o.Since = time.Now().Add(time.Second) // a later outage
	if err := f.svc.EvaluateOffline(ctx, rules.TriggerCameraOffline, o); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(id); n != 2 {
		t.Fatalf("later outage created %d notifications in total, want 2", n)
	}
}

type deliveryInfo struct {
	Channel     uuid.UUID
	Destination string
	Status      string
	HasNotif    bool
	Title       string
}

func (f *fixture) addChannel(typ, config string, enabled bool) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(),
			`INSERT INTO notification_channels (id, tenant_id, name, type, config, enabled) VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
			id, f.env.Demo.TenantID, typ+"-"+id.String(), typ, config, enabled)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fixture) deliveries(ruleID uuid.UUID) []deliveryInfo {
	f.t.Helper()
	var out []deliveryInfo
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(),
			`SELECT channel_id, destination, status, notification_id IS NOT NULL, payload->>'title'
			   FROM notification_deliveries WHERE rule_id = $1 ORDER BY channel_name, destination`, ruleID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d deliveryInfo
			if err := rows.Scan(&d.Channel, &d.Destination, &d.Status, &d.HasNotif, &d.Title); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func TestEvaluateEventEnqueuesDeliveriesForSelectedChannels(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	hook := f.addChannel("webhook", `{"url":"https://example.com/h"}`, true)
	tg := f.addChannel("telegram", `{"chat_ids":["1","2"]}`, true)
	disabled := f.addChannel("webhook", `{"url":"https://example.com/off"}`, false)
	f.addChannel("webhook", `{"url":"https://example.com/unselected"}`, true)
	id := f.addRule("Personas", rules.TriggerEvent, rules.Conditions{}, rules.Actions{NotifyInApp: true, ChannelIDs: []uuid.UUID{hook, tg, disabled}}, true)

	if err := f.svc.EvaluateEvent(ctx, f.eventCtx("person")); err != nil {
		t.Fatal(err)
	}
	got := f.deliveries(id)
	if len(got) != 3 {
		t.Fatalf("deliveries = %+v, want 3 (1 webhook + 2 telegram chats; disabled and unselected channels skipped)", got)
	}
	for _, d := range got {
		if d.Status != "pending" || !d.HasNotif || d.Title != "Personas" || d.Channel == disabled {
			t.Errorf("unexpected delivery %+v", d)
		}
	}

	// Debounced firing does not enqueue again.
	if err := f.svc.EvaluateEvent(ctx, f.eventCtx("person")); err != nil {
		t.Fatal(err)
	}
	if n := len(f.deliveries(id)); n != 3 {
		t.Fatalf("debounced event enqueued more deliveries: %d", n)
	}
}

func TestEvaluateEventChannelsWorkWithoutInAppNotification(t *testing.T) {
	f := newFixture(t)
	hook := f.addChannel("webhook", `{"url":"https://example.com/h"}`, true)
	id := f.addRule("Solo canal", rules.TriggerEvent, rules.Conditions{}, rules.Actions{ChannelIDs: []uuid.UUID{hook}}, true)
	if err := f.svc.EvaluateEvent(context.Background(), f.eventCtx("person")); err != nil {
		t.Fatal(err)
	}
	got := f.deliveries(id)
	if len(got) != 1 || got[0].HasNotif {
		t.Fatalf("deliveries = %+v, want one delivery without an in-app notification", got)
	}
	if n := f.notifications(id); n != 0 {
		t.Fatalf("in-app notifications = %d, want 0", n)
	}
}

func TestEvaluateOfflineEnqueuesDeliveries(t *testing.T) {
	f := newFixture(t)
	hook := f.addChannel("webhook", `{"url":"https://example.com/h"}`, true)
	cam := f.env.Cameras["frigate-h01/plaza"]
	id := f.addRule("Camara caida", rules.TriggerCameraOffline, rules.Conditions{}, rules.Actions{NotifyInApp: true, ChannelIDs: []uuid.UUID{hook}}, true)
	o := rules.Outage{TenantID: cam.TenantID, ResourceID: cam.ID, SiteID: cam.SiteID, Name: cam.DisplayName, Since: time.Now().Add(-time.Hour), Duration: time.Hour}
	for i := 0; i < 2; i++ {
		if err := f.svc.EvaluateOffline(context.Background(), rules.TriggerCameraOffline, o); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.deliveries(id); len(got) != 1 {
		t.Fatalf("deliveries = %+v, want exactly one for the outage", got)
	}
}

func TestEvaluateEventCameraGroupScheduleAndCooldown(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	camNorte := f.env.Cameras["frigate-h01/acceso_norte"]
	camPlaza := f.env.Cameras["frigate-h01/plaza"]

	// Create a camera group in DB and assign camNorte (not camPlaza) to it.
	groupID := uuid.New()
	if err := f.exec(
		`INSERT INTO camera_groups (id, tenant_id, name, description) VALUES ($1, $2, 'Perimetro Norte', '')`,
		groupID, f.env.Demo.TenantID,
	); err != nil {
		t.Fatal(err)
	}
	if err := f.exec(
		`INSERT INTO camera_group_members (group_id, camera_id, tenant_id) VALUES ($1, $2, $3)`,
		groupID, camNorte.ID, f.env.Demo.TenantID,
	); err != nil {
		t.Fatal(err)
	}

	// Set the site's timezone in DB to America/New_York so schedule evaluation uses real DB site timezone.
	if err := f.exec(`UPDATE sites SET timezone = 'America/New_York' WHERE id = $1`, camNorte.SiteID); err != nil {
		t.Fatal(err)
	}

	cooldownShort := 1
	cooldownLong := 600
	ruleShort := f.addRule("Grupo Nocturno Corto", rules.TriggerEvent, rules.Conditions{
		CameraGroupIDs: []uuid.UUID{groupID},
		Schedule: &rules.RuleSchedule{
			DaysOfWeek: []int{5}, // Friday night shift (Fri 22:00 -> Sat 06:00 America/New_York)
			StartTime:  "22:00",
			EndTime:    "06:00",
		},
		CooldownSeconds: &cooldownShort,
	}, rules.Actions{NotifyInApp: true}, true)

	ruleLong := f.addRule("Grupo Nocturno Largo", rules.TriggerEvent, rules.Conditions{
		CameraGroupIDs:  []uuid.UUID{groupID},
		CooldownSeconds: &cooldownLong,
	}, rules.Actions{NotifyInApp: true}, true)

	// 1) Event on camPlaza (not in groupID) inside schedule window -> must not match either rule.
	evNight := f.eventCtx("person")
	// 2026-10-17T06:30:00Z is Saturday 2026-10-17 02:30 EDT (part of Friday 22:00 -> Saturday 06:00 shift).
	evNight.Start = time.Date(2026, 10, 17, 6, 30, 0, 0, time.UTC)
	evPlaza := evNight
	evPlaza.CameraID, evPlaza.CameraName = camPlaza.ID, camPlaza.DisplayName
	if err := f.svc.EvaluateEvent(ctx, evPlaza); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(ruleShort); n != 0 {
		t.Fatalf("camera outside group created %d notifications for ruleShort, want 0", n)
	}

	// 2) Event on camNorte (in groupID) outside schedule window (Friday 14:00 EDT = 18:00Z) -> ruleShort skipped, ruleLong fires.
	evDay := evNight
	evDay.Start = time.Date(2026, 10, 16, 18, 0, 0, 0, time.UTC)
	if err := f.svc.EvaluateEvent(ctx, evDay); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(ruleShort); n != 0 {
		t.Fatalf("event outside schedule created %d notifications for ruleShort, want 0", n)
	}
	if n := f.notifications(ruleLong); n != 1 {
		t.Fatalf("matching event created %d notifications for ruleLong, want 1", n)
	}

	// 3) Event on camNorte inside overnight schedule window -> ruleShort fires (1), ruleLong is debounced (still 1).
	if err := f.svc.EvaluateEvent(ctx, evNight); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(ruleShort); n != 1 {
		t.Fatalf("matching overnight event created %d notifications for ruleShort, want 1", n)
	}
	if n := f.notifications(ruleLong); n != 1 {
		t.Fatalf("debounced ruleLong created %d notifications, want 1", n)
	}

	// Immediate second event within 1s cooldown -> ruleShort is debounced (still 1).
	if err := f.svc.EvaluateEvent(ctx, evNight); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(ruleShort); n != 1 {
		t.Fatalf("immediate repeat created %d notifications for ruleShort, want 1", n)
	}

	// Backdate ruleShort's firing by 2 seconds (> 1s cooldown, < 600s cooldown) -> ruleShort fires again (2), ruleLong stays debounced (1).
	if err := f.exec(`UPDATE rule_firings SET fired_at = now() - interval '2 seconds' WHERE rule_id = $1`, ruleShort); err != nil {
		t.Fatal(err)
	}
	if err := f.exec(`UPDATE rule_firings SET fired_at = now() - interval '2 seconds' WHERE rule_id = $1`, ruleLong); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.EvaluateEvent(ctx, evNight); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(ruleShort); n != 2 {
		t.Fatalf("after 1s cooldown expiry ruleShort notifications = %d, want 2", n)
	}
	if n := f.notifications(ruleLong); n != 1 {
		t.Fatalf("inside 600s cooldown ruleLong notifications = %d, want 1", n)
	}
}

func TestEvaluateEventMapZoneResolver(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	camInside := f.env.Cameras["frigate-h01/acceso_norte"]
	camOutside := f.env.Cameras["frigate-h01/plaza"]

	// Create a map zone polygon covering lng [-64.20, -64.15], lat [-31.45, -31.40].
	zoneID := uuid.New()
	poly := `{"type":"Polygon","coordinates":[[[-64.20,-31.45],[-64.15,-31.45],[-64.15,-31.40],[-64.20,-31.40],[-64.20,-31.45]]]}`
	if err := f.exec(`
INSERT INTO map_zones (id, tenant_id, site_id, name, kind, geometry, min_lat, min_lng, max_lat, max_lng)
VALUES ($1, $2, $3, 'Zona Restringida Norte', 'security', $4::jsonb, -31.45, -64.20, -31.40, -64.15)`,
		zoneID, camInside.TenantID, camInside.SiteID, poly,
	); err != nil {
		t.Fatal(err)
	}

	// Place camInside inside the polygon (-31.42, -64.18) and camOutside outside (-31.55, -64.30).
	if err := f.exec(`
INSERT INTO map_placements (tenant_id, site_id, entity_type, entity_id, lat, lng)
VALUES
  ($1, $2, 'camera', $3, -31.42, -64.18),
  ($1, $2, 'camera', $4, -31.55, -64.30)
ON CONFLICT (entity_type, entity_id) WHERE floor_id IS NULL
DO UPDATE SET lat = EXCLUDED.lat, lng = EXCLUDED.lng`,
		camInside.TenantID, camInside.SiteID, camInside.ID, camOutside.ID,
	); err != nil {
		t.Fatal(err)
	}

	ruleID := f.addRule("Regla Zona Mapa", rules.TriggerEvent, rules.Conditions{
		MapZoneIDs: []uuid.UUID{zoneID},
	}, rules.Actions{NotifyInApp: true}, true)

	// Event from camera outside map zone -> must not match.
	evOut := f.eventCtx("person")
	evOut.CameraID, evOut.CameraName = camOutside.ID, camOutside.DisplayName
	if err := f.svc.EvaluateEvent(ctx, evOut); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(ruleID); n != 0 {
		t.Fatalf("camera outside map zone created %d notifications, want 0", n)
	}

	// Event from camera inside map zone -> resolved dynamically via StoreZoneResolver and matches.
	evIn := f.eventCtx("person")
	evIn.CameraID, evIn.CameraName = camInside.ID, camInside.DisplayName
	if err := f.svc.EvaluateEvent(ctx, evIn); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(ruleID); n != 1 {
		t.Fatalf("camera inside map zone created %d notifications, want 1", n)
	}
}
