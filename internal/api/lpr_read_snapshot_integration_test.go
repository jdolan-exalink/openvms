//go:build integration

package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// seedForeignLPRRead inserts a minimal site/server/camera/lpr_reads row for tenantID by raw SQL,
// so a cross-tenant 404 can be proven without standing up a second Frigate mock.
func seedForeignLPRRead(t *testing.T, env *demofix.Env, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	siteID, serverID, camID, readID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	err := env.Store.TxRaw(ctx, store.TenantScope{TenantID: tenantID}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, 'Cross-tenant site')`,
			siteID, tenantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url)
VALUES ($1, $2, $3, 'cross-tenant-server', 'http://example.invalid')`, serverID, tenantID, siteID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO cameras (id, tenant_id, site_id, server_id, remote_name, display_name, lpr)
VALUES ($1, $2, $3, $4, 'cam1', 'Cross-tenant camera', true)`, camID, tenantID, siteID, serverID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO lpr_reads (id, tenant_id, site_id, server_id, camera_id, remote_event_id, plate, plate_normalized, seen_at)
VALUES ($1, $2, $3, $4, $5, 'remote-1', 'ZZ999ZZ', 'ZZ999ZZ', now())`, readID, tenantID, siteID, serverID, camID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return readID
}

// TestLPRReadSnapshotEndpoint covers the new GET /media/v1/lpr/reads/{id}/snapshot.jpg
// endpoint: an actor with snapshots.view+lpr.view on the read's camera gets the mock Frigate's
// image for the read's own remote_event_id (not the event's detections[0]); an actor with
// snapshots.view but no lpr.view is forbidden; and another tenant's read id 404s.
func TestLPRReadSnapshotEndpoint(t *testing.T) {
	env := demofix.Setup(t)
	adapters := inventory.NewAdapters(env.Svc)
	syncer := &events.Syncer{
		Store: env.Store, Adapters: adapters, Blobs: noopBlobs{}, Log: pgtest.Discard(),
		Interval: time.Second, Backfill: 24 * time.Hour, Concurrency: 2,
	}
	syncer.SyncAll(context.Background())
	evSvc := &events.Service{Store: env.Store, Blobs: noopBlobs{}, Adapters: adapters, Log: pgtest.Discard()}
	mediaSvc := &media.Service{Store: env.Store, Adapters: adapters, Log: pgtest.Discard()}

	h := &api.Handlers{Inv: env.Svc, Events: evSvc, Media: mediaSvc, Log: pgtest.Discard()}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{
		Queries: db.New(env.Pool),
		Media:   (&media.Gateway{Svc: mediaSvc, Actor: api.ActorFrom}).Routes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()

	get := func(t *testing.T, token, path string) *http.Response {
		t.Helper()
		req, _ := http.NewRequestWithContext(context.Background(), "GET", ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	camA := env.Cameras["frigate-h01/acceso_norte"] // LPR-enabled

	ctx := context.Background()
	page, err := evSvc.ListPlates(ctx, env.Admin, events.PlateFilter{CameraIDs: []uuid.UUID{camA.ID}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) == 0 {
		t.Fatalf("camera %s has no synced plate reads", camA.RemoteName)
	}
	readID := page.Items[0].ID

	t.Run("actor with snapshots.view and lpr.view gets the mock's image", func(t *testing.T) {
		resp := get(t, env.AdminToken, "/media/v1/lpr/reads/"+readID.String()+"/snapshot.jpg")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
			t.Errorf("Content-Type = %q, want image/jpeg", ct)
		}
	})

	t.Run("actor without lpr.view is forbidden", func(t *testing.T) {
		u, token, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "snapshot-only")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: u.UserID, Permission: authz.SnapshotsView,
			Effect: authz.Allow, ScopeType: authz.ScopeCamera, ScopeID: &camA.ID,
		}); err != nil {
			t.Fatal(err)
		}
		resp := get(t, token, "/media/v1/lpr/reads/"+readID.String()+"/snapshot.jpg")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("another tenant's read id 404s", func(t *testing.T) {
		// env.AdminToken is a platform actor that sees every tenant (store.ScopeFor), so this
		// must use a tenant-scoped actor to actually exercise row-level security isolation.
		u, token, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "cross-tenant-lpr-a")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []authz.Permission{authz.SnapshotsView, authz.LPRView} {
			if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
				SubjectType: "user", SubjectID: u.UserID, Permission: p,
				Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &env.Demo.TenantID,
			}); err != nil {
				t.Fatal(err)
			}
		}

		tenantB, err := env.Svc.CreateTenant(ctx, env.Admin, "lpr-snapshot-cross-tenant", "Cross Tenant")
		if err != nil {
			t.Fatal(err)
		}
		foreignID := seedForeignLPRRead(t, env, tenantB.ID)

		resp := get(t, token, "/media/v1/lpr/reads/"+foreignID.String()+"/snapshot.jpg")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})
}
