//go:build integration

package alarms_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

var t0 = time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

type corrEnv struct {
	t   *testing.T
	env *demofix.Env
	cam db.GetCameraRow
}

func newCorrEnv(t *testing.T) *corrEnv {
	t.Helper()
	env := demofix.Setup(t)
	return &corrEnv{t: t, env: env, cam: env.Cameras["frigate-h01/plaza"]}
}

// seedEvent inserts a bare alert event on cam starting at start.
func (c *corrEnv) seedEvent(cam db.GetCameraRow, start time.Time, labels ...string) uuid.UUID {
	c.t.Helper()
	var id uuid.UUID
	err := c.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `
INSERT INTO events (tenant_id, site_id, server_id, camera_id, remote_id, severity, labels, start_time)
VALUES ($1, $2, $3, $4, $5, 'alert', $6, $7) RETURNING id`,
			cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, "corr-"+uuid.NewString(), labels, start).Scan(&id)
	})
	if err != nil {
		c.t.Fatal(err)
	}
	return id
}

// correlate runs Correlate in its own transaction for an event of the given source.
func (c *corrEnv) correlate(cam db.GetCameraRow, source string, rule *uuid.UUID, at time.Time, labels ...string) alarms.CorrelateResult {
	c.t.Helper()
	ev := c.seedEvent(cam, at, labels...)
	var res alarms.CorrelateResult
	err := c.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		var err error
		res, err = alarms.Correlate(context.Background(), tx, alarms.CorrelateInput{
			TenantID: cam.TenantID, SiteID: cam.SiteID, CameraID: cam.ID, EventID: ev,
			Source: source, RuleID: rule, Labels: labels, At: at,
		})
		return err
	})
	if err != nil {
		c.t.Fatal(err)
	}
	return res
}

func (c *corrEnv) count(query string, args ...any) int {
	c.t.Helper()
	var n int
	err := c.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), query, args...).Scan(&n)
	})
	if err != nil {
		c.t.Fatal(err)
	}
	return n
}

