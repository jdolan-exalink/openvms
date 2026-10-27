package camconfig

import (
	"strings"
	"testing"
)

func TestInversePatchRestoresOldValues(t *testing.T) {
	p, err := InversePatch(map[string]any{
		"detection.enabled":                                    false,
		"detection.objects":                                    []any{"person"},
		"detection.selection.people":                           false, // derived, ignored
		"recording.enabled":                                    true,
		"analytics.lpr":                                        false,
		"analytics.modules.face_recognition.enabled":           false,
		"analytics.modules.face_recognition.settings.min_area": 400.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if *p.Detection.Enabled != false || len(*p.Detection.Objects) != 1 || *p.Recording.Enabled != true || *p.Analytics.LPR != false {
		t.Fatalf("patch = %+v", p)
	}
	m := p.Analytics.Modules["face_recognition"]
	if m == nil || *m.Enabled != false || *m.Settings.MinArea != 400 {
		t.Fatalf("module = %+v", m)
	}
}

func TestInversePatchRefusesWhatItCannotRestore(t *testing.T) {
	cases := []struct {
		name string
		prev map[string]any
		frag string
	}{
		{"unknown old value", map[string]any{"detection.enabled": nil}, "no se conoce el valor anterior de detection.enabled"},
		{"video", map[string]any{"streams.primary.bitrate": 4096.0}, "video"},
		{"engine path from a yaml edit", map[string]any{"detect.fps": 5.0}, "no se puede restaurar detect.fps"},
		{"nothing recorded", map[string]any{}, "no registró valores anteriores"},
		{"only derived", map[string]any{"detection.selection.people": true}, "no registró valores anteriores"},
		{"partial is refused", map[string]any{"detection.enabled": true, "recording.enabled": nil}, "recording.enabled"},
	}
	for _, c := range cases {
		_, err := InversePatch(c.prev)
		if err == nil || !strings.Contains(err.Error(), c.frag) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.frag)
		}
	}
}

func TestRollbackable(t *testing.T) {
	ok := map[string]any{"detection.enabled": false}
	if r, why := Rollbackable(RollbackSource{Origin: OriginUI, Result: ResultOK, Previous: ok}); !r || why != "" {
		t.Fatalf("ok change: %v %q", r, why)
	}
	if r, _ := Rollbackable(RollbackSource{Origin: OriginUI, Result: ResultUnverified, Previous: ok}); !r {
		t.Fatal("an unverified change was applied and can be undone")
	}
	if r, why := Rollbackable(RollbackSource{Origin: OriginUI, Result: ResultError, Previous: ok}); r || why == "" {
		t.Fatalf("failed change: %v %q", r, why)
	}
	if r, why := Rollbackable(RollbackSource{Origin: OriginYAML, Result: ResultOK, Previous: ok}); r || why == "" {
		t.Fatalf("yaml change: %v %q", r, why)
	}
	if r, why := Rollbackable(RollbackSource{Origin: OriginUI, Result: ResultOK, Previous: map[string]any{"detection.enabled": nil}}); r || why == "" {
		t.Fatalf("no old value: %v %q", r, why)
	}
}
