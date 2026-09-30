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

func TestMapsWriteFeatureFlagDisabled(t *testing.T) {
	te := setupMapsTest(t, false)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	siteID := cam.SiteID
	dummyPlacementID := uuid.New()

	// 1. Unplaced cameras returns 404
	status, _, _ := te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/unplaced?site_id=%s", siteID), te.AdminToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("unplaced: expected 404 when maps flag disabled, got %d", status)
	}

	// 2. Placement upsert returns 404
	lat := -34.6037
	lng := -58.3816
	upsertBody := gen.MapUpsertPlacementRequest{
		SiteId: siteID,
		Lat:    &lat,
		Lng:    &lng,
	}
	status, _, _ = te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, nil, upsertBody)
	if status != http.StatusNotFound {
		t.Fatalf("upsert: expected 404 when maps flag disabled, got %d", status)
	}

	// 3. Placement delete returns 404
	status, _, _ = te.request(http.MethodDelete, fmt.Sprintf("/api/v1/maps/placements/%s", dummyPlacementID), te.AdminToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("delete: expected 404 when maps flag disabled, got %d", status)
	}

	// 4. Site geo update returns 404
	status, _, _ = te.request(http.MethodPatch, fmt.Sprintf("/api/v1/sites/%s/geo", siteID), te.AdminToken, nil, gen.UpdateSiteGeoRequest{
		Lat: &lat,
		Lng: &lng,
	})
	if status != http.StatusNotFound {
		t.Fatalf("site geo: expected 404 when maps flag disabled, got %d", status)
	}
}

