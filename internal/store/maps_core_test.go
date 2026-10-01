//go:build integration

package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func TestMapsCoreMigrationAndQueries(t *testing.T) {
	ctx := context.Background()
	env := demofix.Setup(t)
	tenantID := env.Demo.TenantID
	userID := env.Admin.UserID
	sites, err := env.Svc.ListSites(ctx, env.Admin, nil)
	if err != nil || len(sites) == 0 {
		t.Fatalf("ListSites: %v", err)
	}
	siteID := sites[0].ID

	actor := authz.Actor{TenantID: &tenantID, UserID: userID}
	err = env.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		// 1. Regions
	parentRegion, err := q.CreateMapRegion(ctx, db.CreateMapRegionParams{
		TenantID: tenantID,
		ParentID: nil,
		Name:     "South America",
	})
	if err != nil {
		t.Fatalf("CreateMapRegion parent: %v", err)
	}

	childRegion, err := q.CreateMapRegion(ctx, db.CreateMapRegionParams{
		TenantID: tenantID,
		ParentID: &parentRegion.ID,
		Name:     "Argentina",
	})
	if err != nil {
		t.Fatalf("CreateMapRegion child: %v", err)
	}

	regions, err := q.ListMapRegions(ctx, tenantID)
	if err != nil || len(regions) < 2 {
		t.Fatalf("ListMapRegions = %v, %v; want at least 2", regions, err)
	}

	// 2. Site Geo
	lat := -34.6037
	lng := -58.3816
	zoom := float32(14.5)
	siteGeo, err := q.UpdateSiteGeo(ctx, db.UpdateSiteGeoParams{
		ID:          siteID,
		TenantID:    &tenantID,
		Lat:         &lat,
		Lng:         &lng,
		DefaultZoom: &zoom,
		RegionID:    &childRegion.ID,
	})
	if err != nil {
		t.Fatalf("UpdateSiteGeo: %v", err)
	}
	if siteGeo.Lat == nil || *siteGeo.Lat != lat || siteGeo.Lng == nil || *siteGeo.Lng != lng {
		t.Fatalf("UpdateSiteGeo coords mismatch: %+v", siteGeo)
	}

	// 3. Buildings & Floors
	footprint := json.RawMessage(`{"type":"Polygon","coordinates":[[[-58.38,-34.60],[-58.37,-34.60],[-58.37,-34.61],[-58.38,-34.61],[-58.38,-34.60]]]}`)
	building, err := q.CreateMapBuilding(ctx, db.CreateMapBuildingParams{
		TenantID:  tenantID,
		SiteID:    siteID,
		Name:      "Main Headquarters",
		Footprint: footprint,
		Lat:       &lat,
		Lng:       &lng,
	})
	if err != nil {
		t.Fatalf("CreateMapBuilding: %v", err)
	}

	floor, err := q.CreateMapFloor(ctx, db.CreateMapFloorParams{
		TenantID:         tenantID,
		BuildingID:       building.ID,
		Name:             "Ground Floor",
		Ordinal:          0,
		PlanKey:          "plans/hq-floor-0.png",
		PlanContentType:  "image/png",
		PlanWidthPx:      int32Ptr(1920),
		PlanHeightPx:     int32Ptr(1080),
		Georef:           nil,
	})
	if err != nil {
		t.Fatalf("CreateMapFloor: %v", err)
	}

	floors, err := q.ListMapFloorsByBuilding(ctx, db.ListMapFloorsByBuildingParams{
		BuildingID: building.ID,
		TenantID:   tenantID,
	})
	if err != nil || len(floors) != 1 {
		t.Fatalf("ListMapFloorsByBuilding = %v, %v; want 1", floors, err)
	}

	// 4. Devices
	dev, err := q.CreateMapDevice(ctx, db.CreateMapDeviceParams{
		TenantID: tenantID,
		SiteID:   siteID,
		Kind:     "door",
		Name:     "Front Access Gate",
		Status:   "online",
		Props:    json.RawMessage(`{"access_mode":"badge"}`),
	})
	if err != nil {
		t.Fatalf("CreateMapDevice: %v", err)
	}
	if dev.Kind != "door" {
		t.Fatalf("CreateMapDevice kind = %q, want door", dev.Kind)
	}

	// 5. Placements (Geo + Floor)
	var camID uuid.UUID
	for _, cam := range env.Cameras {
		if cam.SiteID == siteID {
			camID = cam.ID
			break
		}
	}
	if camID == uuid.Nil {
		t.Fatal("no camera found for demo site")
	}

	bearing := float32(45.0)
	fov := float32(90.0)
	rangeM := float32(25.0)
	geoPlacement, err := q.UpsertGeoPlacement(ctx, db.UpsertGeoPlacementParams{
		TenantID:   tenantID,
		SiteID:     siteID,
		EntityType: "camera",
		EntityID:   camID,
		Lat:        &lat,
		Lng:        &lng,
		BearingDeg: &bearing,
		FovDeg:     &fov,
		RangeM:     &rangeM,
		Props:      json.RawMessage(`{"camera_type":"dome"}`),
		UserID:     &userID,
	})
	if err != nil {
		t.Fatalf("UpsertGeoPlacement: %v", err)
	}
	if geoPlacement.Lat == nil || *geoPlacement.Lat != lat {
		t.Fatalf("geoPlacement lat = %v, want %v", geoPlacement.Lat, lat)
	}

	floorX := float32(0.45)
	floorY := float32(0.65)
	floorPlacement, err := q.UpsertFloorPlacement(ctx, db.UpsertFloorPlacementParams{
		TenantID:   tenantID,
		SiteID:     siteID,
		EntityType: "camera",
		EntityID:   camID,
		FloorID:    &floor.ID,
		X:          &floorX,
		Y:          &floorY,
		BearingDeg: &bearing,
		FovDeg:     &fov,
		RangeM:     &rangeM,
		Props:      json.RawMessage(`{"camera_type":"dome"}`),
		UserID:     &userID,
	})
	if err != nil {
		t.Fatalf("UpsertFloorPlacement: %v", err)
	}
	if floorPlacement.X == nil || *floorPlacement.X != floorX {
		t.Fatalf("floorPlacement x = %v, want %v", floorPlacement.X, floorX)
	}

	// 6. Zones
	zoneGeom := json.RawMessage(`{"type":"Polygon","coordinates":[[[-58.381,-34.603],[-58.380,-34.603],[-58.380,-34.604],[-58.381,-34.604],[-58.381,-34.603]]]}`)
	minLat, minLng, maxLat, maxLng := -34.604, -58.381, -34.603, -58.380
	zone, err := q.CreateMapZone(ctx, db.CreateMapZoneParams{
		TenantID: tenantID,
		SiteID:   siteID,
		FloorID:  nil,
		Name:     "Perimeter North",
		Kind:     "security",
		Geometry: zoneGeom,
		MinLat:   &minLat,
		MinLng:   &minLng,
		MaxLat:   &maxLat,
		MaxLng:   &maxLng,
		Style:    json.RawMessage(`{"color":"#ef4444"}`),
		Metadata: json.RawMessage(`{}`),
		UserID:   &userID,
	})
	if err != nil {
		t.Fatalf("CreateMapZone: %v", err)
	}
	if zone.Name != "Perimeter North" {
		t.Fatalf("zone name = %q, want Perimeter North", zone.Name)
	}

	// 7. User Preferences
	prefs, err := q.UpsertMapUserPrefs(ctx, db.UpsertMapUserPrefsParams{
		UserID:   userID,
		TenantID: &tenantID,
		Prefs:    json.RawMessage(`{"default_mode":"live","auto_focus":"center"}`),
	})
	if err != nil {
		t.Fatalf("UpsertMapUserPrefs: %v", err)
	}
	if len(prefs.Prefs) == 0 {
		t.Fatalf("UpsertMapUserPrefs empty prefs")
	}

	return nil
})
if err != nil {
	t.Fatalf("Store.Tx failed: %v", err)
}
}

func int32Ptr(v int32) *int32 {
	return &v
}
