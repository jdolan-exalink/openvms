package catalog

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

func supportedObjects(c Catalog, engine string) []string {
	var ids []string
	for _, cat := range c.Categories {
		for _, o := range cat.Objects {
			if len(o.Engine[engine]) > 0 {
				ids = append(ids, o.ID)
			}
		}
	}
	return ids
}

func TestDefaultIsValidAndOrderStable(t *testing.T) {
	a, b := Default(), Default()
	if !reflect.DeepEqual(a.Categories, b.Categories) {
		t.Fatal("Default() not deterministic")
	}
	var ids []string
	for _, c := range a.Categories {
		ids = append(ids, c.ID)
	}
	want := []string{"people", "vehicle", "animal", "package", "safety", "other"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("category order = %v", ids)
	}
	var veh []string
	for _, o := range a.Categories[1].Objects {
		veh = append(veh, o.ID)
	}
	wantV := []string{"vehicle.car", "vehicle.pickup", "vehicle.truck", "vehicle.bus", "vehicle.motorcycle", "vehicle.bicycle", "vehicle.other", "vehicle.generic"}
	if !reflect.DeepEqual(veh, wantV) {
		t.Fatalf("vehicle order = %v", veh)
	}
}

func TestNewRejectsLabelInTwoObjects(t *testing.T) {
	_, err := New([]Category{
		{ID: "a", Objects: []Object{{ID: "a.x", Engine: fr("car")}}},
		{ID: "b", Objects: []Object{{ID: "b.y", Engine: fr("car")}}},
	})
	if err == nil {
		t.Fatal("expected duplicate label error")
	}
}

func TestEveryLabelBelongsToOneObject(t *testing.T) {
	c := Default()
	seen := map[string]string{}
	for _, cat := range c.Categories {
		for _, o := range cat.Objects {
			for _, l := range o.Engine[EngineFrigate] {
				if prev, dup := seen[l]; dup {
					t.Fatalf("label %q in %s and %s", l, prev, o.ID)
				}
				seen[l] = o.ID
			}
		}
	}
	for _, l := range []string{"person", "car", "dog", "amazon", "license_plate", "toothbrush"} {
		if _, ok := seen[l]; !ok {
			t.Errorf("known label %q unmapped", l)
		}
	}
}

func TestSpecExamples(t *testing.T) {
	c := Default()
	got := c.ExpandToEngine(EngineFrigate, []string{"vehicle"})
	want := []string{"airplane", "boat", "bicycle", "bus", "car", "motorcycle", "train", "truck"}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("vehicle = %v", got)
	}
	pk := c.ExpandToEngine(EngineFrigate, []string{"package.box"})
	for _, l := range []string{"amazon", "ups", "package"} {
		if !contains(pk, l) {
			t.Errorf("package.box missing %q", l)
		}
	}
	if id, cat, ok := c.Classify(EngineFrigate, "ups"); !ok || id != "package.box" || cat != "package" {
		t.Fatalf("Classify ups = %q %q %v", id, cat, ok)
	}
	if _, _, ok := c.Classify(EngineFrigate, "dragon"); ok {
		t.Fatal("unknown label classified")
	}
}

func TestRoundTripIdentity(t *testing.T) {
	c := Default()
	ids := supportedObjects(c, EngineFrigate)
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // deterministic test data
	for i := 0; i < 300; i++ {
		var sel []string
		for _, id := range ids {
			if rng.Intn(3) == 0 {
				sel = append(sel, id)
			}
		}
		labels := c.ExpandToEngine(EngineFrigate, sel)
		back := c.CollapseFromEngine(EngineFrigate, labels)
		if !sameSet(back.Objects, sel) {
			t.Fatalf("round trip %v -> %v -> %v", sel, labels, back.Objects)
		}
		if len(back.Raw) != 0 {
			t.Fatalf("unexpected raw %v", back.Raw)
		}
		// and labels -> objects -> labels is stable
		if again := c.ExpandToEngine(EngineFrigate, back.Objects); !reflect.DeepEqual(again, labels) {
			t.Fatalf("labels not stable: %v vs %v", labels, again)
		}
	}
}

func TestUnknownLabelsSurviveViaRaw(t *testing.T) {
	c := Default()
	s := c.CollapseFromEngine(EngineFrigate, []string{"zzz", "car", "aaa", "zzz"})
	if !reflect.DeepEqual(s.Raw, []string{"aaa", "zzz"}) || !reflect.DeepEqual(s.Objects, []string{"vehicle.car"}) {
		t.Fatalf("collapse = %+v", s)
	}
}

func TestAllVehiclesToggleSemantics(t *testing.T) {
	c := Default()
	all := c.ExpandToEngine(EngineFrigate, []string{"vehicle"})
	if s := c.CollapseFromEngine(EngineFrigate, all); !contains(s.Categories, "vehicle") {
		t.Fatalf("all vehicles not detected: %+v", s)
	}
	// vehicle.pickup has no Frigate labels, so it must not block the toggle.
	partial := c.CollapseFromEngine(EngineFrigate, []string{"car", "truck"})
	if contains(partial.Categories, "vehicle") {
		t.Fatal("partial selection reported as all vehicles")
	}
	if len(c.CollapseFromEngine(EngineFrigate, nil).Categories) != 0 {
		t.Fatal("empty selection selects categories")
	}
}

