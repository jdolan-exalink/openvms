//go:build integration

package alarms_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
	"github.com/jdolan-exalink/openvms/migrations"
)

type recordingPub struct {
	mu       sync.Mutex
	subjects []string
}

func (p *recordingPub) Publish(_ context.Context, subject string, _ []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subjects = append(p.subjects, subject)
	return nil
}

func (c *corrEnv) closer(now time.Time, batch int32) (*alarms.AutoCloser, *recordingPub) {
	pub := &recordingPub{}
	return &alarms.AutoCloser{Store: c.env.Store, Pub: pub, Log: pgtest.Discard(), Batch: batch, Now: func() time.Time { return now }}, pub
}

func TestAutoCloseClosesOnlyAfterTheWindow(t *testing.T) {
	c := newCorrEnv(t)
	ctx := context.Background()
	a := c.correlate(c.cam, "event", nil, t0, "person")

	before, pub := c.closer(t0.Add(29*time.Minute), 100)
	if n, err := before.Sweep(ctx); err != nil || n != 0 {
		t.Fatalf("sweep inside the window = %d, %v; want 0, nil", n, err)
	}
	if got := c.count(`SELECT count(*) FROM alarms WHERE id = $1 AND status = 'open'`, a.AlarmID); got != 1 {
		t.Fatal("alarm closed before the window elapsed")
	}

	now := t0.Add(31 * time.Minute)
	after, pub := c.closer(now, 100)
	if n, err := after.Sweep(ctx); err != nil || n != 1 {
		t.Fatalf("sweep after the window = %d, %v; want 1, nil", n, err)
	}
	if got := c.count(`SELECT count(*) FROM alarms WHERE id = $1 AND status = 'closed' AND auto_closed AND closed_by IS NULL AND closed_at = $2`, a.AlarmID, now); got != 1 {
		t.Fatal("alarm not closed with auto_closed=true, system actor and closed_at=now")
	}
	if got := c.count(`SELECT count(*) FROM alarm_transitions WHERE alarm_id = $1 AND from_status = 'open' AND to_status = 'closed' AND actor_id IS NULL AND comment = 'auto-closed after 30 minutes without activity'`, a.AlarmID); got != 1 {
		t.Fatal("system transition with the auto-close comment not recorded")
	}
	if got := c.count(`SELECT count(*) FROM audit_log WHERE action = 'ALARM_CLOSED' AND target_id = $1 AND actor_id IS NULL AND actor_name = 'system'`, a.AlarmID); got != 1 {
		t.Fatal("system audit entry not recorded")
	}
	if len(pub.subjects) != 1 || pub.subjects[0] != "alarm.closed."+c.cam.TenantID.String() {
		t.Fatalf("published %v, want one alarm.closed.<tenant>", pub.subjects)
	}
	if n, _ := after.Sweep(ctx); n != 0 {
		t.Fatalf("second sweep closed %d, want 0 (idempotent)", n)
	}
}

func TestAutoCloseHonorsConfiguredWindow(t *testing.T) {
	c := newCorrEnv(t)
	ctx := context.Background()
	a := c.correlate(c.cam, "event", nil, t0, "person")
	if err := c.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE alarm_settings SET auto_close_after = interval '10 minutes'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s, _ := c.closer(t0.Add(11*time.Minute), 100)
	if n, err := s.Sweep(ctx); err != nil || n != 1 {
		t.Fatalf("sweep = %d, %v; want 1 with a 10 minute window", n, err)
	}
	if got := c.count(`SELECT count(*) FROM alarm_transitions WHERE alarm_id = $1 AND comment = 'auto-closed after 10 minutes without activity'`, a.AlarmID); got != 1 {
		t.Fatal("comment must state the configured window")
	}
}

