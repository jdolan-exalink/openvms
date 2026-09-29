//go:build integration

package rules_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	if n := f.pub.count("notification.created."); n != 1 {
		t.Fatalf("published %d notification.created messages, want 1", n)
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
