package camconfig

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type labelEngine struct {
	fakeEngine
	labels []string
}

func (l labelEngine) ModelLabels(context.Context) ([]string, error) { return l.labels, nil }

func TestSelectionExpandsToEngineLabels(t *testing.T) {
	p, err := ParsePatch([]byte(`{"detection":{"selection":["vehicle","person"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	r, unsupported := ResolveSelection(p, nil)
	if len(unsupported) != 0 {
		t.Fatalf("unsupported = %v", unsupported)
	}
	want := []string{"airplane", "bicycle", "boat", "bus", "car", "motorcycle", "person", "train", "truck"}
	if got := *r.Detection.Objects; !reflect.DeepEqual(got, want) || r.Detection.Selection != nil {
		t.Fatalf("resolved = %v selection=%v", got, r.Detection.Selection)
	}
	sections, err := FrigateSections(r)
	if err != nil || !reflect.DeepEqual(sections["objects"], map[string]any{"track": want}) {
		t.Fatalf("sections = %v (%v)", sections, err)
	}
}

func TestSelectionAndObjectsTogetherAreRejected(t *testing.T) {
	_, err := ParsePatch([]byte(`{"detection":{"selection":["person"],"objects":["car"]}}`))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownSelectionIdIsRejected(t *testing.T) {
	_, err := ParsePatch([]byte(`{"detection":{"selection":["vehicle.spaceship"]}}`))
	var ve *ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Msg, "vehicle.spaceship") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnresolvedSelectionIsNeverSentToTheEngine(t *testing.T) {
	w := &recordingWriter{}
	p, _ := ParsePatch([]byte(`{"detection":{"selection":["person"]}}`))
	if err := (FrigateAdapter{Writer: w}).Apply(context.Background(), p); err == nil || w.calls != 0 {
		t.Fatalf("err = %v calls = %d", err, w.calls)
	}
}

func TestPlanReportsUnsupportedAndSpanishCatalogChanges(t *testing.T) {
	cur := Camera{Detection: Detection{Objects: []string{"person"}, AvailableLabels: []string{"person", "car", "truck"}}}
	p, _ := ParsePatch([]byte(`{"detection":{"selection":["person","vehicle.truck","vehicle.pickup"]}}`))
	rev, err := FrigateAdapter{}.Plan(context.Background(), cur, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Unsupported) != 1 || !strings.Contains(rev.Unsupported[0], "Camioneta (vehicle.pickup)") {
		t.Fatalf("unsupported = %v", rev.Unsupported)
	}
	var truck *Change
	for i, c := range rev.Changes {
		if c.Path == "detection.selection.vehicle.truck" {
			truck = &rev.Changes[i]
		}
		if c.Path == "detection.selection.person" {
			t.Fatalf("unchanged object listed: %+v", c)
		}
	}
	if truck == nil || truck.Label != "Camión" || truck.From != false || truck.To != true {
		t.Fatalf("changes = %+v", rev.Changes)
	}
}

func TestPlanCategoryFlipUsesCategoryLabel(t *testing.T) {
	cur := Camera{Detection: Detection{Objects: []string{}}}
	p, _ := ParsePatch([]byte(`{"detection":{"selection":["vehicle"]}}`))
	rev, _ := FrigateAdapter{}.Plan(context.Background(), cur, p)
	found := false
	for _, c := range rev.Changes {
		if c.Path == "detection.selection.vehicle" && c.Label == "Vehículos" && c.From == false && c.To == true {
			found = true
		}
	}
	if !found || len(rev.Unsupported) != 0 {
		t.Fatalf("review = %+v", rev)
	}
}

func TestReadCollapsesTrackedLabelsAndKeepsRaw(t *testing.T) {
	eng := labelEngine{
		fakeEngine: fakeEngine{doc: map[string]any{"objects": map[string]any{"track": []any{"person", "car", "truck", "zorro"}}}},
		labels:     []string{"zorro", "person", "car", "truck"},
	}
	cam, err := FrigateAdapter{Engine: eng}.Read(context.Background(), Inventory{CameraID: "c", RemoteName: "cam1"})
	if err != nil {
		t.Fatal(err)
	}
	d := cam.Detection
	if !reflect.DeepEqual(d.Selection, []string{"person", "vehicle.car", "vehicle.truck"}) ||
		!reflect.DeepEqual(d.Raw, []string{"zorro"}) || !reflect.DeepEqual(d.Objects, []string{"person", "car", "truck", "zorro"}) ||
		!reflect.DeepEqual(d.AvailableLabels, []string{"car", "person", "truck", "zorro"}) {
		t.Fatalf("detection = %+v", d)
	}
}

func TestReadWithoutModelLabelsLeavesThemUnknown(t *testing.T) {
	cam, _ := FrigateAdapter{}.Read(context.Background(), Inventory{CameraID: "c"})
	if cam.Detection.AvailableLabels != nil || cam.Detection.Selection == nil || cam.Detection.Raw == nil {
		t.Fatalf("detection = %+v", cam.Detection)
	}
}
