//go:build integration

package visionai_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/visionai"
)

func seedEvent(t *testing.T, env *demofix.Env, cam db.GetCameraRow, remote string, startAgo time.Duration, ended bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	end := "NULL"
	if ended {
		end = "now()"
	}
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO events (id, tenant_id, site_id, server_id, camera_id, remote_id, severity,
			labels, zones, plates, detection_ids, start_time, end_time)
			VALUES ($1,$2,$3,$4,$5,$6,'detection', ARRAY['car'], ARRAY[]::text[], ARRAY['AB123CD'], ARRAY[]::text[],
			now() - make_interval(secs => $7), `+end+`)`,
			id, cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, remote, startAgo.Seconds())
		return err
	})
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
	return id
}

func TestPGSourceStartAgeCap(t *testing.T) {
	env := demofix.Setup(t)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	stale := seedEvent(t, env, cam, "age-1", 2*time.Hour, false)
	fresh := seedEvent(t, env, cam, "age-2", 30*time.Minute, false)

	set, err := visionai.PGSource{Store: env.Store}.RecentEvents(context.Background(), env.Admin, 10*time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID]bool{}
	for _, r := range set.Rows {
		got[r.ID] = true
	}
	if got[stale] {
		t.Errorf("running event started 2h ago must be excluded by the start-age cap")
	}
	if !got[fresh] {
		t.Errorf("running event started 30 minutes ago must be included")
	}
}

func TestPGSourceWithoutLPRAccess(t *testing.T) {
	env := demofix.Setup(t)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	id := seedEvent(t, env, cam, "lpr-1", time.Minute, false)

	// "operador" holds events.view on the camera but not lpr.view.
	set, err := visionai.PGSource{Store: env.Store}.RecentEvents(context.Background(), env.Actor(t, "operador"), 10*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("actor without lpr.view must not fail: %v", err)
	}
	assertSeededWithoutPlates(t, set.Rows, id)
	if set.PlatesVisible {
		t.Fatalf("operador holds no lpr.view: PlatesVisible must be false")
	}
}

func TestPGSourcePlatesVisibleForAdmin(t *testing.T) {
	env := demofix.Setup(t)
	set, err := visionai.PGSource{Store: env.Store}.RecentEvents(context.Background(), env.Admin, 10*time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !set.PlatesVisible {
		t.Fatalf("admin holds lpr.view: PlatesVisible must be true")
	}
}

func TestLoadRowsNilLPRCameraList(t *testing.T) {
	env := demofix.Setup(t)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	id := seedEvent(t, env, cam, "nil-1", time.Minute, false)

	var rows []visionai.EventRow
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		var err error
		rows, err = visionai.LoadRows(context.Background(), tx, []uuid.UUID{cam.ID}, nil, 10*time.Minute, time.Now())
		return err
	})
	if err != nil {
		t.Fatalf("nil lpr camera list must not fail: %v", err)
	}
	assertSeededWithoutPlates(t, rows, id)
}

// assertSeededWithoutPlates checks the seeded event is present with plates
// disallowed, without depending on how many other rows the fixture holds.
func assertSeededWithoutPlates(t *testing.T, rows []visionai.EventRow, id uuid.UUID) {
	t.Helper()
	for _, r := range rows {
		if r.ID == id {
			if r.PlatesAllowed {
				t.Fatalf("seeded event %s must have plates disallowed", id)
			}
			return
		}
	}
	t.Fatalf("seeded event %s missing from rows %+v", id, rows)
}
