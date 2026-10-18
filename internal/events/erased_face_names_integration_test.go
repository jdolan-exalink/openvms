//go:build integration

package events_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/frigatemock"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func seedLabeledReview(env *demofix.Env, camera, id string, labels ...string) {
	start := float64(time.Now().Add(-time.Minute).Unix())
	end := start + 30
	env.Mocks["frigate-h01"].Server.Store.Put(frigatemock.Review{
		ID: id, Camera: camera, StartTime: start, EndTime: &end, Severity: "alert",
		Data: frigatemock.ReviewData{Detections: []string{id}, Objects: []string{"person"}, SubLabels: labels, Zones: []string{}, Audio: []string{}},
	})
}

func storedSubLabels(t *testing.T, env *demofix.Env, remoteID string) []string {
	t.Helper()
	var got []string
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT sub_labels FROM events WHERE remote_id = $1`, remoteID).Scan(&got)
	})
	if err != nil {
		t.Fatalf("read sub_labels of %s: %v", remoteID, err)
	}
	return got
}

// A face name erased from the registry must not come back when Frigate still serves a recent
// review that carries it; names erased longer ago than the retention window are no longer
// filtered, and other tenants' erasures never apply.
func TestIngestDropsErasedFaceNames(t *testing.T) {
	env, syncer, _ := setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mark := func(name string, age time.Duration, tenantOverride bool) {
		err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			tenant := cam.TenantID
			if tenantOverride {
				if err := tx.QueryRow(ctx, `INSERT INTO tenants (slug, name) VALUES ('other-erased', 'o') RETURNING id`).Scan(&tenant); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `INSERT INTO face_erased_names (tenant_id, frigate_name, erased_at) VALUES ($1, $2, now() - $3::interval)`,
				tenant, name, age.String())
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	mark("ana", time.Hour, false)
	mark("viejo", 31*24*time.Hour, false)
	mark("ajeno", time.Hour, true)

	seedLabeledReview(env, cam.RemoteName, "erased-1", "ana", "viejo", "ajeno", "ABC123")
	syncer.SyncAll(ctx)
	got := storedSubLabels(t, env, "erased-1")
	slices.Sort(got)
	if want := []string{"ABC123", "ajeno", "viejo"}; !slices.Equal(got, want) {
		t.Fatalf("sub_labels = %v, want %v", got, want)
	}

	// An update of the same review keeps dropping the name.
	seedLabeledReview(env, cam.RemoteName, "erased-1", "ana", "ABC123")
	syncer.SyncAll(ctx)
	if got := storedSubLabels(t, env, "erased-1"); slices.Contains(got, "ana") {
		t.Fatalf("erased name came back on update: %v", got)
	}
}