func TestMapsUnplacedAndPlacementLifecycle(t *testing.T) {
	te := setupMapsTest(t, true)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	siteID := cam.SiteID

	// 1. Initially, camera is unplaced
	status, _, body := te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/unplaced?site_id=%s", siteID), te.AdminToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200 for unplaced, got %d: %s", status, body)
	}
	var unplacedList gen.MapUnplacedCameraList
	if err := json.Unmarshal(body, &unplacedList); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range unplacedList.Cameras {
		if c.Id == cam.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("camera %s was expected in unplaced list", cam.ID)
	}

	// 2. Validation failures on upsert
	// Missing lat/lng for geo placement
	status, _, body = te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, nil, gen.MapUpsertPlacementRequest{
		SiteId: siteID,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing coords, got %d: %s", status, body)
	}

	// Out of range lat
	badLat := 95.0
	goodLng := -58.3816
	status, _, body = te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, nil, gen.MapUpsertPlacementRequest{
		SiteId: siteID,
		Lat:    &badLat,
		Lng:    &goodLng,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad lat, got %d: %s", status, body)
	}

	// Providing x/y for geo placement (floor_id is null)
	badX := float32(0.5)
	goodLat := -34.6037
	status, _, body = te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, nil, gen.MapUpsertPlacementRequest{
		SiteId: siteID,
		Lat:    &goodLat,
		Lng:    &goodLng,
		X:      &badX,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for x provided in geo placement, got %d: %s", status, body)
	}

	// 3. Valid geo placement upsert
	bearing := float32(180)
	fov := float32(90)
	rangeM := float32(50)
	status, headers, body := te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, nil, gen.MapUpsertPlacementRequest{
		SiteId:     siteID,
		Lat:        &goodLat,
		Lng:        &goodLng,
		BearingDeg: &bearing,
		FovDeg:     &fov,
		RangeM:     &rangeM,
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200 for upsert placement, got %d: %s", status, body)
	}
	etag := headers.Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header on placement upsert")
	}

	var placement gen.MapPlacement
	if err := json.Unmarshal(body, &placement); err != nil {
		t.Fatal(err)
	}
	if placement.EntityId != cam.ID || placement.Revision != 1 {
		t.Fatalf("unexpected placement: id=%s revision=%d", placement.EntityId, placement.Revision)
	}
	if etag != fmt.Sprintf(`"%d"`, placement.Revision) {
		t.Fatalf("ETag %s does not match revision %d", etag, placement.Revision)
	}

	// 4. Camera is no longer in unplaced list
	status, _, body = te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/unplaced?site_id=%s", siteID), te.AdminToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("unplaced list error: %d %s", status, body)
	}
	_ = json.Unmarshal(body, &unplacedList)
	for _, c := range unplacedList.Cameras {
		if c.Id == cam.ID {
			t.Fatalf("camera %s should not be in unplaced list after placement", cam.ID)
		}
	}

	// 5. Optimistic concurrency control (If-Match)
	// Mismatched If-Match returns 409 Conflict
	status, _, body = te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, map[string]string{
		"If-Match": `"999"`,
	}, gen.MapUpsertPlacementRequest{
		SiteId: siteID,
		Lat:    &goodLat,
		Lng:    &goodLng,
	})
	if status != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on revision mismatch, got %d: %s", status, body)
	}

	// Matching If-Match succeeds and increments revision
	newLat := -34.6040
	status, headers, body = te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, map[string]string{
		"If-Match": etag,
	}, gen.MapUpsertPlacementRequest{
		SiteId: siteID,
		Lat:    &newLat,
		Lng:    &goodLng,
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200 for upsert with matching If-Match, got %d: %s", status, body)
	}
	var updatedPlacement gen.MapPlacement
	_ = json.Unmarshal(body, &updatedPlacement)
	if updatedPlacement.Revision != 2 {
		t.Fatalf("expected revision 2, got %d", updatedPlacement.Revision)
	}
	newETag := headers.Get("ETag")
	if newETag != `"2"` {
		t.Fatalf("expected ETag \"2\", got %s", newETag)
	}

	// 6. Delete placement with optimistic locking
	// Mismatched revision returns 409
	status, _, body = te.request(http.MethodDelete, fmt.Sprintf("/api/v1/maps/placements/%s", updatedPlacement.Id), te.AdminToken, map[string]string{
		"If-Match": `"1"`,
	}, nil)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 on delete revision mismatch, got %d: %s", status, body)
	}

	// Matching revision returns 204
	status, _, _ = te.request(http.MethodDelete, fmt.Sprintf("/api/v1/maps/placements/%s", updatedPlacement.Id), te.AdminToken, map[string]string{
		"If-Match": newETag,
	}, nil)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204 No Content on delete, got %d", status)
	}

	// Second delete returns 404
	status, _, _ = te.request(http.MethodDelete, fmt.Sprintf("/api/v1/maps/placements/%s", updatedPlacement.Id), te.AdminToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 on deleting non-existent placement, got %d", status)
	}

	// 7. Camera is back in unplaced list!
	status, _, body = te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/unplaced?site_id=%s", siteID), te.AdminToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("unplaced list error: %d %s", status, body)
	}
	_ = json.Unmarshal(body, &unplacedList)
	found = false
	for _, c := range unplacedList.Cameras {
		if c.Id == cam.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("camera %s was expected back in unplaced list after delete", cam.ID)
	}
}

