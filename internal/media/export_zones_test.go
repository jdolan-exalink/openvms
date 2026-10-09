package media

import (
	"reflect"
	"testing"
)

func TestZonesForManifest(t *testing.T) {
	cfg := map[string]any{
		"zones": map[string]any{
			"porch":   map[string]any{"coordinates": "0.1,0.2,0.3,0.4", "objects": []any{"person"}, "inertia": 3},
			"lawn":    map[string]any{"coordinates": []any{"0.1,0.2", "0.3,0.4"}},
			"nocoord": map[string]any{"objects": []any{"car"}},
			"bad":     "not-an-object",
		},
	}
	want := map[string]any{
		"porch": map[string]any{"coordinates": "0.1,0.2,0.3,0.4"},
		"lawn":  map[string]any{"coordinates": []any{"0.1,0.2", "0.3,0.4"}},
	}
	if got := zonesForManifest(cfg); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestZonesForManifestMissing(t *testing.T) {
	for _, cfg := range []map[string]any{nil, {}, {"zones": nil}, {"zones": "x"}, {"zones": map[string]any{"a": map[string]any{}}}} {
		if got := zonesForManifest(cfg); len(got) != 0 {
			t.Fatalf("cfg %#v: expected empty, got %#v", cfg, got)
		}
	}
}
