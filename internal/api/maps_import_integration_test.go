//go:build integration

package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

type importReport struct {
	DryRun   bool `json:"dry_run"`
	Rows     int  `json:"rows"`
	Upserted int  `json:"upserted"`
	Errors   []struct {
		Line    int    `json:"line"`
		Message string `json:"message"`
	} `json:"errors"`
}

func (te *testMapsEnv) importPlacements(t *testing.T, siteID any, csv string, dryRun bool, token string) (int, importReport, []byte) {
	t.Helper()
	status, _, raw := te.request(http.MethodPost, "/api/v1/maps/placements/import", token, nil, map[string]any{
		"site_id": siteID,
		"csv":     csv,
		"dry_run": dryRun,
	})
	var report importReport
	if status == http.StatusOK {
		if err := json.Unmarshal(raw, &report); err != nil {
			t.Fatalf("import report does not parse: %v: %s", err, raw)
		}
	}
	return status, report, raw
}

func (te *testMapsEnv) cameraUnplaced(t *testing.T, siteID, cameraID any) bool {
	t.Helper()
	status, _, raw := te.request(http.MethodGet, fmt.Sprintf("/api/v1/maps/unplaced?site_id=%s", siteID), te.AdminToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("unplaced list: %d %s", status, raw)
	}
	var list struct {
		Cameras []struct {
			ID string `json:"id"`
		} `json:"cameras"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	for _, c := range list.Cameras {
		if c.ID == fmt.Sprint(cameraID) {
			return true
		}
	}
	return false
}

func TestMapPlacementsImportFeatureFlagDisabled(t *testing.T) {
	te := setupMapsTest(t, false)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	status, _, _ := te.importPlacements(t, cam.SiteID, "camera,lat,lng\n", false, te.AdminToken)
	if status != http.StatusNotFound {
		t.Fatalf("import: expected 404 when maps flag disabled, got %d", status)
	}
}

func TestMapPlacementsImportDryRunAndApply(t *testing.T) {
	te := setupMapsTest(t, true)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	siteID := cam.SiteID

	// 1. Dry run by camera UUID: reports the row it would write, writes nothing.
	status, report, raw := te.importPlacements(t, siteID,
		fmt.Sprintf("camera,lat,lng\n%s,-34.6037,-58.3816\n", cam.ID), true, te.AdminToken)
	if status != http.StatusOK {
		t.Fatalf("dry run: expected 200, got %d: %s", status, raw)
	}
	if !report.DryRun || report.Rows != 1 || report.Upserted != 0 || len(report.Errors) != 0 {
		t.Fatalf("unexpected dry run report: %+v", report)
	}
	if !te.cameraUnplaced(t, siteID, cam.ID) {
		t.Fatal("dry run must not create a placement")
	}

	// 2. Dry run reports every bad row with its line: unknown camera and out-of-range lat.
	badCSV := fmt.Sprintf("camera,lat,lng\nno-existe,-34.6,-58.4\n%s,95,-58.4\n", cam.RemoteName)
	status, report, raw = te.importPlacements(t, siteID, badCSV, true, te.AdminToken)
	if status != http.StatusOK {
		t.Fatalf("bad dry run: expected 200, got %d: %s", status, raw)
	}
	if report.Rows != 2 || report.Upserted != 0 || len(report.Errors) != 2 {
		t.Fatalf("expected 2 rows and 2 errors, got %+v", report)
	}
	if report.Errors[0].Line != 2 || report.Errors[1].Line != 3 {
		t.Fatalf("errors must point at lines 2 and 3, got %+v", report.Errors)
	}

	// 3. Apply is atomic: any invalid row writes nothing at all.
	status, report, raw = te.importPlacements(t, siteID, badCSV, false, te.AdminToken)
	if status != http.StatusOK {
		t.Fatalf("bad apply: expected 200, got %d: %s", status, raw)
	}
	if report.DryRun || report.Upserted != 0 || len(report.Errors) != 2 {
		t.Fatalf("atomic apply must write nothing, got %+v", report)
	}
	if !te.cameraUnplaced(t, siteID, cam.ID) {
		t.Fatal("apply with invalid rows must not create a placement")
	}

	// 4. Apply of a valid row matches by display name and upserts the placement.
	status, report, raw = te.importPlacements(t, siteID,
		fmt.Sprintf("camera,lat,lng,bearing,fov,range\n%s,-34.6037,-58.3816,180,90,50\n", cam.DisplayName),
		false, te.AdminToken)
	if status != http.StatusOK {
		t.Fatalf("apply: expected 200, got %d: %s", status, raw)
	}
	if report.DryRun || report.Rows != 1 || report.Upserted != 1 || len(report.Errors) != 0 {
		t.Fatalf("unexpected apply report: %+v", report)
	}
	if te.cameraUnplaced(t, siteID, cam.ID) {
		t.Fatal("camera must leave the unplaced list after a successful import")
	}

	// 5. RBAC: a token without maps.edit_device is refused, dry run included.
	status, _, raw = te.importPlacements(t, siteID,
		fmt.Sprintf("camera,lat,lng\n%s,-34.6037,-58.3816\n", cam.RemoteName),
		true, te.Demo.Tokens["supervisor"])
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 for a token without maps.edit_device, got %d: %s", status, raw)
	}
}
