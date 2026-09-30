//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/store"
)

func TestMapsZonesFeatureFlagDisabled(t *testing.T) {
	te := setupMapsTest(t, false)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	siteID := cam.SiteID
	dummyZoneID := uuid.New()

	// 1. List zones returns 404
	status, _, _ := te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/zones", siteID), te.AdminToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("list zones: expected 404 when maps flag disabled, got %d", status)
	}

	// 2. Create zone returns 404
	status, _, _ = te.request(http.MethodPost, fmt.Sprintf("/api/v1/maps/sites/%s/zones", siteID), te.AdminToken, nil, gen.CreateMapZoneRequest{
		Name: "Test Zone",
		Kind: gen.CreateMapZoneRequestKindSecurity,
		Geometry: map[string]interface{}{
			"type": "Polygon",
			"coordinates": [][][]float64{
				{{-64.19, -31.42}, {-64.18, -31.42}, {-64.18, -31.41}, {-64.19, -31.41}, {-64.19, -31.42}},
			},
		},
	})
	if status != http.StatusNotFound {
		t.Fatalf("create zone: expected 404 when maps flag disabled, got %d", status)
	}

	// 3. Update zone returns 404
	name := "Updated Zone"
	status, _, _ = te.request(http.MethodPatch, fmt.Sprintf("/api/v1/maps/zones/%s", dummyZoneID), te.AdminToken, nil, gen.UpdateMapZoneRequest{
		Name: &name,
	})
	if status != http.StatusNotFound {
		t.Fatalf("update zone: expected 404 when maps flag disabled, got %d", status)
	}

	// 4. Delete zone returns 404
	status, _, _ = te.request(http.MethodDelete, fmt.Sprintf("/api/v1/maps/zones/%s", dummyZoneID), te.AdminToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("delete zone: expected 404 when maps flag disabled, got %d", status)
	}
}

