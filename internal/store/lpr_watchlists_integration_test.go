//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func TestLprWatchlistsStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	env := demofix.Setup(t)
	tenantID := env.Demo.TenantID
	actor := authz.Actor{TenantID: &tenantID, UserID: env.Admin.UserID}

	err := env.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		q := db.New(tx)

		// 1. Create watchlist
		created, err := q.CreateLprWatchlist(ctx, db.CreateLprWatchlistParams{
			TenantID:    tenantID,
			Name:        "Vehículos Robados",
			Description: "Alertas de pedidos de secuestro",
			Color:       "#dc2626",
			CreatedBy:   &env.Admin.UserID,
		})
		if err != nil {
			t.Fatalf("CreateLprWatchlist failed: %v", err)
		}
		if created.Name != "Vehículos Robados" || created.Color != "#dc2626" {
			t.Fatalf("unexpected created watchlist: %+v", created)
		}

		// 2. Add items to watchlist
		item1, err := q.AddLprWatchlistItem(ctx, db.AddLprWatchlistItemParams{
			TenantID:     tenantID,
			WatchlistID:  created.ID,
			Plate:        "AB123CD",
			Notes:        "Pedido judicial",
			VehicleBrand: "Ford",
			VehicleModel: "Focus",
			VehicleColor: "Gris",
			OwnerName:    "Juan Pérez",
		})
		if err != nil {
			t.Fatalf("AddLprWatchlistItem 1 failed: %v", err)
		}
		if item1.Plate != "AB123CD" || item1.VehicleBrand != "Ford" {
			t.Fatalf("unexpected item 1: %+v", item1)
		}

		item2, err := q.AddLprWatchlistItem(ctx, db.AddLprWatchlistItemParams{
			TenantID:     tenantID,
			WatchlistID:  created.ID,
			Plate:        "AF456GH",
			Notes:        "Alerta temprana",
			VehicleBrand: "Toyota",
			VehicleModel: "Corolla",
			VehicleColor: "Blanco",
			OwnerName:    "Empresa SRL",
		})
		if err != nil {
			t.Fatalf("AddLprWatchlistItem 2 failed: %v", err)
		}
		if item2.Plate != "AF456GH" {
			t.Fatalf("unexpected item 2: %+v", item2)
		}

		// 3. Update existing item via ON CONFLICT
		updatedItem1, err := q.AddLprWatchlistItem(ctx, db.AddLprWatchlistItemParams{
			TenantID:     tenantID,
			WatchlistID:  created.ID,
			Plate:        "AB123CD",
			Notes:        "Secuestro ordenado por juzgado 4",
			VehicleBrand: "Ford",
			VehicleModel: "Focus Titanium",
			VehicleColor: "Gris Plata",
			OwnerName:    "Juan Pérez Actualizado",
		})
		if err != nil {
			t.Fatalf("AddLprWatchlistItem update failed: %v", err)
		}
		if updatedItem1.Notes != "Secuestro ordenado por juzgado 4" || updatedItem1.VehicleModel != "Focus Titanium" {
			t.Fatalf("item was not updated on conflict: %+v", updatedItem1)
		}

		// 4. List watchlists with item_count
		lists, err := q.ListLprWatchlists(ctx, tenantID)
		if err != nil {
			t.Fatalf("ListLprWatchlists failed: %v", err)
		}
		if len(lists) != 1 {
			t.Fatalf("expected 1 watchlist, got %d", len(lists))
		}
		if lists[0].ItemCount != 2 {
			t.Fatalf("expected item_count 2, got %d", lists[0].ItemCount)
		}

		// 5. Get watchlist
		got, err := q.GetLprWatchlist(ctx, db.GetLprWatchlistParams{
			TenantID: tenantID,
			ID:       created.ID,
		})
		if err != nil {
			t.Fatalf("GetLprWatchlist failed: %v", err)
		}
		if got.ID != created.ID || got.ItemCount != 2 {
			t.Fatalf("unexpected GetLprWatchlist: %+v", got)
		}

		// 6. List items
		items, err := q.ListLprWatchlistItems(ctx, db.ListLprWatchlistItemsParams{
			TenantID:    tenantID,
			WatchlistID: created.ID,
		})
		if err != nil {
			t.Fatalf("ListLprWatchlistItems failed: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(items))
		}

		// 7. MatchLprWatchlistsForPlates
		matches, err := q.MatchLprWatchlistsForPlates(ctx, db.MatchLprWatchlistsForPlatesParams{
			TenantID: tenantID,
			Plates:   []string{"AB123CD", "NOTFOUND"},
		})
		if err != nil {
			t.Fatalf("MatchLprWatchlistsForPlates failed: %v", err)
		}
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
		if matches[0].WatchlistID != created.ID || strings.ToUpper(matches[0].Plate) != "AB123CD" {
			t.Fatalf("unexpected match: %+v", matches[0])
		}

		// 8. Remove item
		removed, err := q.RemoveLprWatchlistItem(ctx, db.RemoveLprWatchlistItemParams{
			TenantID:    tenantID,
			WatchlistID: created.ID,
			ID:          item2.ID,
		})
		if err != nil {
			t.Fatalf("RemoveLprWatchlistItem failed: %v", err)
		}
		if removed != 1 {
			t.Fatalf("expected 1 removed row, got %d", removed)
		}

		// 9. Delete watchlist (cascades)
		deleted, err := q.DeleteLprWatchlist(ctx, db.DeleteLprWatchlistParams{
			TenantID: tenantID,
			ID:       created.ID,
		})
		if err != nil {
			t.Fatalf("DeleteLprWatchlist failed: %v", err)
		}
		if deleted != 1 {
			t.Fatalf("expected 1 deleted row, got %d", deleted)
		}

		// Verify empty
		remaining, err := q.ListLprWatchlists(ctx, tenantID)
		if err != nil {
			t.Fatalf("ListLprWatchlists after delete failed: %v", err)
		}
		if len(remaining) != 0 {
			t.Fatalf("expected 0 watchlists, got %d", len(remaining))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}
}
