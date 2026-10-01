//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/maps"
	"github.com/jdolan-exalink/openvms/internal/platform/config"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type testMapsEnv struct {
	*demofix.Env
	server  *httptest.Server
	mapsSvc *maps.Service
}

func setupMapsTest(t *testing.T, enableMaps bool) *testMapsEnv {
	t.Helper()
	env := demofix.Setup(t)
	mapsSvc := &maps.Service{
		Store:  env.Store,
		Config: maps.DefaultConfig(),
		Log:    pgtest.Discard(),
	}
	handlers := &api.Handlers{
		Inv:      env.Svc,
		Maps:     mapsSvc,
		Features: config.Features{Maps: enableMaps},
		Log:      pgtest.Discard(),
	}
	router, err := api.NewRouter(handlers, pgtest.Discard(), api.Options{
		Queries: db.New(env.Pool),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	return &testMapsEnv{
		Env:     env,
		server:  ts,
		mapsSvc: mapsSvc,
	}
}

func (te *testMapsEnv) request(method, path, token string, headers map[string]string, body any) (int, http.Header, []byte) {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}
	req, _ := http.NewRequest(method, te.server.URL+path, bodyReader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, respBody
}

func TestMapsFeatureFlagDisabled(t *testing.T) {
	te := setupMapsTest(t, false)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	siteID := cam.SiteID

	// When maps feature is off, endpoints return 404
	status, _, _ := te.request(http.MethodGet, "/api/v1/maps/config", te.AdminToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 when maps flag disabled, got %d", status)
	}

	status, _, _ = te.request(http.MethodGet, "/api/v1/maps/overview", te.AdminToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 when maps flag disabled, got %d", status)
	}

	status, _, _ = te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s", siteID), te.AdminToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 when maps flag disabled, got %d", status)
	}

	status, _, _ = te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/entities", siteID), te.AdminToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 when maps flag disabled, got %d", status)
	}
}

