//go:build integration

package inventory_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func TestHealthPollerOnCameraChange(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()

	plaza := env.Cameras["frigate-h01/plaza"]
	serverID := plaza.ServerID
	mockH01 := env.Mocks["frigate-h01"]

	// Set h01 cameras to 'offline' initially in the database
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE cameras
			SET status = 'offline'
			WHERE server_id = $1
		`, serverID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var camChanges []inventory.CameraStatusChange
	var srvChanges []inventory.StatusChange

	poller := &inventory.HealthPoller{
		Svc:         env.Svc,
		Interval:    time.Second,
		Concurrency: 2,
		OnChange: func(ctx context.Context, c inventory.StatusChange) {
			mu.Lock()
			defer mu.Unlock()
			srvChanges = append(srvChanges, c)
		},
		OnCameraChange: func(ctx context.Context, c inventory.CameraStatusChange) {
			mu.Lock()
			defer mu.Unlock()
			camChanges = append(camChanges, c)
		},
	}

	// 1. First poll: mock reports active cameras -> cameras transition from offline -> online
	poller.PollOnce(ctx)

	mu.Lock()
	if len(camChanges) == 0 {
		t.Fatalf("expected camera status changes, got 0")
	}
	foundOnline := false
	for _, c := range camChanges {
		if c.ServerID == serverID && c.From == "offline" && c.To == "online" {
			foundOnline = true
			if c.TenantID != env.Demo.TenantID || c.SiteID != plaza.SiteID {
				t.Errorf("unexpected tenant/site in camera status change: %+v", c)
			}
		}
	}
	if !foundOnline {
		t.Errorf("expected at least one camera transition from offline to online, got: %+v", camChanges)
	}
	camChanges = nil
	mu.Unlock()

	// 2. Second poll with no changes: no camera status change emitted
	poller.PollOnce(ctx)
	mu.Lock()
	if len(camChanges) != 0 {
		t.Errorf("expected 0 camera changes on repeat poll, got %d: %+v", len(camChanges), camChanges)
	}
	mu.Unlock()

	// 3. Simulate server outage: server goes offline -> cameras transition to unknown
	mockH01.Server.Offline.Store(true)
	poller.PollOnce(ctx)

	mu.Lock()
	defer mu.Unlock()
	foundUnknown := false
	for _, c := range camChanges {
		if c.ServerID == serverID && c.To == "unknown" {
			foundUnknown = true
			if c.From != "online" {
				t.Errorf("expected from=online, got from=%s", c.From)
			}
		}
	}
	if !foundUnknown {
		t.Errorf("expected camera transitions to unknown when server offline, got: %+v", camChanges)
	}
}