func TestAutoCloseLeavesAlarmsHeldByPeople(t *testing.T) {
	c := newCorrEnv(t)
	ctx := context.Background()
	other := c.env.Cameras["frigate-h01/acceso_norte"]
	ack := c.correlate(c.cam, "event", nil, t0, "person")
	c.setStatus(ack.AlarmID, "acknowledged")
	assigned := c.correlate(c.cam, "event", nil, t0, "car")
	c.setStatus(assigned.AlarmID, "assigned")
	inv := c.correlate(other, "event", nil, t0, "person")
	c.setStatus(inv.AlarmID, "investigating")

	s, _ := c.closer(t0.Add(time.Hour), 100)
	if n, err := s.Sweep(ctx); err != nil || n != 1 {
		t.Fatalf("sweep = %d, %v; want only the acknowledged alarm closed", n, err)
	}
	for id, want := range map[uuid.UUID]string{ack.AlarmID: "closed", assigned.AlarmID: "assigned", inv.AlarmID: "investigating"} {
		if got := c.count(`SELECT count(*) FROM alarms WHERE id = $1 AND status = $2`, id, want); got != 1 {
			t.Fatalf("alarm %s is not %s", id, want)
		}
	}
}

func TestAutoCloseRespectsBatchLimit(t *testing.T) {
	c := newCorrEnv(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		c.correlate(c.cam, "event", nil, t0, "label-"+strings.Repeat("x", i+1))
	}
	s, _ := c.closer(t0.Add(time.Hour), 2)
	for _, want := range []int{2, 2, 1, 0} {
		if n, err := s.Sweep(ctx); err != nil || n != want {
			t.Fatalf("sweep = %d, %v; want %d", n, err, want)
		}
	}
}

func TestBacklogCleanupMigrationClosesOpenAlarmsOlderThan24h(t *testing.T) {
	c := newCorrEnv(t)
	ctx := context.Background()
	now := time.Now().UTC()
	other := c.env.Cameras["frigate-h01/acceso_norte"]

	staleOpen1 := c.correlate(c.cam, "event", nil, now.Add(-48*time.Hour), "person")
	staleOpen2 := c.correlate(c.cam, "event", nil, now.Add(-25*time.Hour), "car")
	recentOpen := c.correlate(c.cam, "event", nil, now.Add(-23*time.Hour), "dog")
	staleAck := c.correlate(other, "event", nil, now.Add(-36*time.Hour), "person")
	c.setStatus(staleAck.AlarmID, "acknowledged")

	raw, err := migrations.FS.ReadFile("00049_alarms_backlog_cleanup.sql")
	if err != nil {
		t.Fatalf("read migration 00049_alarms_backlog_cleanup.sql: %v", err)
	}
	upSQL := string(raw)
	if idx := strings.Index(upSQL, "-- +goose Down"); idx >= 0 {
		upSQL = upSQL[:idx]
	}

	if err := c.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, upSQL)
		return err
	}); err != nil {
		t.Fatalf("execute backlog cleanup migration: %v", err)
	}

	for _, id := range []uuid.UUID{staleOpen1.AlarmID, staleOpen2.AlarmID} {
		if got := c.count(`SELECT count(*) FROM alarms WHERE id = $1 AND status = 'closed' AND auto_closed AND closed_at IS NOT NULL`, id); got != 1 {
			t.Fatalf("stale open alarm %s was not auto-closed by migration", id)
		}
		if got := c.count(`SELECT count(*) FROM alarm_transitions WHERE alarm_id = $1 AND from_status = 'open' AND to_status = 'closed' AND actor_id IS NULL AND comment = 'Auto-closed by 24h backlog cleanup migration'`, id); got != 1 {
			t.Fatalf("stale open alarm %s missing system transition row", id)
		}
		if got := c.count(`SELECT count(*) FROM audit_log WHERE action = 'ALARM_CLOSED' AND target_id = $1 AND actor_name = 'system'`, id); got != 1 {
			t.Fatalf("stale open alarm %s missing system audit entry", id)
		}
	}
	if got := c.count(`SELECT count(*) FROM alarms WHERE id = $1 AND status = 'open' AND NOT auto_closed`, recentOpen.AlarmID); got != 1 {
		t.Fatal("recent open alarm (<24h) must remain open")
	}
	if got := c.count(`SELECT count(*) FROM alarms WHERE id = $1 AND status = 'acknowledged' AND NOT auto_closed`, staleAck.AlarmID); got != 1 {
		t.Fatal("non-open alarm must remain untouched by backlog cleanup migration")
	}
}
