//go:build integration

package inventory_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// deleteWithTombstone does what a camera delete does to the rows the sync looks at: soft-deletes
// the camera and leaves a tombstone for its (server, remote_name).
func deleteWithTombstone(t *testing.T, env *demofix.Env, id, tenantID, serverID uuid.UUID, remoteName string) {
	t.Helper()
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), `UPDATE cameras SET deleted_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(),
			`INSERT INTO camera_tombstones (camera_id, tenant_id, server_id, remote_name, retain_days) VALUES ($1, $2, $3, $4, 7)`,
			id, tenantID, serverID, remoteName)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func liveCamerasNamed(t *testing.T, env *demofix.Env, serverID uuid.UUID, remoteName string) int {
	t.Helper()
	var n int
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT count(*) FROM cameras WHERE server_id = $1 AND remote_name = $2 AND deleted_at IS NULL`, serverID, remoteName).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A camera that is deleted but still configured in Frigate must not come back as a new row.
func TestSyncDoesNotRecreateATombstonedCamera(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	deleteWithTombstone(t, env, cam.ID, cam.TenantID, cam.ServerID, cam.RemoteName)

	res, err := env.Svc.SyncServer(ctx, env.Admin, cam.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if got := liveCamerasNamed(t, env, cam.ServerID, cam.RemoteName); got != 0 {
		t.Fatalf("live cameras named %q after the sync = %d, want 0 (tombstoned)", cam.RemoteName, got)
	}
	if res.Added != 0 {
		t.Fatalf("sync added %d cameras, want 0: the only camera Frigate still lists under that name is tombstoned", res.Added)
	}
	if res.Skipped != 1 {
		t.Fatalf("sync skipped %d cameras, want 1", res.Skipped)
	}
	if res.Updated == 0 {
		t.Fatal("the other cameras of the server must still be updated by the sync")
	}
}

// Without a tombstone the sync behaves as before: nothing is skipped and nothing is lost.
func TestSyncWithoutTombstonesSkipsNothing(t *testing.T) {
	env := demofix.Setup(t)
	cam := env.Cameras["frigate-h01/plaza"]

	res, err := env.Svc.SyncServer(context.Background(), env.Admin, cam.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 0 || res.Added != 0 {
		t.Fatalf("sync of an untouched server: added=%d skipped=%d, want 0 and 0", res.Added, res.Skipped)
	}
	if got := liveCamerasNamed(t, env, cam.ServerID, cam.RemoteName); got != 1 {
		t.Fatalf("live cameras named %q = %d, want 1", cam.RemoteName, got)
	}
}
