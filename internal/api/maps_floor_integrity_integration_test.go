//go:build integration

package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/store"
)

func TestMapsFloorRejectsOtherSiteAndDeletedBuilding(t *testing.T) {
	te := setupMapsTest(t, true)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	building, floor, otherSite := uuid.New(), uuid.New(), uuid.New()
	ctx := context.Background()
	if err := te.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO sites(id,tenant_id,name) VALUES($1,$2,'Other')`, otherSite, te.Demo.TenantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO map_buildings(id,tenant_id,site_id,name) VALUES($1,$2,$3,'Plant')`, building, te.Demo.TenantID, otherSite); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO map_floors(id,tenant_id,building_id,name) VALUES($1,$2,$3,'Plan')`, floor, te.Demo.TenantID, building)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	x, y := float32(.4), float32(.6)
	status, _, body := te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, nil, gen.MapUpsertPlacementRequest{SiteId: cam.SiteID, FloorId: &floor, X: &x, Y: &y})
	if status != http.StatusBadRequest && status != http.StatusNotFound {
		t.Fatalf("other-site floor must be rejected: %d %s", status, body)
	}
	status, _, body = te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/entities?floor_id=%s", cam.SiteID, floor), te.AdminToken, nil, nil)
	if status != http.StatusBadRequest && status != http.StatusNotFound {
		t.Fatalf("other-site floor read must be rejected: %d %s", status, body)
	}
	if err := te.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE map_buildings SET site_id=$1,deleted_at=now() WHERE id=$2`, cam.SiteID, building)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	status, _, body = te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, nil, gen.MapUpsertPlacementRequest{SiteId: cam.SiteID, FloorId: &floor, X: &x, Y: &y})
	if status != http.StatusNotFound {
		t.Fatalf("deleted building floor must be rejected: %d %s", status, body)
	}
}