func (c *corrEnv) setStatus(id uuid.UUID, status string) {
	c.t.Helper()
	err := c.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE alarms SET status = $2 WHERE id = $1`, id, status)
		return err
	})
	if err != nil {
		c.t.Fatal(err)
	}
}

func TestCorrelateSameKeyWithinWindowMergesIntoOneAlarm(t *testing.T) {
	c := newCorrEnv(t)
	const n = 5
	var first alarms.CorrelateResult
	for i := 0; i < n; i++ {
		res := c.correlate(c.cam, "event", nil, t0.Add(time.Duration(i)*5*time.Minute), "person")
		if i == 0 {
			first = res
			if res.Outcome != alarms.Created {
				t.Fatalf("first event outcome = %v, want Created", res.Outcome)
			}
			continue
		}
		if res.Outcome != alarms.Merged || res.AlarmID != first.AlarmID {
			t.Fatalf("event %d: outcome=%v alarm=%s, want Merged into %s", i, res.Outcome, res.AlarmID, first.AlarmID)
		}
	}
	if got := c.count(`SELECT count(*) FROM alarms WHERE camera_id = $1`, c.cam.ID); got != 1 {
		t.Fatalf("alarms = %d, want 1", got)
	}
	if got := c.count(`SELECT event_count FROM alarms WHERE id = $1`, first.AlarmID); got != n {
		t.Fatalf("event_count = %d, want %d", got, n)
	}
	if got := c.count(`SELECT count(*) FROM alarm_events WHERE alarm_id = $1`, first.AlarmID); got != n {
		t.Fatalf("alarm_events = %d, want %d", got, n)
	}
	if got := c.count(`SELECT count(*) FROM alarms WHERE id = $1 AND first_event_at = $2 AND last_event_at = $3`,
		first.AlarmID, t0, t0.Add((n-1)*5*time.Minute)); got != 1 {
		t.Fatal("first_event_at / last_event_at not maintained")
	}
}

func TestCorrelateIsIdempotentPerEvent(t *testing.T) {
	c := newCorrEnv(t)
	ev := c.seedEvent(c.cam, t0, "person")
	in := alarms.CorrelateInput{TenantID: c.cam.TenantID, SiteID: c.cam.SiteID, CameraID: c.cam.ID, EventID: ev,
		Source: "event", Labels: []string{"person"}, At: t0}
	run := func() alarms.CorrelateResult {
		var res alarms.CorrelateResult
		if err := c.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
			var err error
			res, err = alarms.Correlate(context.Background(), tx, in)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return res
	}
	if r := run(); r.Outcome != alarms.Created {
		t.Fatalf("first = %v, want Created", r.Outcome)
	}
	if r := run(); r.Outcome != alarms.Skipped {
		t.Fatalf("replay = %v, want Skipped", r.Outcome)
	}
	if got := c.count(`SELECT event_count FROM alarms WHERE camera_id = $1`, c.cam.ID); got != 1 {
		t.Fatalf("event_count after replay = %d, want 1", got)
	}
}

func TestCorrelateDifferentCameraOrLabelOpensSeparateAlarms(t *testing.T) {
	c := newCorrEnv(t)
	other := c.env.Cameras["frigate-h01/acceso_norte"]
	a := c.correlate(c.cam, "event", nil, t0, "person")
	b := c.correlate(c.cam, "event", nil, t0.Add(time.Minute), "car")
	d := c.correlate(other, "event", nil, t0.Add(2*time.Minute), "person")
	if a.AlarmID == b.AlarmID || a.AlarmID == d.AlarmID || b.AlarmID == d.AlarmID {
		t.Fatalf("alarms merged across label/camera: %s %s %s", a.AlarmID, b.AlarmID, d.AlarmID)
	}
	for _, r := range []alarms.CorrelateResult{a, b, d} {
		if r.Outcome != alarms.Created {
			t.Fatalf("outcome = %v, want Created", r.Outcome)
		}
	}
}

func TestCorrelateAfterClosedAlarmOpensNewOne(t *testing.T) {
	c := newCorrEnv(t)
	a := c.correlate(c.cam, "event", nil, t0, "person")
	c.setStatus(a.AlarmID, "closed")
	b := c.correlate(c.cam, "event", nil, t0.Add(time.Minute), "person")
	if b.Outcome != alarms.Created || b.AlarmID == a.AlarmID {
		t.Fatalf("event after close: outcome=%v alarm=%s, want a new alarm", b.Outcome, b.AlarmID)
	}
}

func TestCorrelateAfterWindowOpensNewAlarmAndClosesStaleOne(t *testing.T) {
	c := newCorrEnv(t)
	a := c.correlate(c.cam, "event", nil, t0, "person")
	b := c.correlate(c.cam, "event", nil, t0.Add(31*time.Minute), "person")
	if b.Outcome != alarms.Created || b.AlarmID == a.AlarmID {
		t.Fatalf("event after window: outcome=%v alarm=%s, want a new alarm", b.Outcome, b.AlarmID)
	}
	if got := c.count(`SELECT count(*) FROM alarms WHERE id = $1 AND status = 'closed' AND auto_closed`, a.AlarmID); got != 1 {
		t.Fatal("the stale alarm must be auto-closed so the key is free")
	}
	if got := c.count(`SELECT count(*) FROM alarm_transitions WHERE alarm_id = $1 AND to_status = 'closed' AND actor_id IS NULL`, a.AlarmID); got != 1 {
		t.Fatal("stale close must record a system transition")
	}
}

func TestCorrelateHeldAlarmKeepsAbsorbingPastWindow(t *testing.T) {
	c := newCorrEnv(t)
	a := c.correlate(c.cam, "event", nil, t0, "person")
	c.setStatus(a.AlarmID, "investigating")
	b := c.correlate(c.cam, "event", nil, t0.Add(3*time.Hour), "person")
	if b.Outcome != alarms.Merged || b.AlarmID != a.AlarmID {
		t.Fatalf("investigating alarm: outcome=%v alarm=%s, want Merged into %s", b.Outcome, b.AlarmID, a.AlarmID)
	}
}

func TestCorrelateRuleAndEventSourcesNeverMerge(t *testing.T) {
	c := newCorrEnv(t)
	rule := uuid.New()
	// Rules reference rules(id); the key only embeds the id, so a bare uuid is enough here.
	e := c.correlate(c.cam, "event", nil, t0, "person")
	r := c.correlate(c.cam, "rule", &rule, t0.Add(time.Minute), "person")
	if e.AlarmID == r.AlarmID || r.Outcome != alarms.Created {
		t.Fatalf("rule alarm merged into event alarm (event=%s rule=%s outcome=%v)", e.AlarmID, r.AlarmID, r.Outcome)
	}
	r2 := c.correlate(c.cam, "rule", &rule, t0.Add(2*time.Minute), "person")
	if r2.Outcome != alarms.Merged || r2.AlarmID != r.AlarmID {
		t.Fatalf("second rule event: outcome=%v alarm=%s, want Merged into %s", r2.Outcome, r2.AlarmID, r.AlarmID)
	}
}

func TestCorrelateConcurrentEventsYieldOneAlarm(t *testing.T) {
	c := newCorrEnv(t)
	const n = 8
	evs := make([]uuid.UUID, n)
	for i := range evs {
		evs[i] = c.seedEvent(c.cam, t0.Add(time.Duration(i)*time.Second), "person")
	}
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			errs <- c.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
				_, err := alarms.Correlate(context.Background(), tx, alarms.CorrelateInput{
					TenantID: c.cam.TenantID, SiteID: c.cam.SiteID, CameraID: c.cam.ID, EventID: evs[i],
					Source: "event", Labels: []string{"person"}, At: t0.Add(time.Duration(i) * time.Second),
				})
				return err
			})
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got := c.count(`SELECT count(*) FROM alarms WHERE camera_id = $1`, c.cam.ID); got != 1 {
		t.Fatalf("alarms = %d, want 1", got)
	}
	if got := c.count(`SELECT event_count FROM alarms WHERE camera_id = $1`, c.cam.ID); got != n {
		t.Fatalf("event_count = %d, want %d", got, n)
	}
}

func TestAlarmEventsAreTenantIsolated(t *testing.T) {
	c := newCorrEnv(t)
	ctx := context.Background()
	a := c.correlate(c.cam, "event", nil, t0, "person")
	tenantB, err := c.env.Svc.CreateTenant(ctx, c.env.Admin, "alarm-events-b", "Alarm Events B")
	if err != nil {
		t.Fatal(err)
	}
	scopeB := store.TenantScope{TenantID: tenantB.ID}
	var seen int
	if err := c.env.Store.TxRaw(ctx, scopeB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM alarm_events`).Scan(&seen)
	}); err != nil {
		t.Fatal(err)
	}
	if seen != 0 {
		t.Fatalf("tenant B sees %d alarm_events of another tenant", seen)
	}
	err = c.env.Store.TxRaw(ctx, scopeB, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO alarm_events (alarm_id, event_id, tenant_id, source) VALUES ($1, $2, $3, 'event')`,
			a.AlarmID, uuid.New(), c.cam.TenantID)
		return err
	})
	if err == nil {
		t.Fatal("tenant B inserted an alarm_events row for another tenant")
	}
}

func TestCorrelateMarksAlarmLateWhenEventOlderThanAutoCloseWindow(t *testing.T) {
	c := newCorrEnv(t)
	ctx := context.Background()
	now := t0.Add(2 * time.Hour)

	// Fresh event within the 30m auto-close window: late = false.
	freshEv := c.seedEvent(c.cam, now.Add(-5*time.Minute), "person")
	var freshRes alarms.CorrelateResult
	if err := c.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		var err error
		freshRes, err = alarms.Correlate(ctx, tx, alarms.CorrelateInput{
			TenantID: c.cam.TenantID, SiteID: c.cam.SiteID, CameraID: c.cam.ID, EventID: freshEv,
			Source: "event", Labels: []string{"person"}, At: now.Add(-5 * time.Minute), Now: now,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if freshRes.Late {
		t.Fatalf("fresh alarm Late = true, want false")
	}

	// Old event past the 30m auto-close window at ingestion: late = true.
	lateEv := c.seedEvent(c.cam, now.Add(-45*time.Minute), "car")
	var lateRes alarms.CorrelateResult
	if err := c.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		var err error
		lateRes, err = alarms.Correlate(ctx, tx, alarms.CorrelateInput{
			TenantID: c.cam.TenantID, SiteID: c.cam.SiteID, CameraID: c.cam.ID, EventID: lateEv,
			Source: "event", Labels: []string{"car"}, At: now.Add(-45 * time.Minute), Now: now,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !lateRes.Late {
		t.Fatalf("late alarm Late = false, want true")
	}

	alarmSvc := &alarms.Service{Store: c.env.Store}
	gotFresh, err := alarmSvc.Get(ctx, c.env.Admin, freshRes.AlarmID)
	if err != nil {
		t.Fatal(err)
	}
	if gotFresh.Late {
		t.Fatalf("Get(fresh).Late = true, want false")
	}
	gotLate, err := alarmSvc.Get(ctx, c.env.Admin, lateRes.AlarmID)
	if err != nil {
		t.Fatal(err)
	}
	if !gotLate.Late {
		t.Fatalf("Get(late).Late = false, want true")
	}
}

