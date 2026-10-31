//go:build integration

package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/store"
)

func TestVehicleBrandModelQueryAndFilter(t *testing.T) {
	env, _, svc := setup(t)
	ctx := context.Background()

	cam := env.Cameras["frigate-h01/acceso_norte"]
	serverID := cam.ServerID
	cameraID := cam.ID
	siteID := cam.SiteID
	tenantID := env.Demo.TenantID

	evID := uuid.New()
	tSeen := time.Now().UTC().Truncate(time.Microsecond)
	plate := "AF111ZZ"

	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		// 1. Create event
		_, err := tx.Exec(ctx, `
INSERT INTO events (id, tenant_id, site_id, server_id, camera_id, remote_id, severity, labels, plates, start_time, reviewed)
VALUES ($1, $2, $3, $4, $5, 'ev-toyota-hilux', 'alert', '{car,vehicle}', ARRAY[$6], $7, false)`,
			evID, tenantID, siteID, serverID, cameraID, plate, tSeen)
		if err != nil {
			return err
		}

		// 2. Insert vehicle_attributes
		_, err = tx.Exec(ctx, `
INSERT INTO vehicle_attributes (event_id, tenant_id, site_id, camera_id, vehicle_type, vehicle_type_confidence, vehicle_color, vehicle_color_confidence, color_quality, brand, model, processed_at)
VALUES ($1, $2, $3, $4, 'pickup', 0.95, 'silver', 0.9, 'good', 'Toyota', 'Hilux', now())`,
			evID, tenantID, siteID, cameraID)
		if err != nil {
			return err
		}

		// 3. Insert lpr_reads
		_, err = tx.Exec(ctx, `
INSERT INTO lpr_reads (tenant_id, site_id, server_id, camera_id, remote_event_id, plate, plate_normalized, score, label, seen_at)
VALUES ($1, $2, $3, $4, 'ev-toyota-hilux', $5, $5, 0.98, 'car', $6)`,
			tenantID, siteID, serverID, cameraID, plate, tSeen)
		return err
	})
	if err != nil {
		t.Fatalf("setup test data: %v", err)
	}

	// 1. Test GetEvent returns brand and model
	e, err := svc.GetEvent(ctx, env.Admin, evID)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if e.Vehicle == nil {
		t.Fatalf("GetEvent: expected VehicleAttr, got nil")
	}
	if e.Vehicle.Brand != "Toyota" || e.Vehicle.Model != "Hilux" {
		t.Errorf("GetEvent Vehicle got brand=%q model=%q, want Toyota, Hilux", e.Vehicle.Brand, e.Vehicle.Model)
	}

	// 2. Test ListEvents with VehicleBrand
	evPage, err := svc.ListEvents(ctx, env.Admin, events.Filter{VehicleBrand: "Toyota"})
	if err != nil {
		t.Fatalf("ListEvents(VehicleBrand=Toyota): %v", err)
	}
	if len(evPage.Items) == 0 {
		t.Fatalf("ListEvents(VehicleBrand=Toyota): expected at least 1 item, got 0")
	}
	if evPage.Items[0].Vehicle == nil || evPage.Items[0].Vehicle.Brand != "Toyota" {
		t.Errorf("ListEvents item vehicle brand = %v, want Toyota", evPage.Items[0].Vehicle)
	}

	// 3. Test ListEvents with VehicleModel
	evPageModel, err := svc.ListEvents(ctx, env.Admin, events.Filter{VehicleModel: "Hilux"})
	if err != nil {
		t.Fatalf("ListEvents(VehicleModel=Hilux): %v", err)
	}
	if len(evPageModel.Items) == 0 {
		t.Fatalf("ListEvents(VehicleModel=Hilux): expected at least 1 item, got 0")
	}

	// 4. Test ListEvents with Nonexistent Brand
	evPageNone, err := svc.ListEvents(ctx, env.Admin, events.Filter{VehicleBrand: "NoSuchBrand123"})
	if err != nil {
		t.Fatalf("ListEvents(VehicleBrand=NoSuchBrand123): %v", err)
	}
	if len(evPageNone.Items) != 0 {
		t.Errorf("ListEvents(VehicleBrand=NoSuchBrand123): got %d items, want 0", len(evPageNone.Items))
	}

	// 5. Test ListPlates with VehicleBrand
	platePage, err := svc.ListPlates(ctx, env.Admin, events.PlateFilter{VehicleBrand: "Toyota"})
	if err != nil {
		t.Fatalf("ListPlates(VehicleBrand=Toyota): %v", err)
	}
	if len(platePage.Items) == 0 {
		t.Fatalf("ListPlates(VehicleBrand=Toyota): expected at least 1 item, got 0")
	}
	if platePage.Items[0].Vehicle == nil || platePage.Items[0].Vehicle.Brand != "Toyota" || platePage.Items[0].Vehicle.Model != "Hilux" {
		t.Errorf("ListPlates item vehicle = %v, want brand=Toyota model=Hilux", platePage.Items[0].Vehicle)
	}

	// 6. Test ListPlates with VehicleModel
	platePageModel, err := svc.ListPlates(ctx, env.Admin, events.PlateFilter{VehicleModel: "hilux"})
	if err != nil {
		t.Fatalf("ListPlates(VehicleModel=hilux): %v", err)
	}
	if len(platePageModel.Items) == 0 {
		t.Fatalf("ListPlates(VehicleModel=hilux): expected at least 1 item, got 0")
	}

	// 7. Test ListPlates with Nonexistent Model
	platePageNone, err := svc.ListPlates(ctx, env.Admin, events.PlateFilter{VehicleModel: "NoSuchModel999"})
	if err != nil {
		t.Fatalf("ListPlates(VehicleModel=NoSuchModel999): %v", err)
	}
	if len(platePageNone.Items) != 0 {
		t.Errorf("ListPlates(VehicleModel=NoSuchModel999): got %d items, want 0", len(platePageNone.Items))
	}
}
