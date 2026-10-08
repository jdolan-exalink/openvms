//go:build integration

package events_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/frigatemock"
	"github.com/jdolan-exalink/openvms/internal/store"
)

type storedTrack struct {
	Label   string
	Box     []float64
	Path    []map[string]float64
	Ended   bool
	Present bool
}

// readTrack loads the object_tracks row of a Frigate object id on the named mock server.
func readTrack(t *testing.T, ctx context.Context, store_ *store.Store, server, remoteID string) storedTrack {
	t.Helper()
	var out storedTrack
	err := store_.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		var box, path []byte
		err := tx.QueryRow(ctx, `
SELECT ot.label, ot.box, ot.path, ot.end_time IS NOT NULL
FROM object_tracks ot JOIN frigate_servers fs ON fs.id = ot.server_id
WHERE fs.name = $1 AND ot.remote_object_id = $2`, server, remoteID).Scan(&out.Label, &box, &path, &out.Ended)
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		out.Present = true
		if box != nil {
			if err := json.Unmarshal(box, &out.Box); err != nil {
				return err
			}
		}
		return json.Unmarshal(path, &out.Path)
	})
	if err != nil {
		t.Fatalf("read object_tracks %s/%s: %v", server, remoteID, err)
	}
	return out
}

// TestObjectTracksSync proves the syncer stores a tracked object's box and path_data, keeps
// updating the row while the object is open (even past objectOverlap) and finalizes it once
// Frigate reports the object ended.
func TestObjectTracksSync(t *testing.T) {
	env, syncer, _ := setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mock := env.Mocks["frigate-h01"].Server.Store

	// An object that started 30 minutes ago and is still open: older than objectOverlap (10 min).
	const id = "manual-track-open"
	start := float64(time.Now().Add(-30 * time.Minute).Unix())
	put := func(end *float64, track *frigatemock.Track) {
		mock.Put(frigatemock.Review{
			ID: id, Camera: cam.RemoteName, StartTime: start, EndTime: end, Severity: "detection",
			Data:  frigatemock.ReviewData{Detections: []string{id}, Objects: []string{"person"}, SubLabels: []string{}, Zones: []string{}, Audio: []string{}},
			Track: track,
		})
	}

	put(nil, &frigatemock.Track{
		Box:  []float64{0.1, 0.2, 0.3, 0.4},
		Path: [][3]float64{{0.10, 0.20, start + 1}, {0.15, 0.25, start + 2}},
	})
	syncer.SyncAll(ctx)

	got := readTrack(t, ctx, env.Store, "frigate-h01", id)
	if !got.Present {
		t.Fatal("no object_tracks row after the first sync")
	}
	if got.Label != "person" || len(got.Box) != 4 || got.Box[2] != 0.3 {
		t.Errorf("stored label/box wrong: %+v", got)
	}
	if len(got.Path) != 2 || got.Path[1]["x"] != 0.15 || got.Path[1]["t"] != start+2 {
		t.Errorf("stored path wrong: %+v", got.Path)
	}
	if got.Ended {
		t.Error("an open object must have no end_time")
	}

	// Frigate extends the path and ends the object. The first sync moved the cursor past this
	// object's start by more than objectOverlap; only the open-object hold-back re-reads it.
	end := start + 40
	put(&end, &frigatemock.Track{
		Box:  []float64{0.5, 0.6, 0.1, 0.1},
		Path: [][3]float64{{0.10, 0.20, start + 1}, {0.15, 0.25, start + 2}, {0.30, 0.40, start + 30}},
	})
	syncer.SyncAll(ctx)

	got = readTrack(t, ctx, env.Store, "frigate-h01", id)
	if len(got.Path) != 3 || got.Path[2]["x"] != 0.30 {
		t.Errorf("path not updated after the object ended: %+v", got.Path)
	}
	if len(got.Box) != 4 || got.Box[0] != 0.5 {
		t.Errorf("box not updated: %+v", got.Box)
	}
	if !got.Ended {
		t.Error("end_time not stored after the object ended")
	}
}
