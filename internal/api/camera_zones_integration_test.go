//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/store"
)

func TestCameraZonesPersistenceAndIsolation(t *testing.T) {
	te := setupMapsTest(t, false)
	ctx := context.Background()
	cam := te.Cameras["frigate-h01/acceso_norte"]
	path := "/api/v1/cameras/" + cam.ID.String() + "/zones"
	input := gen.CameraZoneInput{
		Name:   " entry ",
		Points: []gen.CameraZonePoint{{X: 0.12349, Y: 0.1}, {X: 0.9, Y: 0.1}, {X: 0.5, Y: 0.9}},
	}
	settings := map[string]interface{}{"objects": []string{"person"}, "loitering_time": float64(2)}
	input.Settings = &settings
	if status, _, body := te.request(http.MethodPost, path, te.Demo.Tokens["operador"], nil, input); status != http.StatusForbidden {
		t.Fatalf("operator create status = %d, want %d: %s", status, http.StatusForbidden, body)
	}

	status, _, body := te.request(http.MethodPost, path, te.AdminToken, nil, input)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s", status, http.StatusCreated, body)
	}
	var created gen.CameraZone
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	if created.Id == nil || created.CameraId == nil || *created.CameraId != cam.ID {
		t.Fatalf("created identity = %+v", created)
	}
	if created.Name != "entry" || len(created.Points) != 3 || created.Points[0].X != 0.123 {
		t.Fatalf("zone was not normalized: %+v", created)
	}
	if created.Settings["loitering_time"] != float64(2) {
		t.Fatalf("settings not preserved: %#v", created.Settings)
	}
	err := te.Store.TxRaw(ctx, store.TenantScope{TenantID: cam.TenantID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO camera_zones (tenant_id, camera_id, name, points)
VALUES ($1, $2, 'outside_frame', '[{"x":-0.1,"y":0.1},{"x":0.9,"y":0.1},{"x":0.5,"y":0.9}]'::jsonb)`, cam.TenantID, cam.ID)
		return err
	})
	if err == nil {
		t.Fatal("database accepted points outside the normalized frame")
	}
	for _, test := range []struct {
		name   string
		points string
	}{
		{name: "missing_x", points: `[{"y":0.1},{"x":0.9,"y":0.1},{"x":0.5,"y":0.9}]`},
		{name: "missing_y", points: `[{"x":0.1},{"x":0.9,"y":0.1},{"x":0.5,"y":0.9}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := te.Store.TxRaw(ctx, store.TenantScope{TenantID: cam.TenantID}, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `INSERT INTO camera_zones (tenant_id, camera_id, name, points)
VALUES ($1, $2, $3, $4::jsonb)`, cam.TenantID, cam.ID, "missing_coordinate_"+test.name, test.points)
				return err
			})
			if err == nil {
				t.Fatalf("database accepted points with %s", test.name)
			}
		})
	}
	if status, _, body := te.request(http.MethodPost, path, te.AdminToken, nil, input); status != http.StatusConflict {
		t.Fatalf("duplicate zone name status = %d, want %d: %s", status, http.StatusConflict, body)
	}

	zonePath := fmt.Sprintf("%s/%s", path, created.Id.String())
	status, _, body = te.request(http.MethodGet, path, te.AdminToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list status = %d, want %d: %s", status, http.StatusOK, body)
	}
	var listed gen.CameraZoneList
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Zones) != 1 || listed.Zones[0].Id == nil || *listed.Zones[0].Id != *created.Id {
		t.Fatalf("list lost stable zone ID: %+v", listed.Zones)
	}

	input.Name = "entry_updated"
	input.Points[0].X = 0.2
	status, _, body = te.request(http.MethodPut, zonePath, te.AdminToken, nil, input)
	if status != http.StatusOK {
		t.Fatalf("update status = %d, want %d: %s", status, http.StatusOK, body)
	}
	var updated gen.CameraZone
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Id == nil || *updated.Id != *created.Id || updated.Name != input.Name {
		t.Fatalf("update changed stable ID or failed to persist fields: %+v", updated)
	}

	// A zone ID cannot be used through another camera's resource path.
	other := te.Cameras["frigate-c01/ruta_1"]
	status, _, _ = te.request(http.MethodPut, "/api/v1/cameras/"+other.ID.String()+"/zones/"+created.Id.String(), te.AdminToken, nil, input)
	if status != http.StatusNotFound {
		t.Fatalf("cross-camera update status = %d, want %d", status, http.StatusNotFound)
	}

	otherTenant, err := te.Svc.CreateTenant(ctx, te.Admin, "camera-zones-isolation", "Camera Zones Isolation")
	if err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := te.Store.TxRaw(ctx, store.TenantScope{TenantID: otherTenant.ID}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM camera_zones WHERE id = $1`, *created.Id).Scan(&visible)
	}); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("other tenant saw %d camera zones, want 0", visible)
	}

	status, _, _ = te.request(http.MethodDelete, zonePath, te.AdminToken, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d", status, http.StatusNoContent)
	}
}