func TestMapsReadEndpointsAndRBAC(t *testing.T) {
	te := setupMapsTest(t, true)
	ctx := context.Background()
	tenantID := te.Demo.TenantID

	cam := te.Cameras["frigate-h01/acceso_norte"]
	hiddenCam := te.Cameras["frigate-h01/plaza"]
	siteID := cam.SiteID
	serverID := cam.ServerID

	// Seed site geo coords and region
	regionID := uuid.New()
	buildingID := uuid.New()
	floorID := uuid.New()
	zoneID := uuid.New()
	deviceID := uuid.New()

	err := te.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO map_regions (id, tenant_id, name)
			VALUES ($1, $2, 'Cordoba Central')
		`, regionID, tenantID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			UPDATE sites
			SET lat = -31.4201, lng = -64.1888, default_zoom = 16.5, region_id = $1
			WHERE id = $2
		`, regionID, siteID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO map_buildings (id, tenant_id, site_id, name, lat, lng)
			VALUES ($1, $2, $3, 'Edificio Central', -31.4201, -64.1888)
		`, buildingID, tenantID, siteID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO map_floors (id, tenant_id, building_id, name, ordinal, plan_width_px, plan_height_px)
			VALUES ($1, $2, $3, 'Planta Baja', 0, 1920, 1080)
		`, floorID, tenantID, buildingID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO map_zones (id, tenant_id, site_id, name, kind, geometry)
			VALUES ($1, $2, $3, 'Perimetro Norte', 'perimeter', '{"type":"Polygon","coordinates":[[[-64.189,-31.420],[-64.188,-31.420],[-64.188,-31.421],[-64.189,-31.421],[-64.189,-31.420]]]}'::jsonb)
		`, zoneID, tenantID, siteID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO map_devices (id, tenant_id, site_id, kind, name, status)
			VALUES ($1, $2, $3, 'sensor', 'Sensor Puerta Principal', 'online')
		`, deviceID, tenantID, siteID)
		if err != nil {
			return err
		}

		// Place authorized camera on geo map
		_, err = tx.Exec(ctx, `
			INSERT INTO map_placements (
				tenant_id, site_id, entity_type, entity_id, floor_id,
				lat, lng, bearing_deg, fov_deg, range_m, props, revision
			) VALUES (
				$1, $2, 'camera', $3, NULL,
				-31.4202, -64.1887, 135, 90, 45, '{"camera_type":"dome","ptz":true}'::jsonb, 101
			)
		`, tenantID, siteID, cam.ID)
		if err != nil {
			return err
		}

		// Place hidden camera on geo map (operador does not have access to this one)
		_, err = tx.Exec(ctx, `
			INSERT INTO map_placements (
				tenant_id, site_id, entity_type, entity_id, floor_id,
				lat, lng, bearing_deg, fov_deg, range_m, props, revision
			) VALUES (
				$1, $2, 'camera', $3, NULL,
				-31.4203, -64.1889, 45, 60, 30, '{}'::jsonb, 102
			)
		`, tenantID, siteID, hiddenCam.ID)
		if err != nil {
			return err
		}

		// Place server on geo map
		_, err = tx.Exec(ctx, `
			INSERT INTO map_placements (
				tenant_id, site_id, entity_type, entity_id, floor_id,
				lat, lng, bearing_deg, fov_deg, range_m, props, revision
			) VALUES (
				$1, $2, 'server', $3, NULL,
				-31.4205, -64.1890, 0, 70, 30, '{}'::jsonb, 103
			)
		`, tenantID, siteID, serverID)
		if err != nil {
			return err
		}

		// Place device on floor plan
		_, err = tx.Exec(ctx, `
			INSERT INTO map_placements (
				tenant_id, site_id, entity_type, entity_id, floor_id,
				x, y, bearing_deg, fov_deg, range_m, props, revision
			) VALUES (
				$1, $2, 'device', $3, $4,
				0.45, 0.60, 0, 70, 30, '{}'::jsonb, 104
			)
		`, tenantID, siteID, deviceID, floorID)
		return err
	})
	if err != nil {
		t.Fatalf("failed to seed test map data: %v", err)
	}

	// 1. GET /api/v1/maps/config
	t.Run("GetMapConfig", func(t *testing.T) {
		status, _, body := te.request(http.MethodGet, "/api/v1/maps/config", te.AdminToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", status, string(body))
		}
		var cfg gen.MapConfig
		if err := json.Unmarshal(body, &cfg); err != nil {
			t.Fatalf("failed to unmarshal config: %v", err)
		}
		if cfg.Provider.Id != "openfreemap" || cfg.Provider.Kind != gen.VectorStyle || cfg.Provider.Offline {
			t.Fatalf("unexpected provider config: %+v", cfg.Provider)
		}
		if cfg.Provider.StyleUrlLight == nil || *cfg.Provider.StyleUrlLight == "" {
			t.Fatal("expected a light vector style URL")
		}
	})

	// 2. GET /api/v1/maps/overview
	t.Run("GetMapOverview", func(t *testing.T) {
		status, _, body := te.request(http.MethodGet, "/api/v1/maps/overview", te.AdminToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", status, string(body))
		}
		var list gen.MapSiteOverviewList
		if err := json.Unmarshal(body, &list); err != nil {
			t.Fatalf("failed to unmarshal overview list: %v", err)
		}
		if len(list.Items) == 0 {
			t.Fatal("expected at least 1 site in overview")
		}
		var foundSite *gen.MapSiteOverview
		for _, s := range list.Items {
			if s.Id == siteID {
				foundSite = &s
				break
			}
		}
		if foundSite == nil {
			t.Fatalf("site ID %s not found in overview", siteID)
		}
		if foundSite.Lat == nil || *foundSite.Lat != -31.4201 {
			t.Fatalf("expected lat -31.4201, got %v", foundSite.Lat)
		}
		if foundSite.RegionName == nil || *foundSite.RegionName != "Cordoba Central" {
			t.Fatalf("expected region Cordoba Central, got %v", foundSite.RegionName)
		}
	})

	// 3. GET /api/v1/maps/sites/{siteId}
	t.Run("GetMapSiteDetails", func(t *testing.T) {
		status, _, body := te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s", siteID), te.AdminToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", status, string(body))
		}
		var details gen.MapSiteDetails
		if err := json.Unmarshal(body, &details); err != nil {
			t.Fatalf("failed to unmarshal site details: %v", err)
		}
		if details.Id != siteID {
			t.Fatalf("expected site ID %s, got %s", siteID, details.Id)
		}
		if len(details.Buildings) != 1 {
			t.Fatalf("expected 1 building, got %d", len(details.Buildings))
		}
		b := details.Buildings[0]
		if b.Name != "Edificio Central" || len(b.Floors) != 1 {
			t.Fatalf("unexpected building: %+v", b)
		}
		if b.Floors[0].Name != "Planta Baja" || b.Floors[0].Ordinal != 0 {
			t.Fatalf("unexpected floor: %+v", b.Floors[0])
		}
		if len(details.Zones) != 1 {
			t.Fatalf("expected 1 zone, got %d", len(details.Zones))
		}
		if details.Zones[0].Name != "Perimetro Norte" || details.Zones[0].Kind != gen.MapZoneKindPerimeter {
			t.Fatalf("unexpected zone: %+v", details.Zones[0])
		}
	})

	// 4. GET /api/v1/maps/sites/{siteId}/entities (with ETag & 304 handling)
	t.Run("GetMapSiteEntities", func(t *testing.T) {
		status, headers, body := te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/entities", siteID), te.AdminToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", status, string(body))
		}
		etag := headers.Get("ETag")
		if etag == "" {
			t.Fatal("expected ETag header in response")
		}

		var resp gen.MapEntitiesResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("failed to unmarshal entities response: %v", err)
		}
		if resp.Revision != 104 {
			t.Fatalf("expected revision 104, got %d", resp.Revision)
		}
		// Admin sees all 4 placed entities
		if len(resp.Entities) != 4 {
			t.Fatalf("expected 4 placed entities for admin, got %d", len(resp.Entities))
		}

		// Verify camera entity
		var camFound, srvFound, devFound bool
		for _, e := range resp.Entities {
			switch e.T {
			case gen.MapEntityTCamera:
				if e.Id == cam.ID {
					camFound = true
					if e.Pos.K != gen.Geo || e.Pos.Lat == nil || *e.Pos.Lat != -31.4202 {
						t.Fatalf("unexpected camera position: %+v", e.Pos)
					}
					if e.Cam == nil || e.Cam.Type != "dome" || !e.Cam.Ptz || e.Cam.Bearing != 135 {
						t.Fatalf("unexpected camera props: %+v", e.Cam)
					}
				}
			case gen.MapEntityTServer:
				srvFound = true
				if e.Pos.K != gen.Geo || e.Pos.Lat == nil || *e.Pos.Lat != -31.4205 {
					t.Fatalf("unexpected server position: %+v", e.Pos)
				}
			case gen.MapEntityTDevice:
				devFound = true
				if e.Pos.K != gen.Floor || e.Pos.FloorId == nil || *e.Pos.FloorId != floorID {
					t.Fatalf("unexpected device position: %+v", e.Pos)
				}
				if e.Pos.X == nil || *e.Pos.X != 0.45 {
					t.Fatalf("unexpected device X: %v", e.Pos.X)
				}
			}
		}
		if !camFound || !srvFound || !devFound {
			t.Fatalf("expected camera, server, device to be found, got: cam=%v, srv=%v, dev=%v", camFound, srvFound, devFound)
		}

		// Test 304 Not Modified when ETag matches
		status304, _, _ := te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/entities", siteID), te.AdminToken, map[string]string{
			"If-None-Match": etag,
		}, nil)
		if status304 != http.StatusNotModified {
			t.Fatalf("expected 304 Not Modified with matching ETag, got %d", status304)
		}

		// Test is_geo=true filter
		statusGeo, _, bodyGeo := te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/entities?is_geo=true", siteID), te.AdminToken, nil, nil)
		if statusGeo != http.StatusOK {
			t.Fatalf("expected 200 for geo filter, got %d", statusGeo)
		}
		var respGeo gen.MapEntitiesResponse
		_ = json.Unmarshal(bodyGeo, &respGeo)
		if len(respGeo.Entities) != 3 {
			t.Fatalf("expected 3 geo entities, got %d", len(respGeo.Entities))
		}

		// Test floor_id filter
		statusFloor, _, bodyFloor := te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/entities?floor_id=%s", siteID, floorID), te.AdminToken, nil, nil)
		if statusFloor != http.StatusOK {
			t.Fatalf("expected 200 for floor filter, got %d", statusFloor)
		}
		var respFloor gen.MapEntitiesResponse
		_ = json.Unmarshal(bodyFloor, &respFloor)
		if len(respFloor.Entities) != 1 || respFloor.Entities[0].T != gen.MapEntityTDevice {
			t.Fatalf("expected 1 floor entity (device), got %d", len(respFloor.Entities))
		}
	})

	// 5. RBAC Filtering:
	t.Run("RBAC_Filtering", func(t *testing.T) {
		operadorActor := te.Actor(t, "operador")
		operatorToken := te.Demo.Tokens["operador"]

		// operador has NO maps.view on the site -> 403 Forbidden on site map
		status, _, _ := te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s", siteID), operatorToken, nil, nil)
		if status != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for operator without maps.view, got %d", status)
		}

		// Grant maps.view on the site to operador
		_, err := te.Env.Svc.CreateGrant(ctx, te.Env.Admin, inventory.GrantInput{
			SubjectType: "user",
			SubjectID:   operadorActor.UserID,
			Permission:  authz.MapsView,
			ScopeType:   authz.ScopeSite,
			ScopeID:     &siteID,
			Effect:      authz.Allow,
		})
		if err != nil {
			t.Fatalf("failed to grant maps.view: %v", err)
		}

		// Now site details are visible
		status, _, _ = te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s", siteID), operatorToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200 OK after granting maps.view, got %d", status)
		}

		// When querying entities:
		// operador has cameras.view on "acceso_norte" (cam), but NOT on "plaza" (hiddenCam) and NOT servers.view.
		// Therefore:
		// - device: visible (site maps.view)
		// - cam (acceso_norte): visible (authorized camera)
		// - hiddenCam (plaza): HIDDEN (not authorized)
		// - server: HIDDEN (no servers.view)
		// Total visible: 2 entities!
		status, _, body := te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/entities", siteID), operatorToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", status)
		}
		var entitiesResp gen.MapEntitiesResponse
		_ = json.Unmarshal(body, &entitiesResp)
		if len(entitiesResp.Entities) != 2 {
			t.Fatalf("expected exactly 2 entities visible to operador (cam + device), got %d: %+v", len(entitiesResp.Entities), entitiesResp.Entities)
		}

		for _, e := range entitiesResp.Entities {
			if e.Id == hiddenCam.ID {
				t.Fatalf("unauthorized camera %s should have been filtered out", hiddenCam.ID)
			}
			if e.T == gen.MapEntityTServer {
				t.Fatalf("unauthorized server %s should have been filtered out", e.Id)
			}
		}

		// Grant servers.view on the site to operador
		_, err = te.Env.Svc.CreateGrant(ctx, te.Env.Admin, inventory.GrantInput{
			SubjectType: "user",
			SubjectID:   operadorActor.UserID,
			Permission:  authz.ServersView,
			ScopeType:   authz.ScopeSite,
			ScopeID:     &siteID,
			Effect:      authz.Allow,
		})
		if err != nil {
			t.Fatalf("failed to grant servers.view: %v", err)
		}

		// Now server is visible too (cam + device + server = 3 entities; hiddenCam still filtered!)
		status, _, body = te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/entities", siteID), operatorToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", status)
		}
		_ = json.Unmarshal(body, &entitiesResp)
		if len(entitiesResp.Entities) != 3 {
			t.Fatalf("expected 3 entities (cam + device + server), got %d", len(entitiesResp.Entities))
		}
		for _, e := range entitiesResp.Entities {
			if e.Id == hiddenCam.ID {
				t.Fatalf("unauthorized camera %s must still be filtered out", hiddenCam.ID)
			}
		}
	})
}

