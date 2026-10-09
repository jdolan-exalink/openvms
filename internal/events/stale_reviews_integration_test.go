//go:build integration

package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/frigatemock"
	"github.com/jdolan-exalink/openvms/internal/store"
)

func readEventEnd(t *testing.T, ctx context.Context, store_ *store.Store, server, remoteID string) *time.Time {
	t.Helper()
	var end *time.Time
	err := store_.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
SELECT e.end_time FROM events e JOIN frigate_servers fs ON fs.id = e.server_id
WHERE fs.name = $1 AND e.remote_id = $2`, server, remoteID).Scan(&end)
	})
	if err != nil {
		t.Fatalf("read event %s/%s: %v", server, remoteID, err)
	}
	return end
}

// TestStaleOpenReviewsAreResolved covers the three outcomes for an event open for more than
// StaleOpenWindow (the regular pull no longer re-reads it): Frigate closed it, Frigate lost it,
// Frigate still has it open.
func TestStaleOpenReviewsAreResolved(t *testing.T) {
	env, syncer, _ := setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mock := env.Mocks["frigate-h01"].Server.Store

	start := float64(time.Now().Add(-3 * time.Hour).Unix())
	put := func(id string, end *float64, track *frigatemock.Track) {
		mock.Put(frigatemock.Review{
			ID: id, Camera: cam.RemoteName, StartTime: start, EndTime: end, Severity: "detection",
			Data:  frigatemock.ReviewData{Detections: []string{id}, Objects: []string{"person"}, SubLabels: []string{}, Zones: []string{}, Audio: []string{}},
			Track: track,
		})
	}
	const closedID, goneID, openID = "stale-closed", "stale-gone", "stale-open"
	put(closedID, nil, nil)
	put(goneID, nil, &frigatemock.Track{Box: []float64{0.1, 0.1, 0.1, 0.1}, Path: [][3]float64{{0.1, 0.1, start + 5}, {0.2, 0.2, start + 600}}})
	put(openID, nil, nil)
	syncer.SyncAll(ctx)
	for _, id := range []string{closedID, goneID, openID} {
		if readEventEnd(t, ctx, env.Store, "frigate-h01", id) != nil {
			t.Fatalf("%s must start open", id)
		}
	}

	// Frigate closes one late, loses another, and keeps the third open.
	end := start + 120
	put(closedID, &end, nil)
	mock.Delete(goneID)
	syncer.SyncAll(ctx)

	got := readEventEnd(t, ctx, env.Store, "frigate-h01", closedID)
	if got == nil || got.Unix() != int64(end) {
		t.Errorf("closed in Frigate: end_time = %v, want %v", got, time.Unix(int64(end), 0))
	}
	got = readEventEnd(t, ctx, env.Store, "frigate-h01", goneID)
	if want := int64(start + 600); got == nil || got.Unix() != want {
		t.Errorf("gone from Frigate: end_time = %v, want last track point %v", got, time.Unix(want, 0))
	}
	if got := readEventEnd(t, ctx, env.Store, "frigate-h01", openID); got != nil {
		t.Errorf("still open in Frigate: end_time = %v, want untouched", got)
	}
}

// TestListEventsOverlap proves the overlap filter returns events running during the window that
// started before it, and ignores stale open rows.
func TestListEventsOverlap(t *testing.T) {
	env, syncer, svc := setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mock := env.Mocks["frigate-h01"].Server.Store

	base := time.Now().Add(-6 * time.Hour).Truncate(time.Second)
	unix := func(t time.Time) float64 { return float64(t.Unix()) }
	put := func(id string, start time.Time, end *time.Time) {
		var e *float64
		if end != nil {
			v := unix(*end)
			e = &v
		}
		mock.Put(frigatemock.Review{
			ID: id, Camera: cam.RemoteName, StartTime: unix(start), EndTime: e, Severity: "detection",
			Data: frigatemock.ReviewData{Detections: []string{id}, Objects: []string{"person"}, SubLabels: []string{}, Zones: []string{}, Audio: []string{}},
		})
	}
	winFrom, winTo := base, base.Add(10*time.Minute)
	endsInside := base.Add(2 * time.Minute)
	endsBefore := base.Add(-time.Minute)
	put("ov-spans", base.Add(-5*time.Minute), &endsInside)
	put("ov-before", base.Add(-10*time.Minute), &endsBefore)
	put("ov-fresh-open", base.Add(-30*time.Minute), nil)
	put("ov-stale-open", base.Add(-5*time.Hour), nil)
	syncer.SyncAll(ctx)

	ids := func(f events.Filter) map[string]bool {
		f.CameraIDs = []uuid.UUID{cam.ID}
		f.Limit = 500
		page, err := svc.ListEvents(ctx, env.Admin, f)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, e := range page.Items {
			out[e.RemoteID] = true
		}
		return out
	}
	got := ids(events.Filter{From: &winFrom, To: &winTo, Overlap: true})
	if !got["ov-spans"] {
		t.Error("an event that started before the window and ended inside it must match")
	}
	if got["ov-before"] || got["ov-stale-open"] {
		t.Errorf("events outside the window or stale-open must not match: %v", got)
	}
	if got["ov-fresh-open"] {
		t.Log("fresh open event within the stale window matches (expected)")
	} else {
		t.Error("an open event that started within StaleOpenWindow before the window must match")
	}
	if plain := ids(events.Filter{From: &winFrom, To: &winTo}); plain["ov-spans"] {
		t.Error("without overlap only events starting inside the window match")
	}
}
