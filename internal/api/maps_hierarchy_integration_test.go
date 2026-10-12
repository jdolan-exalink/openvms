//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
)

func TestMapsHierarchyPrivatePlanLifecycle(t *testing.T) {
	te := setupMapsTest(t, true)
	ctx := context.Background()
	cam := te.Cameras["frigate-h01/acceso_norte"]
	status, _, body := te.request(http.MethodPost, fmt.Sprintf("/api/v1/maps/sites/%s/buildings", cam.SiteID), te.AdminToken, nil, map[string]any{"name": "Plant A"})
	if status != 201 {
		t.Fatalf("create building: %d %s", status, body)
	}
	var b struct {
		ID       uuid.UUID `json:"id"`
		Revision int64     `json:"revision"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatal(err)
	}
	status, _, body = te.request(http.MethodPost, fmt.Sprintf("/api/v1/maps/sites/%s/buildings/%s/floors", cam.SiteID, b.ID), te.AdminToken, nil, map[string]any{"name": "Level 1", "ordinal": 1})
	if status != 201 {
		t.Fatalf("create floor: %d %s", status, body)
	}
	var f struct {
		ID       uuid.UUID `json:"id"`
		Revision int64     `json:"revision"`
	}
	if err := json.Unmarshal(body, &f); err != nil {
		t.Fatal(err)
	}
	// A geographic placement must not make this camera placed on an unrelated floor.
	lat, lng := -31.1, -60.1
	status, _, body = te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, nil, gen.MapUpsertPlacementRequest{SiteId: cam.SiteID, Lat: &lat, Lng: &lng})
	if status != 200 {
		t.Fatalf("geo: %d %s", status, body)
	}
	status, _, body = te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/unplaced?site_id=%s&floor_id=%s", cam.SiteID, f.ID), te.AdminToken, nil, nil)
	if status != 200 || !bytes.Contains(body, []byte(cam.ID.String())) {
		t.Fatalf("floor unplaced must include geographically placed camera: %d %s", status, body)
	}
	var p bytes.Buffer
	_ = png.Encode(&p, image.NewRGBA(image.Rect(0, 0, 4, 3)))
	path := fmt.Sprintf("/api/v1/maps/sites/%s/floors/%s/plan", cam.SiteID, f.ID)
	req, _ := http.NewRequest(http.MethodPut, te.server.URL+path, bytes.NewReader(append(p.Bytes(), []byte("<script>bad</script>")...)))
	req.Header.Set("Authorization", "Bearer "+te.AdminToken)
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("If-Match", fmt.Sprint(f.Revision))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	uploadBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err := json.Unmarshal(uploadBody, &f); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("upload: %d", resp.StatusCode)
	}
	status, headers, body := te.request(http.MethodGet, path, te.AdminToken, nil, nil)
	if status != 200 || headers.Get("Content-Type") != "image/png" || headers.Get("X-Content-Type-Options") != "nosniff" || headers.Get("Cache-Control") != "private, no-store" || !bytes.Equal(body, p.Bytes()) {
		t.Fatalf("private canonical PNG: %d %v %s", status, headers, body)
	}
	status, _, _ = te.request(http.MethodGet, path, "", nil, nil)
	if status != 401 {
		t.Fatalf("unauthenticated image: %d", status)
	}
	status, _, _ = te.request(http.MethodGet, path, te.Demo.Tokens["operador"], nil, nil)
	if status != 403 {
		t.Fatalf("unauthorized image: %d", status)
	}
	// An uncertain blob write must not change the floor, and its unique object is cleaned up.
	blobs := te.mapsSvc.Blobs.(*mapPlanBlobs)
	blobs.mu.Lock()
	blobs.failPut = true
	blobs.mu.Unlock()
	req, _ = http.NewRequest(http.MethodPut, te.server.URL+path, bytes.NewReader(p.Bytes()))
	req.Header.Set("Authorization", "Bearer "+te.AdminToken)
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("If-Match", fmt.Sprint(f.Revision))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Fatalf("failed storage must fail upload: %d", resp.StatusCode)
	}
	blobs.mu.Lock()
	blobs.failPut = false
	blobs.mu.Unlock()
	blobs.mu.Lock()
	count := len(blobs.data)
	blobs.mu.Unlock()
	if count != 1 {
		t.Fatalf("lost upload leaked object: %d", count)
	}
	status, _, body = te.request(http.MethodGet, path, te.AdminToken, nil, nil)
	if status != 200 || !bytes.Equal(body, p.Bytes()) {
		t.Fatal("failed replacement changed prior plan")
	}
	// The camera can exist on two floors while its geographic position is preserved.
	status, _, body = te.request(http.MethodPost, fmt.Sprintf("/api/v1/maps/sites/%s/buildings/%s/floors", cam.SiteID, b.ID), te.AdminToken, nil, map[string]any{"name": "Level 2", "ordinal": 2})
	if status != 201 {
		t.Fatalf("second floor: %d %s", status, body)
	}
	var second struct {
		ID       uuid.UUID `json:"id"`
		Revision int64     `json:"revision"`
	}
	_ = json.Unmarshal(body, &second)
	x, y := float32(.3), float32(.7)
	for _, floorID := range []uuid.UUID{f.ID, second.ID} {
		status, _, body = te.request(http.MethodPut, fmt.Sprintf("/api/v1/maps/placements/camera/%s", cam.ID), te.AdminToken, nil, gen.MapUpsertPlacementRequest{SiteId: cam.SiteID, FloorId: &floorID, X: &x, Y: &y})
		if status != 200 {
			t.Fatalf("floor placement: %d %s", status, body)
		}
	}
	status, _, body = te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/entities", cam.SiteID), te.AdminToken, nil, nil)
	var entities gen.MapEntitiesResponse
	_ = json.Unmarshal(body, &entities)
	if status != 200 || len(entities.Entities) != 3 {
		t.Fatalf("geo plus two floor placements: %d %s", status, body)
	}
	createFloorZone := func(floorID uuid.UUID, name string) uuid.UUID {
		t.Helper()
		code, _, response := te.request(http.MethodPost, fmt.Sprintf("/api/v1/maps/sites/%s/zones", cam.SiteID), te.AdminToken, nil, gen.CreateMapZoneRequest{
			FloorId: &floorID,
			Name:    name,
			Kind:    gen.CreateMapZoneRequestKindWarning,
			Geometry: map[string]interface{}{
				"type": "Polygon",
				"coordinates": [][][]float64{
					{{0.1, 0.1}, {0.8, 0.1}, {0.8, 0.9}, {0.1, 0.9}, {0.1, 0.1}},
				},
			},
		})
		if code != http.StatusCreated {
			t.Fatalf("create %s zone: %d %s", name, code, response)
		}
		var zone gen.MapZone
		if err := json.Unmarshal(response, &zone); err != nil {
			t.Fatal(err)
		}
		return zone.Id
	}
	deletedZoneID := createFloorZone(f.ID, "Zone on deleted floor")
	keptZoneID := createFloorZone(second.ID, "Zone on retained floor")

	// A fully authorized user in another tenant cannot delete this floor or its contents.
	otherTenant, err := te.Svc.CreateTenant(ctx, te.Admin, "floor-isolation", "Floor Isolation")
	if err != nil {
		t.Fatal(err)
	}
	otherSiteName := "Other tenant site"
	otherSite, err := te.Svc.CreateSite(ctx, te.Admin, otherTenant.ID, inventory.SiteInput{Name: &otherSiteName})
	if err != nil {
		t.Fatal(err)
	}
	otherActor, otherToken, err := bootstrap.TenantUser(ctx, te.Store, otherTenant.ID, "floor-owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := te.Svc.CreateGrant(ctx, te.Admin, inventory.GrantInput{
		SubjectType: "user", SubjectID: otherActor.UserID, Permission: authz.MapsEdit,
		Effect: authz.Allow, ScopeType: authz.ScopeSite, ScopeID: &otherSite.ID,
	}); err != nil {
		t.Fatal(err)
	}
	status, _, body = te.request(http.MethodPost, fmt.Sprintf("/api/v1/maps/sites/%s/buildings", otherSite.ID), otherToken, nil, map[string]any{"name": "Other building"})
	if status != http.StatusCreated {
		t.Fatalf("other tenant's valid maps.edit grant: %d %s", status, body)
	}
	var otherBuilding struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(body, &otherBuilding); err != nil {
		t.Fatal(err)
	}
	status, _, body = te.request(http.MethodPost, fmt.Sprintf("/api/v1/maps/sites/%s/buildings/%s/floors", otherSite.ID, otherBuilding.ID), otherToken, nil, map[string]any{"name": "Other floor", "ordinal": 1})
	if status != http.StatusCreated {
		t.Fatalf("other tenant's floor: %d %s", status, body)
	}
	var otherFloor struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(body, &otherFloor); err != nil {
		t.Fatal(err)
	}
	deletePath := fmt.Sprintf("/api/v1/maps/sites/%s/floors/%s", cam.SiteID, f.ID)
	status, _, body = te.request(http.MethodDelete, deletePath, otherToken, map[string]string{"If-Match": fmt.Sprint(f.Revision)}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("other tenant must not delete floor: %d %s", status, body)
	}

	status, _, body = te.request(http.MethodDelete, deletePath, te.AdminToken, map[string]string{"If-Match": fmt.Sprint(f.Revision)}, nil)
	if status != http.StatusNoContent {
		t.Fatalf("occupied floor deletion must cascade: %d %s", status, body)
	}
	status, _, body = te.request(http.MethodGet, path, te.AdminToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("deleted floor plan remains readable: %d %s", status, body)
	}
	blobs.mu.Lock()
	remainingBlobs := len(blobs.data)
	blobs.mu.Unlock()
	if remainingBlobs != 0 {
		t.Fatalf("deleted floor leaked private plan blobs: %d", remainingBlobs)
	}
	status, _, body = te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/sites/%s/entities", cam.SiteID), te.AdminToken, nil, nil)
	if err := json.Unmarshal(body, &entities); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || len(entities.Entities) != 2 {
		t.Fatalf("geo and other-floor placements must survive deletion: %d %s", status, body)
	}
	var geo, retainedFloor bool
	for _, entity := range entities.Entities {
		if entity.Id != cam.ID || entity.Pos.FloorId == nil && entity.Pos.K != "geo" {
			t.Fatalf("unexpected entity after floor deletion: %+v", entity)
		}
		if entity.Pos.FloorId == nil {
			geo = true
		} else if *entity.Pos.FloorId == second.ID {
			retainedFloor = true
		}
	}
	if !geo || !retainedFloor {
		t.Fatalf("surviving placements: geo=%t other-floor=%t", geo, retainedFloor)
	}
	var deletedPlacements, retainedPlacements, deletedZoneActive, retainedZoneActive, deletedFloorLive, otherFloorLive int
	if err := te.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM map_placements WHERE floor_id = $1),
			(SELECT count(*) FROM map_placements WHERE floor_id = $2),
			(SELECT count(*) FROM map_zones WHERE id = $3 AND deleted_at IS NULL),
			(SELECT count(*) FROM map_zones WHERE id = $4 AND deleted_at IS NULL),
			(SELECT count(*) FROM map_floors WHERE id = $1 AND deleted_at IS NULL),
			(SELECT count(*) FROM map_floors WHERE id = $5 AND deleted_at IS NULL)`,
			f.ID, second.ID, deletedZoneID, keptZoneID, otherFloor.ID,
		).Scan(&deletedPlacements, &retainedPlacements, &deletedZoneActive, &retainedZoneActive, &deletedFloorLive, &otherFloorLive)
	}); err != nil {
		t.Fatal(err)
	}
	if deletedPlacements != 0 || retainedPlacements != 1 || deletedZoneActive != 0 || retainedZoneActive != 1 || deletedFloorLive != 0 || otherFloorLive != 1 {
		t.Fatalf("cascade/isolation counts: deleted placements=%d retained placements=%d deleted zone=%d retained zone=%d deleted floor=%d other tenant floor=%d", deletedPlacements, retainedPlacements, deletedZoneActive, retainedZoneActive, deletedFloorLive, otherFloorLive)
	}
	status, _, _ = te.request(http.MethodDelete, fmt.Sprintf("/api/v1/maps/sites/%s/buildings/%s", cam.SiteID, b.ID), te.AdminToken, map[string]string{"If-Match": fmt.Sprint(b.Revision)}, nil)
	if status != 400 {
		t.Fatalf("nonempty building deletion: %d", status)
	}

	status, _, body = te.request(http.MethodPatch, fmt.Sprintf("/api/v1/maps/sites/%s/buildings/%s", cam.SiteID, b.ID), te.AdminToken, map[string]string{"If-Match": fmt.Sprint(b.Revision)}, map[string]any{"name": "Plant renamed"})
	if status != 200 {
		t.Fatalf("rename: %d %s", status, body)
	}
	status, _, _ = te.request(http.MethodPatch, fmt.Sprintf("/api/v1/maps/sites/%s/buildings/%s", cam.SiteID, b.ID), te.AdminToken, map[string]string{"If-Match": fmt.Sprint(b.Revision)}, map[string]any{"name": "Stale"})
	if status != 409 {
		t.Fatalf("stale update: %d", status)
	}
}

// Add deletion to the existing in-memory private storage fake without changing branding's API.
type mapPlanBlobs struct {
	*memBlobs
	failPut bool
}

func (b *mapPlanBlobs) Delete(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.data, key)
	delete(b.ct, key)
	return nil
}

func (b *mapPlanBlobs) Put(ctx context.Context, key string, data []byte, ct string) error {
	if err := b.memBlobs.Put(ctx, key, data, ct); err != nil {
		return err
	}
	b.mu.Lock()
	fail := b.failPut
	b.mu.Unlock()
	if fail {
		return errors.New("simulated uncertain private storage write")
	}
	return nil
}
