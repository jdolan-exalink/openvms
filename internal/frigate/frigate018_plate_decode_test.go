package frigate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestFrigate018RecognizedPlateDecoding pins the real Frigate 0.18 tracked-object shape
// (verified live against the user's server, see odd/tasks/m3-search.md M3-7 evidence):
// plates arrive on GET /api/events as data.recognized_license_plate (string) and
// data.recognized_license_plate_score (float), never on the top-level sub_label. It
// serves the two sanitized real fixtures through an httptest server shaped like
// Frigate 0.18 and decodes them through the real adapter path (Connect -> v018,
// embedding v017's TrackedObjects), so the full JSON-to-domain decode is exercised,
// not a hand-built struct.
func TestFrigate018RecognizedPlateDecoding(t *testing.T) {
	withPlate, err := os.ReadFile("testdata/frigate018_event_with_plate.json")
	if err != nil {
		t.Fatal(err)
	}
	noPlate, err := os.ReadFile("testdata/frigate018_event_no_plate.json")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0.18.0-77a66e7"))
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[" + string(withPlate) + "," + string(noPlate) + "]"))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	ctx := context.Background()
	a, err := Connect(ctx, ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name() != "v018" {
		t.Fatalf("want v018 adapter for a 0.18 server, got %s", a.Name())
	}

	objs, err := a.TrackedObjects(ctx, ObjectQuery{Cameras: []string{"portones"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 {
		t.Fatalf("want 2 tracked objects, got %d", len(objs))
	}

	withPlateObj, noPlateObj := objs[0], objs[1]

	if withPlateObj.Plate != "ABC123" {
		t.Errorf("plate: want %q, got %q", "ABC123", withPlateObj.Plate)
	}
	if withPlateObj.PlateScore == nil || *withPlateObj.PlateScore != 0.5702818274497986 {
		t.Errorf("plate score: want 0.5702818274497986, got %v", withPlateObj.PlateScore)
	}
	if withPlateObj.SubLabel != "" {
		t.Errorf("sub_label must stay empty; the plate never rides on it: got %q", withPlateObj.SubLabel)
	}

	if noPlateObj.Plate != "" {
		t.Errorf("event with no recognized plate yet must decode an empty plate, got %q", noPlateObj.Plate)
	}
	if noPlateObj.PlateScore != nil {
		t.Errorf("event with no recognized plate yet must decode a nil plate score, got %v", *noPlateObj.PlateScore)
	}
}

// TestObjectDataPlateTagIsLoadBearing proves TestFrigate018RecognizedPlateDecoding is not
// vacuous: unmarshaling the real "with plate" fixture through a deliberately mistagged
// copy of objectData (as if recognized_license_plate stopped decoding) must produce an
// empty plate. This mirrors the manual RED/GREEN check performed while writing this test
// (temporarily renaming objectData's `Plate` json tag to "recognized_license_plate_typo"
// made TestFrigate018RecognizedPlateDecoding fail with `plate: want "ABC123", got ""`;
// restoring the tag made it pass again) and keeps that proof executable going forward.
func TestObjectDataPlateTagIsLoadBearing(t *testing.T) {
	raw, err := os.ReadFile("testdata/frigate018_event_with_plate.json")
	if err != nil {
		t.Fatal(err)
	}

	type mistaggedObjectData struct {
		Plate      *string  `json:"recognized_license_plate_typo"`
		PlateScore *float64 `json:"recognized_license_plate_score"`
	}
	type mistaggedEvent struct {
		Data mistaggedObjectData `json:"data"`
	}

	var mistagged mistaggedEvent
	if err := json.Unmarshal(raw, &mistagged); err != nil {
		t.Fatal(err)
	}
	if mistagged.Data.Plate != nil {
		t.Fatalf("a mistagged json field must not decode a plate, got %v", *mistagged.Data.Plate)
	}

	var correct objectResponse
	if err := json.Unmarshal(raw, &correct); err != nil {
		t.Fatal(err)
	}
	if correct.Data.Plate == nil || *correct.Data.Plate != "ABC123" {
		t.Fatalf("the real objectData tag must decode the plate, got %v", correct.Data.Plate)
	}
}
