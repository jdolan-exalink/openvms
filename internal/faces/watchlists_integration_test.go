//go:build integration

package faces_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/faces"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func TestWatchlistTablesForceRowLevelSecurity(t *testing.T) {
	sf := newSyncFixture(t)
	for _, table := range []string{"face_watchlists", "face_watchlist_members"} {
		var on, forced bool
		err := sf.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE relname = $1`, table).Scan(&on, &forced)
		})
		if err != nil || !on || !forced {
			t.Fatalf("%s rls = %v/%v (%v), want enabled and forced", table, on, forced, err)
		}
	}
}

func TestWatchlistCRUDMembersAuthzAndAudit(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	ana, beto := sf.person("Ana"), sf.person("Beto")

	wl, err := sf.svc.CreateWatchlist(ctx, sf.manager, sf.tenant, faces.WatchlistInput{Name: "VIP", Description: "front desk", PersonIDs: []uuid.UUID{ana.ID, ana.ID}})
	if err != nil || wl.Name != "VIP" || !slices.Equal(wl.PersonIDs, []uuid.UUID{ana.ID}) {
		t.Fatalf("create = %+v, %v (duplicate member ids collapse)", wl, err)
	}
	if _, err := sf.svc.CreateWatchlist(ctx, sf.manager, sf.tenant, faces.WatchlistInput{Name: " vip "}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate name = %v, want conflict", err)
	}
	var bad *faces.ValidationError
	if _, err := sf.svc.CreateWatchlist(ctx, sf.manager, sf.tenant, faces.WatchlistInput{Name: "Ghost", PersonIDs: []uuid.UUID{uuid.New()}}); !errors.As(err, &bad) {
		t.Fatalf("unknown person = %v, want validation error", err)
	}
	if _, err := sf.svc.CreateWatchlist(ctx, sf.manager, sf.tenant, faces.WatchlistInput{Name: "  "}); !errors.As(err, &bad) {
		t.Fatalf("blank name = %v, want validation error", err)
	}

	newName := "VIP guests"
	members := []uuid.UUID{beto.ID, ana.ID}
	up, err := sf.svc.UpdateWatchlist(ctx, sf.manager, sf.tenant, wl.ID, faces.WatchlistUpdate{Name: &newName, PersonIDs: &members})
	if err != nil || up.Name != newName || up.Description != "front desk" || len(up.PersonIDs) != 2 {
		t.Fatalf("update = %+v, %v (omitted description is kept, members replaced)", up, err)
	}
	none := []uuid.UUID{}
	if up, err = sf.svc.UpdateWatchlist(ctx, sf.manager, sf.tenant, wl.ID, faces.WatchlistUpdate{PersonIDs: &none}); err != nil || len(up.PersonIDs) != 0 {
		t.Fatalf("clear members = %+v, %v", up, err)
	}

	viewer := sf.user(sf.tenant, "viewer", "faces.view")
	if list, err := sf.svc.ListWatchlists(ctx, viewer, sf.tenant); err != nil || len(list) != 1 {
		t.Fatalf("viewer list = %v, %v", list, err)
	}
	if _, err := sf.svc.CreateWatchlist(ctx, viewer, sf.tenant, faces.WatchlistInput{Name: "Nope"}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer create = %v, want forbidden", err)
	}
	if err := sf.svc.DeleteWatchlist(ctx, viewer, sf.tenant, wl.ID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer delete = %v, want forbidden", err)
	}
	nobody := sf.user(sf.tenant, "nobody")
	if _, err := sf.svc.ListWatchlists(ctx, nobody, sf.tenant); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("no faces.view list = %v, want forbidden", err)
	}

	if err := sf.svc.DeleteWatchlist(ctx, sf.manager, sf.tenant, wl.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := sf.svc.GetWatchlist(ctx, sf.manager, sf.tenant, wl.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get after delete = %v, want not found", err)
	}
	for _, action := range []string{faces.ActionWatchlistCreated, faces.ActionWatchlistUpdated, faces.ActionWatchlistDeleted, faces.ActionWatchlistRead} {
		if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1`, action); n == 0 {
			t.Fatalf("no %s audit row", action)
		}
	}
	if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action LIKE 'FACE_WATCHLIST_%' AND (details::text LIKE '%VIP%' OR details::text LIKE '%Ana%')`); n != 0 {
		t.Fatal("watchlist audit details carry names")
	}
}

func TestWatchlistsAreTenantIsolated(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	wl, err := sf.svc.CreateWatchlist(ctx, sf.manager, sf.tenant, faces.WatchlistInput{Name: "VIP"})
	if err != nil {
		t.Fatal(err)
	}
	other := uuid.New()
	err = sf.env.Store.Tx(ctx, store.TenantScope{TenantID: other}, func(q *db.Queries) error {
		rows, err := q.ListFaceWatchlists(ctx, sf.tenant)
		if err != nil || len(rows) != 0 {
			t.Fatalf("another tenant scope sees %d watchlists (%v), want 0", len(rows), err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	stranger := sf.user(sf.tenant, "stranger", "faces.view")
	if _, err := sf.svc.GetWatchlist(ctx, stranger, uuid.New(), wl.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign tenant get = %v, want not found", err)
	}
}

func TestDeletingAPersonLeavesEveryWatchlist(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	ana, beto := sf.person("Ana"), sf.person("Beto")
	wl, err := sf.svc.CreateWatchlist(ctx, sf.manager, sf.tenant, faces.WatchlistInput{Name: "VIP", PersonIDs: []uuid.UUID{ana.ID, beto.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := sf.svc.DeletePerson(ctx, sf.manager, sf.tenant, ana.ID); err != nil {
		t.Fatal(err)
	}
	got, err := sf.svc.GetWatchlist(ctx, sf.manager, sf.tenant, wl.ID)
	if err != nil || !slices.Equal(got.PersonIDs, []uuid.UUID{beto.ID}) {
		t.Fatalf("members after deleting a person = %v, %v, want only Beto", got.PersonIDs, err)
	}
	// A tombstone cannot be re-added.
	members := []uuid.UUID{ana.ID}
	var bad *faces.ValidationError
	if _, err := sf.svc.UpdateWatchlist(ctx, sf.manager, sf.tenant, wl.ID, faces.WatchlistUpdate{PersonIDs: &members}); !errors.As(err, &bad) {
		t.Fatalf("adding a deleted person = %v, want validation error", err)
	}
}

func TestWatchlistInUseByARuleCannotBeDeleted(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	wl, err := sf.svc.CreateWatchlist(ctx, sf.manager, sf.tenant, faces.WatchlistInput{Name: "VIP"})
	if err != nil {
		t.Fatal(err)
	}
	sf.exec(`INSERT INTO rules (tenant_id, name, trigger_type, conditions, actions) VALUES ($1, 'vip', 'event', jsonb_build_object('face_watchlist_ids', jsonb_build_array($2::text)), '{}'::jsonb)`, sf.tenant, wl.ID)
	if err := sf.svc.DeleteWatchlist(ctx, sf.manager, sf.tenant, wl.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("delete in use = %v, want conflict", err)
	}
	sf.exec(`DELETE FROM rules WHERE tenant_id = $1`, sf.tenant)
	if err := sf.svc.DeleteWatchlist(ctx, sf.manager, sf.tenant, wl.ID); err != nil {
		t.Fatalf("delete once unused = %v", err)
	}
}

// A rule create that already checked the watchlist holds its row; a delete racing it must wait for
// that transaction and then see the new rule instead of removing a list the rule points at.
func TestWatchlistDeleteWaitsForAConcurrentRuleReference(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	wl, err := sf.svc.CreateWatchlist(ctx, sf.manager, sf.tenant, faces.WatchlistInput{Name: "VIP"})
	if err != nil {
		t.Fatal(err)
	}

	locked, release := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock() // a failed assertion must not leave the holder transaction open
	go func() {
		holder <- sf.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			// What a rule create does: lock the referenced list (shared), then insert the rule.
			if _, err := tx.Exec(ctx, `SELECT id FROM face_watchlists WHERE id = $1 FOR SHARE`, wl.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO rules (tenant_id, name, trigger_type, conditions, actions) VALUES ($1, 'vip', 'event', jsonb_build_object('face_watchlist_ids', jsonb_build_array($2::text)), '{}'::jsonb)`, sf.tenant, wl.ID); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked

	deleted := make(chan error, 1)
	go func() { deleted <- sf.svc.DeleteWatchlist(ctx, sf.manager, sf.tenant, wl.ID) }()
	select {
	case err := <-deleted:
		t.Fatalf("delete finished while a rule create held the list: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	unblock()
	if err := <-holder; err != nil {
		t.Fatal(err)
	}
	if err := <-deleted; !errors.Is(err, store.ErrConflict) {
		t.Fatalf("delete after the rule committed = %v, want conflict", err)
	}
	if _, err := sf.svc.GetWatchlist(ctx, sf.manager, sf.tenant, wl.ID); err != nil {
		t.Fatalf("watchlist referenced by a rule is gone: %v", err)
	}
}
