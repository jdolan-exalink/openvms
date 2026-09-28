//go:build integration

package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/frigatemock"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// seedReview injects a finished review directly into a mock Frigate's store, on the given
// camera, with one detection sharing the review's own id, at the given severity (which decides
// HasSnapshot for its derived tracked object, see internal/frigatemock/media.go objects).
func seedReview(t *testing.T, env *demofix.Env, server, camera, id, severity string) {
	t.Helper()
	start := float64(time.Now().Add(-time.Minute).Unix())
	end := start + 30
	env.Mocks[server].Server.Store.Put(frigatemock.Review{
		ID: id, Camera: camera, StartTime: start, EndTime: &end, Severity: severity,
		Data: frigatemock.ReviewData{Detections: []string{id}, Objects: []string{"person"}, SubLabels: []string{}, Zones: []string{}, Audio: []string{}},
	})
}

// hasSnapshotForRemoteID reads has_snapshot for the event ingested from the review with this
// remote id, on the named mock server.
func hasSnapshotForRemoteID(t *testing.T, env *demofix.Env, server, remoteID string) bool {
	t.Helper()
	ctx := context.Background()
	var got bool
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
SELECT e.has_snapshot FROM events e JOIN frigate_servers fs ON fs.id = e.server_id
WHERE fs.name = $1 AND e.remote_id = $2`, server, remoteID).Scan(&got)
	})
	if err != nil {
		t.Fatalf("read has_snapshot for %s/%s: %v", server, remoteID, err)
	}
	return got
}

// putObjectSnapshot directly records, in object_snapshots, that a tracked object id reported
// has_snapshot=true — standing in for that fact being observed independently of the review's own
// upsert cycle (e.g. the object was synced while the review's detection_ids did not include it
// yet, or the review had not been indexed at all): a race the mock's HTTP surface cannot
// reproduce directly, since it always derives tracked objects from whatever is in Store at the
// instant of the call, so review and its objects are otherwise never observed out of sync.
func putObjectSnapshot(t *testing.T, env *demofix.Env, server, remoteObjectID string) {
	t.Helper()
	ctx := context.Background()
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO object_snapshots (server_id, tenant_id, remote_object_id)
SELECT fs.id, fs.tenant_id, $2 FROM frigate_servers fs WHERE fs.name = $1
ON CONFLICT (server_id, remote_object_id) DO NOTHING`, server, remoteObjectID)
		return err
	})
	if err != nil {
		t.Fatalf("insert object_snapshot for %s/%s: %v", server, remoteObjectID, err)
	}
}

// TestHasSnapshotConvergence proves the fix for the review finding on commits 7658e50..6b8a355:
// markHasSnapshot only updates an events row that already exists and already lists the object's
// id in detection_ids, so a has_snapshot fact recorded independently of the review's own upsert
// (e.g. seen before the review's detection_ids widened to include it) used to be silently lost
// once the object's own re-sync window closed. upsertReview now also derives has_snapshot from
// object_snapshots on every review upsert (the same pattern it already uses to derive plates
// from lpr_reads), so the fact converges regardless of arrival order.
func TestHasSnapshotConvergence(t *testing.T) {
	env, syncer, _ := setup(t)
	ctx := context.Background()

	camPlain := env.Cameras["frigate-h01/plaza"] // no LPR capability, unrelated to this fact

	t.Run("a has_snapshot fact recorded out of band is picked up on the review's next upsert", func(t *testing.T) {
		const id = "manual-conv-oob"
		seedReview(t, env, "frigate-h01", camPlain.RemoteName, id, "detection")
		syncer.SyncAll(ctx)
		if got := hasSnapshotForRemoteID(t, env, "frigate-h01", id); got {
			t.Fatalf("has_snapshot = true right after a detection-severity sync, want false")
		}

		// The fact arrives independently of a normal object poll (see putObjectSnapshot).
		putObjectSnapshot(t, env, "frigate-h01", id)

		// The review is still open and within reviewOverlap, so the next pull re-upserts it,
		// which must now re-derive has_snapshot from object_snapshots.
		syncer.SyncAll(ctx)
		if got := hasSnapshotForRemoteID(t, env, "frigate-h01", id); !got {
			t.Errorf("has_snapshot = false after object_snapshots recorded the fact and the review was re-upserted, want true")
		}
	})

	t.Run("a delayed has_snapshot=true is picked up when Frigate flips it later", func(t *testing.T) {
		const id = "manual-conv-delayed"
		seedReview(t, env, "frigate-h01", camPlain.RemoteName, id, "detection")
		syncer.SyncAll(ctx)
		if got := hasSnapshotForRemoteID(t, env, "frigate-h01", id); got {
			t.Fatalf("has_snapshot = true right after a detection-severity sync, want false")
		}

		// Frigate later reports the same review (and its one detection) as alert-severity, so
		// its tracked object now reports has_snapshot=true.
		seedReview(t, env, "frigate-h01", camPlain.RemoteName, id, "alert")
		syncer.SyncAll(ctx)
		if got := hasSnapshotForRemoteID(t, env, "frigate-h01", id); !got {
			t.Errorf("has_snapshot = false after Frigate flipped it to true on a later sync, want true")
		}
	})
}
