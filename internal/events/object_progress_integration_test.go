//go:build integration

package events_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/frigatemock"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// seedPlateBacklog puts n single-detection reviews on the LPR camera, spread over the window
// [now-5h, now-2h], so they sit before the mock's own seeded reviews (the last hour). Every one
// carries a distinct recognized plate. It returns the start of the backlog.
func seedPlateBacklog(mock *frigatemock.Store, camera string, n int) time.Time {
	first := time.Now().Add(-5 * time.Hour)
	for i := 0; i < n; i++ {
		start := float64(first.Add(time.Duration(i)*20*time.Second).UnixMicro()) / 1e6
		end := start + 5
		id := fmt.Sprintf("backlog-%04d", i)
		mock.Put(frigatemock.Review{
			ID: id, Camera: camera, StartTime: start, EndTime: &end, Severity: "detection",
			Data: frigatemock.ReviewData{Detections: []string{id}, Objects: []string{"car"}, SubLabels: []string{fmt.Sprintf("BK%04dZZ", i)}, Zones: []string{}, Audio: []string{}},
		})
	}
	return first
}

func serverByName(t *testing.T, ctx context.Context, s *store.Store, name string) db.FrigateServer {
	t.Helper()
	var out db.FrigateServer
	err := s.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		all, err := q.ListAllActiveServers(ctx)
		if err != nil {
			return err
		}
		for _, srv := range all {
			if srv.Name == name {
				out = srv
				return nil
			}
		}
		return fmt.Errorf("server %q not found", name)
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func objectCursor(t *testing.T, ctx context.Context, s *store.Store, serverID any) *time.Time {
	t.Helper()
	var c *time.Time
	err := s.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT object_cursor FROM event_sync_state WHERE server_id = $1`, serverID).Scan(&c)
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func countBacklogReads(t *testing.T, ctx context.Context, s *store.Store, serverID any) int {
	t.Helper()
	var n int
	err := s.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM lpr_reads WHERE server_id = $1 AND remote_event_id LIKE 'backlog-%'`, serverID).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestObjectSyncAdvancesWithinDeadline reproduces a busy remote Frigate whose object backlog
// does not fit in one pass: each request is slow and the per-server deadline is short. A pass
// that persisted nothing until every page was fetched would be cancelled forever with the cursor
// stuck; instead each pass must commit what it fetched and stop with headroom (no error), so
// successive passes catch up.
func TestObjectSyncAdvancesWithinDeadline(t *testing.T) {
	env, syncer, _ := setup(t)
	bg := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"] // LPR-enabled
	mock := env.Mocks["frigate-h01"].Server
	const backlog = 900
	first := seedPlateBacklog(mock.Store, cam.RemoteName, backlog)
	srv := serverByName(t, bg, env.Store, "frigate-h01")
	syncer.ObjectStopMargin = 1800 * time.Millisecond

	// Warm-up: let the reviews pass store the 900 backlog reviews (slow, and not what is under
	// test) while the object requests hang until the pass deadline.
	mock.ObjectsDelay = 10 * time.Second
	warm, cancelWarm := context.WithTimeout(bg, 5*time.Second)
	_ = syncer.SyncServer(warm, srv)
	cancelWarm()
	if cur := objectCursor(t, bg, env.Store, srv.ID); cur != nil {
		t.Fatalf("object_cursor = %v after the warm-up, want it unset", cur)
	}

	// 1s per object page and a 2.5s pass with a 1.8s stop margin: page 1 is fetched and
	// committed, then the pass sees too little time left for page 2 and stops without error.
	mock.ObjectsDelay = time.Second
	ctx, cancel := context.WithTimeout(bg, 2500*time.Millisecond)
	err := syncer.SyncServer(ctx, srv)
	cancel()
	if err != nil {
		t.Fatalf("a bounded partial pass must succeed, got %v", err)
	}

	cur := objectCursor(t, bg, env.Store, srv.ID)
	if cur == nil || !cur.After(first) {
		t.Fatalf("object_cursor = %v after a bounded pass, want it advanced past the backlog start %v", cur, first)
	}
	got := countBacklogReads(t, bg, env.Store, srv.ID)
	if got == 0 {
		t.Fatal("no lpr_reads persisted by the bounded pass")
	}
	if got >= backlog {
		t.Fatalf("the bounded pass persisted the whole backlog (%d); the test no longer exercises a partial pass", got)
	}

	// Following passes, each as bounded as the first, converge on the whole backlog.
	for i := 0; i < 6 && countBacklogReads(t, bg, env.Store, srv.ID) < backlog; i++ {
		ctx, cancel := context.WithTimeout(bg, 2500*time.Millisecond)
		_ = syncer.SyncServer(ctx, srv)
		cancel()
	}
	if got := countBacklogReads(t, bg, env.Store, srv.ID); got != backlog {
		t.Fatalf("lpr_reads after successive passes = %d, want all %d", got, backlog)
	}
}

// TestObjectSyncKeepsPagesCommittedOnFetchError proves a fetch error in the middle of a paged
// pull is still reported, but the pages stored before it stay committed together with the cursor.
func TestObjectSyncKeepsPagesCommittedOnFetchError(t *testing.T) {
	env, syncer, _ := setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	mock := env.Mocks["frigate-h01"].Server
	first := seedPlateBacklog(mock.Store, cam.RemoteName, 900)
	srv := serverByName(t, ctx, env.Store, "frigate-h01")

	mock.ObjectsFailAfter = 1 // page 1 answers, page 2 fails
	if err := syncer.SyncServer(ctx, srv); err == nil {
		t.Fatal("SyncServer = nil, want the mid-pass fetch error")
	}
	cur := objectCursor(t, ctx, env.Store, srv.ID)
	if cur == nil || !cur.After(first) {
		t.Fatalf("object_cursor = %v, want it advanced past %v by the page committed before the error", cur, first)
	}
	if got := countBacklogReads(t, ctx, env.Store, srv.ID); got != 500 {
		t.Fatalf("lpr_reads = %d, want the 500 of the first page", got)
	}
}
