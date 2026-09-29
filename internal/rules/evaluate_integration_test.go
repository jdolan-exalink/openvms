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