// TestMapEntitiesExposePlacementRevision proves the editor can read the per-placement
// revision it later sends back as If-Match (M-W8).
func TestMapEntitiesExposePlacementRevision(t *testing.T) {
	te := setupMapsTest(t, true)
	ctx := context.Background()
	tenantID := te.Demo.TenantID
	cam := te.Cameras["frigate-h01/acceso_norte"]
	siteID := cam.SiteID

	err := te.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO map_placements (
				tenant_id, site_id, entity_type, entity_id, floor_id,
				lat, lng, bearing_deg, fov_deg, range_m, props, revision
			) VALUES (
				$1, $2, 'camera', $3, NULL,
				-34.6037, -58.3816, 90, 70, 30, '{}'::jsonb, 7
			)
		`, tenantID, siteID, cam.ID)
		return err
	})
	if err != nil {
		t.Fatalf("failed to seed placement: %v", err)
	}

	status, _, body := te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/entities", siteID), te.AdminToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}

	var resp struct {
		Entities []struct {
			ID  uuid.UUID `json:"id"`
			Rev *int64    `json:"rev"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range resp.Entities {
		if e.ID != cam.ID {
			continue
		}
		found = true
		if e.Rev == nil {
			t.Fatal("expected placement revision (rev) on placed entity")
		}
		if *e.Rev != 7 {
			t.Fatalf("expected rev 7, got %d", *e.Rev)
		}
	}
	if !found {
		t.Fatalf("placed camera %s missing from entities", cam.ID)
	}
}
