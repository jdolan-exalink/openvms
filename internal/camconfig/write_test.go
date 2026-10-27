package camconfig

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func bp(b bool) *bool { return &b }

type recordingWriter struct {
	calls    int
	sections map[string]any
	err      error
}

func (w *recordingWriter) PatchSections(_ context.Context, s map[string]any) error {
	w.calls++
	w.sections = s
	return w.err
}

func TestParsePatchRejectsWhatItCannotTranslate(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown top-level field": `{"streams":{"primary":{"width":1920}}}`,
		"unknown nested field":    `{"detection":{"enabled":true,"sensitivity":3}}`,
		"wrong type":              `{"detection":{"enabled":"yes"}}`,
		"empty patch":             `{}`,
		"empty branches":          `{"detection":{},"recording":{}}`,
		"bad object label":        `{"detection":{"objects":["Person; rm"]}}`,
		"trailing data":           `{"recording":{"enabled":true}} {}`,
		"not an object":           `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParsePatch([]byte(raw))
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want *ValidationError", err)
			}
		})
	}
}

func TestParsePatchNormalizesObjects(t *testing.T) {
	p, err := ParsePatch([]byte(`{"detection":{"objects":["person","car","person"]},"analytics":{"lpr":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := *p.Detection.Objects; !reflect.DeepEqual(got, []string{"person", "car"}) {
		t.Fatalf("objects = %v", got)
	}
}

func TestMergeKeepsOlderFieldsAndOverridesNewer(t *testing.T) {
	objs := []string{"person"}
	base := Patch{Detection: &DetectionPatch{Enabled: bp(true), Objects: &objs}, Recording: &RecordingPatch{Enabled: bp(true)}}
	got := Merge(base, Patch{Detection: &DetectionPatch{Enabled: bp(false)}, Analytics: &AnalyticsPatch{LPR: bp(true)}})
	if *got.Detection.Enabled || len(*got.Detection.Objects) != 1 || !*got.Recording.Enabled || !*got.Analytics.LPR {
		t.Fatalf("merge = %+v", got)
	}
}

func readyCamera() Camera {
	return Camera{
		Detection: Detection{Enabled: bp(false), Objects: []string{"person"}},
		Recording: Recording{Enabled: bp(true)},
		Analytics: Analytics{LPR: bp(false)},
		Sources:   []Source{{Name: "frigate", Status: SourceOK}},
	}
}

func TestPlanListsChangesAndImpactWithoutTouchingTheEngine(t *testing.T) {
	w := &recordingWriter{}
	ad := FrigateAdapter{Writer: w}
	objs := []string{"person", "car"}
	rev, err := ad.Plan(context.Background(), readyCamera(), Patch{
		Detection: &DetectionPatch{Enabled: bp(true), Objects: &objs},
		Recording: &RecordingPatch{Enabled: bp(true)}, // already on: not a change
		Analytics: &AnalyticsPatch{LPR: bp(true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.calls != 0 {
		t.Fatalf("Plan wrote to the engine %d times", w.calls)
	}
	paths := []string{}
	for _, c := range rev.Changes {
		paths = append(paths, c.Path)
	}
	if !reflect.DeepEqual(paths, []string{"detection.enabled", "detection.objects", "analytics.lpr"}) {
		t.Fatalf("changes = %v", paths)
	}
	if c := rev.Changes[0]; c.From != false || c.To != true || c.Label == "" {
		t.Fatalf("detection change = %+v", c)
	}
	var codes []string
	for _, i := range rev.Impact {
		codes = append(codes, i.Code)
	}
	// lpr is not hot-reloadable in Frigate, detect/objects are.
	if !reflect.DeepEqual(codes, []string{ImpactEngineRestart, ImpactEngineReload}) {
		t.Fatalf("impact = %v", rev.Impact)
	}
	if rev.Impact[1].Message != "Frigate recargará configuración;" {
		t.Fatalf("message = %q", rev.Impact[1].Message)
	}
}

func TestPlanRecordingChangeWarnsAboutInterruption(t *testing.T) {
	cam := readyCamera()
	rev, _ := FrigateAdapter{}.Plan(context.Background(), cam, Patch{Recording: &RecordingPatch{Enabled: bp(false)}})
	found := false
	for _, i := range rev.Impact {
		found = found || i.Message == "grabación puede interrumpirse brevemente."
	}
	if !found {
		t.Fatalf("impact = %+v", rev.Impact)
	}
}

func TestPlanUnknownCurrentValueIsNull(t *testing.T) {
	rev, _ := FrigateAdapter{}.Plan(context.Background(), Camera{}, Patch{Recording: &RecordingPatch{Enabled: bp(true)}})
	if len(rev.Changes) != 1 || rev.Changes[0].From != nil {
		t.Fatalf("changes = %+v", rev.Changes)
	}
}

func TestPlanRejectsInvalidPatch(t *testing.T) {
	if _, err := (FrigateAdapter{}).Plan(context.Background(), Camera{}, Patch{}); err == nil {
		t.Fatal("empty patch must be rejected")
	}
}

func TestApplyTranslatesToFrigateSections(t *testing.T) {
	w := &recordingWriter{}
	objs := []string{"person"}
	err := FrigateAdapter{Writer: w}.Apply(context.Background(), Patch{
		Detection: &DetectionPatch{Enabled: bp(true), Objects: &objs},
		Recording: &RecordingPatch{Enabled: bp(false)},
		Analytics: &AnalyticsPatch{LPR: bp(true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"detect":  map[string]any{"enabled": true},
		"objects": map[string]any{"track": []string{"person"}},
		"record":  map[string]any{"enabled": false},
		"lpr":     map[string]any{"enabled": true},
	}
	if !reflect.DeepEqual(w.sections, want) {
		t.Fatalf("sections = %#v", w.sections)
	}
}

func TestApplyFailsClosed(t *testing.T) {
	w := &recordingWriter{}
	if err := (FrigateAdapter{Writer: w}).Apply(context.Background(), Patch{}); err == nil || w.calls != 0 {
		t.Fatalf("invalid patch: err=%v calls=%d", err, w.calls)
	}
	if err := (FrigateAdapter{}).Apply(context.Background(), Patch{Recording: &RecordingPatch{Enabled: bp(true)}}); err == nil {
		t.Fatal("no writer must be an error")
	}
	boom := errors.New("engine down")
	w = &recordingWriter{err: boom}
	if err := (FrigateAdapter{Writer: w}).Apply(context.Background(), Patch{Recording: &RecordingPatch{Enabled: bp(true)}}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestSanitizeErrorHidesCredentials(t *testing.T) {
	// Built at run time: the fixture holds no credential-looking literal.
	userinfo := "admin" + ":" + "hunter2" + "@"
	err := errors.New("Get http://" + userinfo + "10.0.0.5:5000/api/config?token=abc123 failed: password=hunter2")
	got := SanitizeError(err)
	for _, bad := range []string{"hunter2", "abc123", "admin:"} {
		if strings.Contains(got, bad) {
			t.Fatalf("%q leaked in %q", bad, got)
		}
	}
	if long := SanitizeError(errors.New(strings.Repeat("x", 1000))); len([]rune(long)) > 301 {
		t.Fatalf("not capped: %d", len(long))
	}
}
