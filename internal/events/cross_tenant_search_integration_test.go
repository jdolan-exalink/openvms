//go:build integration

package events_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// seedForeignEvent inserts a minimal site/server/camera/event/plate-read for tenantID by raw
// SQL. Cross-tenant search only needs the rows to exist -- not a working Frigate -- so this
// avoids standing up a second mock server just to prove isolation.
func seedForeignEvent(t *testing.T, env *demofix.Env, tenantID uuid.UUID, plate string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	siteID, serverID, camID, evID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
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
		if _, err := tx.Exec(ctx, `INSERT INTO events (id, tenant_id, site_id, server_id, camera_id, remote_id, severity, start_time, plates)
VALUES ($1, $2, $3, $4, $5, 'remote-1', 'alert', now(), ARRAY[$6::text])`,
			evID, tenantID, siteID, serverID, camID, plate); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO lpr_reads (id, tenant_id, site_id, server_id, camera_id, remote_event_id, plate, plate_normalized, seen_at)
VALUES ($1, $2, $3, $4, $5, 'remote-1', $6, $6, now())`, uuid.New(), tenantID, siteID, serverID, camID, plate)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return evID
}

// grantFullAccess grants tenantID-wide events/LPR/camera permissions to userID, acting as the
// platform admin (who already holds every permission at platform scope).
func grantFullAccess(t *testing.T, env *demofix.Env, tenantID, userID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	for _, p := range []authz.Permission{authz.CamerasView, authz.EventsView, authz.EventsSearch, authz.LPRView, authz.LPRSearch} {
		if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: userID, Permission: p,
			Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenantID,
		}); err != nil {
			t.Fatalf("grant %s: %v", p, err)
		}
	}
}

// TestCrossTenantSearchIsolation proves events.Service never leaks another tenant's events or
// plate reads: not through ListEvents/GetEvent, and not through ListPlates (partial or exact),
// even for an actor who holds every relevant permission in their own tenant.
func TestCrossTenantSearchIsolation(t *testing.T) {
	env, syncer, svc := setup(t)
	ctx := context.Background()
	syncer.SyncAll(ctx)

	// Tenant A: a full-access tenant admin, in addition to the already-covered "operador".
	adminA, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "admin-a-full")
	if err != nil {
		t.Fatal(err)
	}
	grantFullAccess(t, env, env.Demo.TenantID, adminA.UserID)

	// Tenant B: its own full-access tenant admin, with its own event and plate read seeded
	// directly (no live Frigate needed for a read-only search test).
	tenantB, err := env.Svc.CreateTenant(ctx, env.Admin, "cross-tenant-b", "Cross Tenant B")
	if err != nil {
		t.Fatal(err)
	}
	adminB, _, err := bootstrap.TenantUser(ctx, env.Store, tenantB.ID, "admin-b-full")
	if err != nil {
		t.Fatal(err)
	}
	grantFullAccess(t, env, tenantB.ID, adminB.UserID)

	plate := events.NormalizePlate("zz 999 zz")
	eventB := seedForeignEvent(t, env, tenantB.ID, plate)

	// Sanity check: tenant B's own admin sees the seeded event and plate read. This proves the
	// zero-result assertions below are not vacuous -- they would fail (find the row) if tenant
	// scoping were bypassed.
	t.Run("sanity: tenant B sees its own seeded event and plate", func(t *testing.T) {
		evs, err := svc.ListEvents(ctx, adminB, events.Filter{Plate: plate})
		if err != nil {
			t.Fatal(err)
		}
		if len(evs.Items) != 1 || evs.Items[0].ID != eventB {
			t.Fatalf("tenant B admin ListEvents(plate=%s) = %+v, want the seeded event", plate, evs.Items)
		}
		reads, err := svc.ListPlates(ctx, adminB, events.PlateFilter{Plate: plate})
		if err != nil {
			t.Fatal(err)
		}
		if len(reads.Items) != 1 {
			t.Fatalf("tenant B admin ListPlates(plate=%s) = %+v, want 1 read", plate, reads.Items)
		}
		if _, err := svc.GetEvent(ctx, adminB, eventB); err != nil {
			t.Fatalf("tenant B admin GetEvent(own event) = %v", err)
		}
	})

	for _, tc := range []struct {
		name  string
		actor authz.Actor
	}{
		{"tenant A operator", env.Actor(t, "operador")},
		{"tenant A full-access admin", adminA},
	} {
		t.Run(tc.name+" gets zero results for tenant B's event and plate", func(t *testing.T) {
			a := tc.actor

			evs, err := svc.ListEvents(ctx, a, events.Filter{Plate: plate})
			if err != nil {
				t.Fatal(err)
			}
			if len(evs.Items) != 0 {
				t.Errorf("ListEvents(plate=%s) = %d items, want 0", plate, len(evs.Items))
			}

			all, err := svc.ListEvents(ctx, a, events.Filter{Limit: 500})
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range all.Items {
				if e.ID == eventB {
					t.Errorf("plain ListEvents leaked tenant B's event %s", e.ID)
				}
			}

			if _, err := svc.GetEvent(ctx, a, eventB); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("GetEvent(tenant B's event) = %v, want not found", err)
			}

			partial, err := svc.ListPlates(ctx, a, events.PlateFilter{Plate: plate[1:5]})
			if err != nil {
				t.Fatal(err)
			}
			if len(partial.Items) != 0 {
				t.Errorf("ListPlates partial(%s) = %d items, want 0", plate[1:5], len(partial.Items))
			}

			exact, err := svc.ListPlates(ctx, a, events.PlateFilter{Plate: plate, Exact: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(exact.Items) != 0 {
				t.Errorf("ListPlates exact(%s) = %d items, want 0", plate, len(exact.Items))
			}
		})
	}

	t.Run("row-level security hides tenant B's event and plate read even from raw SQL", func(t *testing.T) {
		tx, err := env.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", env.Demo.TenantID.String()); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM events WHERE id = $1", eventB).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("tenant A raw SQL sees %d rows of tenant B's event through events RLS", n)
		}
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM lpr_reads WHERE plate_normalized = $1", plate).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("tenant A raw SQL sees %d rows of tenant B's plate read through lpr_reads RLS", n)
		}
	})
}