func TestMapsFloorPlacementValidation(t *testing.T) {
	te := setupMapsTest(t, true)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	siteID := cam.SiteID
	ctx := context.Background()

	// Insert a building and floor
	buildingID := uuid.New()
	floorID := uuid.New()
	err := te.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO map_buildings (id, tenant_id, site_id, name)
			VALUES ($1, $2, $3, 'Building 1')
		`, buildingID, te.Demo.TenantID, siteID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO map_floors (id, tenant_id, building_id, name, ordinal)
			VALUES ($1, $2, $3, 'Floor 1', 1)
		`, floorID, te.Demo.TenantID, buildingID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Missing x/y for floor placement
	status, _, body := te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, nil, gen.MapUpsertPlacementRequest{
		SiteId:  siteID,
		FloorId: &floorID,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing floor x/y, got %d: %s", status, body)
	}

	// 2. Out of range x (> 1)
	badX := float32(1.5)
	goodY := float32(0.5)
	status, _, body = te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, nil, gen.MapUpsertPlacementRequest{
		SiteId:  siteID,
		FloorId: &floorID,
		X:       &badX,
		Y:       &goodY,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for x > 1, got %d: %s", status, body)
	}

	// 3. Providing lat/lng with floor_id is forbidden
	lat := -34.6037
	goodX := float32(0.25)
	status, _, body = te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, nil, gen.MapUpsertPlacementRequest{
		SiteId:  siteID,
		FloorId: &floorID,
		X:       &goodX,
		Y:       &goodY,
		Lat:     &lat,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for lat provided in floor placement, got %d: %s", status, body)
	}

	// 4. Valid floor placement
	status, _, body = te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, nil, gen.MapUpsertPlacementRequest{
		SiteId:  siteID,
		FloorId: &floorID,
		X:       &goodX,
		Y:       &goodY,
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200 for floor placement, got %d: %s", status, body)
	}
	var placement gen.MapPlacement
	if err := json.Unmarshal(body, &placement); err != nil {
		t.Fatal(err)
	}
	if placement.FloorId == nil || *placement.FloorId != floorID || *placement.X != goodX || *placement.Y != goodY {
		t.Fatalf("unexpected floor placement values: %+v", placement)
	}
}

func TestMapsSiteGeoUpdateAndAudit(t *testing.T) {
	te := setupMapsTest(t, true)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	siteID := cam.SiteID
	ctx := context.Background()

	// 1. Validation failure: lat out of range
	badLat := 120.0
	status, _, body := te.request(http.MethodPatch, fmt.Sprintf("/api/v1/sites/%s/geo", siteID), te.AdminToken, nil, gen.UpdateSiteGeoRequest{
		Lat: &badLat,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad lat, got %d: %s", status, body)
	}

	// 2. Validation failure: zoom out of range
	badZoom := float32(30.0)
	status, _, body = te.request(http.MethodPatch, fmt.Sprintf("/api/v1/sites/%s/geo", siteID), te.AdminToken, nil, gen.UpdateSiteGeoRequest{
		DefaultZoom: &badZoom,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad zoom, got %d: %s", status, body)
	}

	// 3. Successful site geo update
	newLat := -34.603722
	newLng := -58.381592
	newZoom := float32(16.5)
	status, _, body = te.request(http.MethodPatch, fmt.Sprintf("/api/v1/sites/%s/geo", siteID), te.AdminToken, nil, gen.UpdateSiteGeoRequest{
		Lat:         &newLat,
		Lng:         &newLng,
		DefaultZoom: &newZoom,
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200 for site geo update, got %d: %s", status, body)
	}
	var siteGeo gen.SiteGeo
	if err := json.Unmarshal(body, &siteGeo); err != nil {
		t.Fatal(err)
	}
	if siteGeo.Id != siteID || *siteGeo.Lat != newLat || *siteGeo.Lng != newLng || *siteGeo.DefaultZoom != newZoom {
		t.Fatalf("unexpected site geo: %+v", siteGeo)
	}

	// Also verify via overview endpoint that site has the updated coordinates
	status, _, body = te.request(http.MethodGet, "/api/v1/maps/overview", te.AdminToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("overview status: %d %s", status, body)
	}
	var overview gen.MapSiteOverviewList
	_ = json.Unmarshal(body, &overview)
	found := false
	for _, s := range overview.Items {
		if s.Id == siteID {
			if s.Lat == nil || *s.Lat != newLat || s.Lng == nil || *s.Lng != newLng {
				t.Fatalf("overview lat/lng mismatch: lat=%v lng=%v", s.Lat, s.Lng)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("site %s not found in overview", siteID)
	}

	// 4. Verify audit log entry for site geo update
	var auditCount int
	err := te.Pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_log
		WHERE action = 'maps.site.geo_update' AND target_type = 'site' AND target_id = $1
	`, siteID).Scan(&auditCount)
	if err != nil {
		t.Fatal(err)
	}
	if auditCount < 1 {
		t.Fatalf("expected at least 1 audit entry for maps.site.geo_update, got %d", auditCount)
	}
}
