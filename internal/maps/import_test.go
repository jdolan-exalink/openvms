package maps

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func importCameras() []siteCamera {
	return []siteCamera{
		{ID: uuid.New(), DisplayName: "Acceso Norte", RemoteName: "acceso_norte"},
		{ID: uuid.New(), DisplayName: "Acceso Sur", RemoteName: "acceso_sur"},
		{ID: uuid.New(), DisplayName: "Dup", RemoteName: "dup_a"},
		{ID: uuid.New(), DisplayName: "Dup", RemoteName: "dup_b"},
	}
}

func TestParsePlacementCSVAcceptsEveryCameraReference(t *testing.T) {
	cams := importCameras()
	byID := cams[0].ID.String()
	csv := "camera,lat,lng,bearing,fov,range\n" +
		byID + ",-34.6037,-58.3816,180,90,50\n" +
		"acceso sur,-34.61,-58.4,,,\n"

	rows, errs, total := parsePlacementCSV(csv, cams)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %+v", errs)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("expected 2 rows of 2, got rows=%d total=%d", len(rows), total)
	}
	if rows[0].CameraID != cams[0].ID || rows[1].CameraID != cams[1].ID {
		t.Fatalf("rows resolved to the wrong cameras: %+v", rows)
	}
	if rows[0].BearingDeg == nil || *rows[0].BearingDeg != 180 ||
		rows[0].FovDeg == nil || *rows[0].FovDeg != 90 ||
		rows[0].RangeM == nil || *rows[0].RangeM != 50 {
		t.Fatalf("optional columns not parsed: %+v", rows[0])
	}
	if rows[1].BearingDeg != nil || rows[1].FovDeg != nil || rows[1].RangeM != nil {
		t.Fatalf("empty optional columns must stay nil: %+v", rows[1])
	}
	if rows[0].Line != 2 || rows[1].Line != 3 {
		t.Fatalf("rows must carry their physical line, got %+v", rows)
	}
}

func TestParsePlacementCSVReportsOneErrorPerBadRow(t *testing.T) {
	cams := importCameras()
	csv := "camera,lat,lng,bearing,fov,range\n" +
		"no-existe,-34.6,-58.4,,,\n" + // unknown camera
		"Dup,-34.6,-58.4,,,\n" + // ambiguous display name
		"acceso_norte,abc,-58.4,,,\n" + // lat does not parse
		"acceso_norte,95,-58.4,,,\n" + // lat out of range
		"acceso_sur,-34.6,ar900,,,\n" + // lng does not parse
		"acceso_norte,-34.6,-58.4,400,,\n" + // bearing out of range
		"acceso_norte,-34.6,-58.4,,0,\n" + // fov out of range
		"acceso_norte,-34.6,-58.4,,,-1\n" // range negative

	rows, errs, total := parsePlacementCSV(csv, cams)
	if total != 8 {
		t.Fatalf("expected 8 data rows, got %d", total)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no valid rows, got %+v", rows)
	}
	want := []string{
		`cámara desconocida: "no-existe"`,
		`cámara ambigua: "Dup"`,
		`lat inválida: "abc"`,
		`lat fuera de rango: 95`,
		`lng inválida: "ar900"`,
		`bearing fuera de rango: 400`,
		`fov fuera de rango: 0`,
		`range fuera de rango: -1`,
	}
	if len(errs) != len(want) {
		t.Fatalf("expected %d errors, got %+v", len(want), errs)
	}
	for i, message := range want {
		if errs[i].Line != i+2 || errs[i].Message != message {
			t.Errorf("error %d: got line %d %q, want line %d %q",
				i, errs[i].Line, errs[i].Message, i+2, message)
		}
	}
}

func TestParsePlacementCSVRejectsDuplicateAndEmptyReferences(t *testing.T) {
	cams := importCameras()
	csv := "camera,lat,lng\n" +
		"acceso_norte,-34.6,-58.4\n" +
		"acceso_norte,-34.61,-58.41\n" +
		",-34.62,-58.42\n"

	rows, errs, total := parsePlacementCSV(csv, cams)
	if total != 3 || len(rows) != 1 {
		t.Fatalf("expected 1 valid row of 3, got rows=%d total=%d errs=%+v", len(rows), total, errs)
	}
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %+v", errs)
	}
	if errs[0].Line != 3 || errs[0].Message != `cámara repetida: "acceso_norte"` {
		t.Errorf("duplicate row: got line %d %q", errs[0].Line, errs[0].Message)
	}
	if errs[1].Line != 4 || errs[1].Message != "cámara vacía" {
		t.Errorf("empty row: got line %d %q", errs[1].Line, errs[1].Message)
	}
}

func TestParsePlacementCSVNeedsTheRequiredColumnsFirst(t *testing.T) {
	cams := importCameras()

	rows, errs, total := parsePlacementCSV("camera,lat\nacceso_norte,-34.6\n", cams)
	if rows != nil || total != 0 || len(errs) != 1 {
		t.Fatalf("missing lng must stop the parse, got rows=%v total=%d errs=%+v", rows, total, errs)
	}
	if errs[0].Line != 1 || !strings.Contains(errs[0].Message, "lng") {
		t.Fatalf("expected a header error naming lng, got %+v", errs[0])
	}

	rows, errs, total = parsePlacementCSV("", cams)
	if rows != nil || total != 0 || len(errs) != 1 || errs[0].Line != 1 {
		t.Fatalf("empty input must stop the parse, got rows=%v total=%d errs=%+v", rows, total, errs)
	}
}

func TestParsePlacementCSVIgnoresExcelBOMAndQuotedFields(t *testing.T) {
	cams := importCameras()
	csv := "\ufeffcamera,lat,lng\n\"Acceso Norte\",-34.6,-58.4\n"

	rows, errs, total := parsePlacementCSV(csv, cams)
	if len(errs) != 0 || total != 1 || len(rows) != 1 || rows[0].CameraID != cams[0].ID {
		t.Fatalf("BOM + quoted name must parse, got rows=%+v errs=%+v total=%d", rows, errs, total)
	}
}