func TestMapsZonesValidationAndLifecycle(t *testing.T) {
	te := setupMapsTest(t, true)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	siteID := cam.SiteID
	ctx := context.Background()

	// 1. Validation: Self-intersecting polygon (bowtie)
	status, _, body := te.request(http.MethodPost, fmt.Sprintf("/api/v1/maps/sites/%s/zones", siteID), te.AdminToken, nil, gen.CreateMapZoneRequest{
		Name: "Bowtie Zone",
		Kind: gen.CreateMapZoneRequestKindPerimeter,
		Geometry: map[string]interface{}{
			"type": "Polygon",
			"coordinates": [][][]float64{
				{{-64.19, -31.42}, {-64.18, -31.41}, {-64.19, -31.41}, {-64.18, -31.42}, {-64.19, -31.42}},
			},
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for self-intersecting polygon, got %d: %s", status, body)
	}

	// 2. Validation: Unclosed polygon
	status, _, body = te.request(http.MethodPost, fmt.Sprintf("/api/v1/maps/sites/%s/zones", siteID), te.AdminToken, nil, gen.CreateMapZoneRequest{
		Name: "Unclosed Zone",
		Kind: gen.CreateMapZoneRequestKindPerimeter,
		Geometry: map[string]interface{}{
			"type": "Polygon",
			"coordinates": [][][]float64{
				{{-64.19, -31.42}, {-64.18, -31.42}, {-64.18, -31.41}, {-64.19, -31.41}},
			},
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for unclosed polygon, got %d: %s", status, body)
	}

	// 3. Validation: Out of bounds geo coordinates
	status, _, body = te.request(http.MethodPost, fmt.Sprintf("/api/v1/maps/sites/%s/zones", siteID), te.AdminToken, nil, gen.CreateMapZoneRequest{
		Name: "Out of Bounds Zone",
		Kind: gen.CreateMapZoneRequestKindPerimeter,
		Geometry: map[string]interface{}{
			"type": "Polygon",
			"coordinates": [][][]float64{
				{{-195.0, -31.42}, {-64.18, -31.42}, {-64.18, -31.41}, {-195.0, -31.41}, {-195.0, -31.42}},
			},
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for lng out of bounds, got %d: %s", status, body)
	}

	// 4. Create valid geo zone
	status, _, body = te.request(http.MethodPost, fmt.Sprintf("/api/v1/maps/sites/%s/zones", siteID), te.AdminToken, nil, gen.CreateMapZoneRequest{
		Name: "Perimetro Acceso Norte",
		Kind: gen.CreateMapZoneRequestKindPerimeter,
		Geometry: map[string]interface{}{
			"type": "Polygon",
			"coordinates": [][][]float64{
				{{-64.19, -31.42}, {-64.18, -31.42}, {-64.18, -31.41}, {-64.19, -31.41}, {-64.19, -31.42}},
			},
		},
		Style: &map[string]interface{}{
			"color": "#ff0000",
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("expected 201 Created for valid zone, got %d: %s", status, body)
	}

	var createdZone gen.MapZone
	if err := json.Unmarshal(body, &createdZone); err != nil {
		t.Fatal(err)
	}
	if createdZone.Name != "Perimetro Acceso Norte" || createdZone.Kind != gen.MapZoneKindPerimeter {
		t.Fatalf("unexpected zone data: %+v", createdZone)
	}
	// Verify computed bbox
	if createdZone.MinLat == nil || *createdZone.MinLat != -31.42 || createdZone.MaxLat == nil || *createdZone.MaxLat != -31.41 {
		t.Fatalf("unexpected bbox lat: min=%v max=%v", createdZone.MinLat, createdZone.MaxLat)
	}
	if createdZone.MinLng == nil || *createdZone.MinLng != -64.19 || createdZone.MaxLng == nil || *createdZone.MaxLng != -64.18 {
		t.Fatalf("unexpected bbox lng: min=%v max=%v", createdZone.MinLng, createdZone.MaxLng)
	}

	// 5. List zones includes the created zone
	status, _, body = te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/zones", siteID), te.AdminToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list zones status: %d %s", status, body)
	}
	var zoneList gen.MapZoneList
	if err := json.Unmarshal(body, &zoneList); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, z := range zoneList.Zones {
		if z.Id == createdZone.Id {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected zone %s to be listed", createdZone.Id)
	}

	// 6. Update zone
	updatedName := "Perimetro Acceso Norte Modificado"
	newKind := gen.UpdateMapZoneRequestKindSecurity
	status, _, body = te.request(http.MethodPatch, fmt.Sprintf("/api/v1/maps/zones/%s", createdZone.Id), te.AdminToken, nil, gen.UpdateMapZoneRequest{
		Name: &updatedName,
		Kind: &newKind,
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200 for update zone, got %d: %s", status, body)
	}
	var updatedZone gen.MapZone
	if err := json.Unmarshal(body, &updatedZone); err != nil {
		t.Fatal(err)
	}
	if updatedZone.Name != updatedName || updatedZone.Kind != gen.MapZoneKindSecurity {
		t.Fatalf("unexpected updated zone: %+v", updatedZone)
	}

	// 7. Delete zone
	status, _, _ = te.request(http.MethodDelete, fmt.Sprintf("/api/v1/maps/zones/%s", createdZone.Id), te.AdminToken, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204 for delete zone, got %d", status)
	}

	// 8. Zone no longer in list
	status, _, body = te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/zones", siteID), te.AdminToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list zones status: %d %s", status, body)
	}
	_ = json.Unmarshal(body, &zoneList)
	for _, z := range zoneList.Zones {
		if z.Id == createdZone.Id {
			t.Fatalf("deleted zone %s should not be in list", createdZone.Id)
		}
	}

	// 9. Deleting again returns 404
	status, _, _ = te.request(http.MethodDelete, fmt.Sprintf("/api/v1/maps/zones/%s", createdZone.Id), te.AdminToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 on deleting non-existent zone, got %d", status)
	}

	// 10. Verify audit log entries
	var auditCreate, auditUpdate, auditDelete int
	_ = te.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'maps.zone.create' AND target_id = $1`, createdZone.Id).Scan(&auditCreate)
	_ = te.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'maps.zone.update' AND target_id = $1`, createdZone.Id).Scan(&auditUpdate)
	_ = te.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'maps.zone.delete' AND target_id = $1`, createdZone.Id).Scan(&auditDelete)

	if auditCreate != 1 || auditUpdate != 1 || auditDelete != 1 {
		t.Fatalf("expected audit counts (1,1,1), got create=%d update=%d delete=%d", auditCreate, auditUpdate, auditDelete)
	}
}

func TestMapsFloorZoneCreation(t *testing.T) {
	te := setupMapsTest(t, true)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	siteID := cam.SiteID
	ctx := context.Background()

	buildingID := uuid.New()
	floorID := uuid.New()
	err := te.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO map_buildings (id, tenant_id, site_id, name)
			VALUES ($1, $2, $3, 'Building Floor Test')
		`, buildingID, te.Demo.TenantID, siteID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO map_floors (id, tenant_id, building_id, name, ordinal)
			VALUES ($1, $2, $3, 'Floor Level 1', 1)
		`, floorID, te.Demo.TenantID, buildingID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Floor coordinates out of range (> 1.0) returns 400
	status, _, body := te.request(http.MethodPost, fmt.Sprintf("/api/v1/maps/sites/%s/zones", siteID), te.AdminToken, nil, gen.CreateMapZoneRequest{
		FloorId: &floorID,
		Name:    "Invalid Floor Zone",
		Kind:    gen.CreateMapZoneRequestKindWarning,
		Geometry: map[string]interface{}{
			"type": "Polygon",
			"coordinates": [][][]float64{
				{{0.0, 0.0}, {1.5, 0.0}, {1.5, 1.0}, {0.0, 1.0}, {0.0, 0.0}},
			},
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for floor coord > 1.0, got %d: %s", status, body)
	}

	// 2. Valid floor zone returns 201 with floor_id set and nil geo bbox
	status, _, body = te.request(http.MethodPost, fmt.Sprintf("/api/v1/maps/sites/%s/zones", siteID), te.AdminToken, nil, gen.CreateMapZoneRequest{
		FloorId: &floorID,
		Name:    "Valid Floor Zone",
		Kind:    gen.CreateMapZoneRequestKindWarning,
		Geometry: map[string]interface{}{
			"type": "Polygon",
			"coordinates": [][][]float64{
				{{0.1, 0.1}, {0.8, 0.1}, {0.8, 0.9}, {0.1, 0.9}, {0.1, 0.1}},
			},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("expected 201 for valid floor zone, got %d: %s", status, body)
	}
	var zone gen.MapZone
	_ = json.Unmarshal(body, &zone)
	if zone.FloorId == nil || *zone.FloorId != floorID {
		t.Fatalf("expected floor_id %s, got %v", floorID, zone.FloorId)
	}
	if zone.MinLat != nil || zone.MaxLat != nil {
		t.Fatalf("expected nil geo bbox for floor zone, got min=%v max=%v", zone.MinLat, zone.MaxLat)
	}
}