func TestPPEFlaggedWhenModelLacksIt(t *testing.T) {
	c := Default()
	avail := []string{"person", "car"}
	labels, unsup := c.ExpandForModel(EngineFrigate, []string{"person", "safety.helmet", "safety"}, avail)
	if !reflect.DeepEqual(labels, []string{"person"}) {
		t.Fatalf("labels = %v", labels)
	}
	if !reflect.DeepEqual(unsup, []string{"safety.helmet"}) {
		t.Fatalf("unsupported = %v", unsup)
	}
	labels, unsup = c.ExpandForModel(EngineFrigate, []string{"safety.helmet"}, []string{"helmet"})
	if !reflect.DeepEqual(labels, []string{"helmet"}) || len(unsup) != 0 {
		t.Fatalf("supported PPE: %v %v", labels, unsup)
	}
}

func TestNoInventedPrecision(t *testing.T) {
	c := Default()
	labels, unsup := c.ExpandForModel(EngineFrigate, []string{"vehicle.pickup", "nope"}, nil)
	if len(labels) != 0 || !reflect.DeepEqual(unsup, []string{"nope", "vehicle.pickup"}) {
		t.Fatalf("labels=%v unsupported=%v", labels, unsup)
	}
	// An engine without a mapping reports every explicit object unsupported.
	if l := c.ExpandToEngine("lidar", []string{"vehicle"}); len(l) != 0 {
		t.Fatalf("unmapped engine labels = %v", l)
	}
}

// Provision only says "vehicle": the category maps to that one label and the finer objects
// stay unsupported instead of being approximated.
func TestProvisionVehicleHasNoSubtype(t *testing.T) {
	c := Default()
	if l := c.ExpandToEngine(EngineProvision, []string{"vehicle"}); !reflect.DeepEqual(l, []string{"vehicle"}) {
		t.Fatalf("provision vehicles = %v", l)
	}
	labels, unsup := c.ExpandForModel(EngineProvision, []string{"vehicle.car", "vehicle.truck", "vehicle.generic"}, nil)
	if !reflect.DeepEqual(labels, []string{"vehicle"}) || !reflect.DeepEqual(unsup, []string{"vehicle.car", "vehicle.truck"}) {
		t.Fatalf("labels=%v unsupported=%v", labels, unsup)
	}
	sel := c.CollapseFromEngine(EngineProvision, []string{"vehicle", "license_plate"})
	if !reflect.DeepEqual(sel.Objects, []string{"vehicle.generic", "other.license_plate"}) || !contains(sel.Categories, "vehicle") || len(sel.Raw) != 0 {
		t.Fatalf("collapse = %+v", sel)
	}
	// The Frigate view of the same catalog is unchanged by the Provision object.
	if l := c.ExpandToEngine(EngineFrigate, []string{"vehicle.generic"}); len(l) != 0 {
		t.Fatalf("frigate labels for the generic vehicle = %v", l)
	}
}

func TestProvisionLabelsBelongToOneObject(t *testing.T) {
	c := Default()
	seen := map[string]string{}
	for _, cat := range c.Categories {
		for _, o := range cat.Objects {
			for _, l := range o.Engine[EngineProvision] {
				if prev, dup := seen[l]; dup {
					t.Fatalf("provision label %q in %s and %s", l, prev, o.ID)
				}
				seen[l] = o.ID
			}
		}
	}
	if len(seen) != 2 {
		t.Fatalf("only the evidenced labels (vehicle, license_plate) may be mapped: %v", seen)
	}
}

func TestProvisionRoundTripIdentity(t *testing.T) {
	c := Default()
	ids := supportedObjects(c, EngineProvision)
	for mask := 0; mask < 1<<len(ids); mask++ {
		var sel []string
		for i, id := range ids {
			if mask&(1<<i) != 0 {
				sel = append(sel, id)
			}
		}
		labels := c.ExpandToEngine(EngineProvision, sel)
		back := c.CollapseFromEngine(EngineProvision, labels)
		if !sameSet(back.Objects, sel) || len(back.Raw) != 0 {
			t.Fatalf("round trip %v -> %v -> %+v", sel, labels, back)
		}
	}
}

func TestEngineMappingsAreIndependent(t *testing.T) {
	c, err := New([]Category{{ID: "vehicle", Objects: []Object{
		{ID: "vehicle.any", Engine: map[string][]string{"provision": {"vehicle"}, EngineFrigate: {"car", "truck"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ExpandToEngine("provision", []string{"vehicle"}); !reflect.DeepEqual(got, []string{"vehicle"}) {
		t.Fatalf("provision = %v", got)
	}
	if id, _, ok := c.Classify("provision", "vehicle"); !ok || id != "vehicle.any" {
		t.Fatal("provision classify failed")
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func sameSet(a, b []string) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return reflect.DeepEqual(x, y) || (len(x) == 0 && len(y) == 0)
}
