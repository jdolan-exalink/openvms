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
	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

func TestMapsHierarchyPrivatePlanLifecycle(t *testing.T) {
	te := setupMapsTest(t, true)
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
	status, _, body = te.request(http.MethodDelete, fmt.Sprintf("/api/v1/maps/sites/%s/floors/%s", cam.SiteID, f.ID), te.AdminToken, map[string]string{"If-Match": fmt.Sprint(f.Revision)}, nil)
	if status != 400 {
		t.Fatalf("occupied floor deletion must preserve layout: %d %s", status, body)
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
