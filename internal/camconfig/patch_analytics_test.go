package camconfig

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParsePatchAnalyticsModules(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown module":       `{"analytics":{"modules":{"teleport":{"enabled":true}}}}`,
		"empty module":         `{"analytics":{"modules":{"lpr":{}}}}`,
		"unknown module field": `{"analytics":{"modules":{"lpr":{"enabled":true,"gain":3}}}}`,
		"settings w/o backing": `{"analytics":{"modules":{"face_detection":{"settings":{"min_area":10}}}}}`,
		"negative area":        `{"analytics":{"modules":{"lpr":{"settings":{"min_area":-1}}}}}`,
		"lpr conflict":         `{"analytics":{"lpr":true,"modules":{"lpr":{"enabled":false}}}}`,
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

func TestModuleTranslatesToFrigateSections(t *testing.T) {
	p, err := ParsePatch([]byte(`{"analytics":{"modules":{"lpr":{"enabled":true,"settings":{"min_area":1200}},"face_recognition":{"enabled":false}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := FrigateSections(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"lpr":              map[string]any{"enabled": true, "min_area": 1200},
		"face_recognition": map[string]any{"enabled": false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sections = %v", got)
	}
}

func TestLegacyAnalyticsLPRStillWorks(t *testing.T) {
	p, err := ParsePatch([]byte(`{"analytics":{"lpr":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := FrigateSections(p)
	if !reflect.DeepEqual(got, map[string]any{"lpr": map[string]any{"enabled": true}}) {
		t.Fatalf("sections = %v", got)
	}
	// Both spellings of the same switch collapse to the module.
	p, err = ParsePatch([]byte(`{"analytics":{"lpr":true,"modules":{"lpr":{"enabled":true}}}}`))
	if err != nil || p.Analytics.LPR != nil || p.Analytics.Modules[ModuleLPR].Enabled == nil {
		t.Fatalf("normalized = %+v (%v)", p.Analytics, err)
	}
	got, _ = FrigateSections(p)
	if !reflect.DeepEqual(got, map[string]any{"lpr": map[string]any{"enabled": true}}) {
		t.Fatalf("sections = %v", got)
	}
}

func TestModuleWithoutFrigateSwitchIsRejectedNotDropped(t *testing.T) {
	p, err := ParsePatch([]byte(`{"analytics":{"modules":{"similar_search":{"enabled":true}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = FrigateSections(p)
	var ve *ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Msg, "No disponible con este motor") {
		t.Fatalf("err = %v", err)
	}
	w := &recordingWriter{}
	if err := (FrigateAdapter{Writer: w}).Apply(context.Background(), p); err == nil || w.calls != 0 {
		t.Fatalf("apply err = %v, writes = %d", err, w.calls)
	}
}

func TestApplyModulePatchWritesThroughTheWriter(t *testing.T) {
	p, _ := ParsePatch([]byte(`{"analytics":{"modules":{"face_recognition":{"enabled":true}}}}`))
	w := &recordingWriter{}
	if err := (FrigateAdapter{Writer: w}).Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(w.sections, map[string]any{"face_recognition": map[string]any{"enabled": true}}) {
		t.Fatalf("sections = %v", w.sections)
	}
}

func TestPlanModuleChangesAndUnsupported(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{"lpr":{"enabled":false,"min_area":1000},"face_recognition":{"enabled":false}}`)
	p, _ := ParsePatch([]byte(`{"analytics":{"modules":{"lpr":{"enabled":true,"settings":{"min_area":2000}},"vehicle_color":{"enabled":true}}}}`))
	rev, err := FrigateAdapter{}.Plan(context.Background(), cam, p)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	for _, c := range rev.Changes {
		paths = append(paths, c.Path)
	}
	if !reflect.DeepEqual(paths, []string{"analytics.modules.lpr.enabled", "analytics.modules.lpr.settings.min_area"}) {
		t.Fatalf("changes = %v", paths)
	}
	if len(rev.Unsupported) != 1 || !strings.Contains(rev.Unsupported[0], "Color de vehículo") {
		t.Fatalf("unsupported = %v", rev.Unsupported)
	}
	if len(rev.Impact) != 1 || rev.Impact[0].Code != ImpactEngineRestart {
		t.Fatalf("impact = %+v", rev.Impact)
	}
}

func TestCheckModulesRejectsProvisionAndUnavailable(t *testing.T) {
	prov := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c", Tags: []string{"lpr_provision"}}, `{}`)
	p, _ := ParsePatch([]byte(`{"analytics":{"modules":{"lpr":{"enabled":false}}}}`))
	err := CheckModules(prov, p)
	var ve *ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Msg, "Se configura en la cámara") {
		t.Fatalf("provision err = %v", err)
	}
	rev, _ := FrigateAdapter{}.Plan(context.Background(), prov, p)
	if len(rev.Changes) != 0 || len(rev.Unsupported) != 1 {
		t.Fatalf("provision plan = %+v", rev)
	}
	frig := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{"lpr":{"enabled":true}}`)
	if err := CheckModules(frig, p); err != nil {
		t.Fatalf("frigate lpr must be writable: %v", err)
	}
	none := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{}`)
	if err := CheckModules(none, p); err == nil {
		t.Fatal("no evidence, must be refused")
	}
}

func TestModuleDriftIsReported(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{"face_recognition":{"enabled":false,"min_area":750}}`)
	p, _ := ParsePatch([]byte(`{"analytics":{"modules":{"face_recognition":{"enabled":true,"settings":{"min_area":750}}}}}`))
	d := Diff(p, cam)
	if len(d) != 1 || d[0].Path != "analytics.modules.face_recognition.enabled" {
		t.Fatalf("drift = %+v", d)
	}
	cam = modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{}`)
	if d := Diff(p, cam); len(d) != 2 {
		t.Fatalf("absence is not agreement: %+v", d)
	}
}

func TestMergeKeepsLatestLPRSwitch(t *testing.T) {
	base, _ := ParsePatch([]byte(`{"analytics":{"lpr":true}}`))
	over, _ := ParsePatch([]byte(`{"analytics":{"modules":{"lpr":{"enabled":false}}}}`))
	m := Merge(base, over)
	if m.Analytics.LPR != nil || *m.Analytics.Modules[ModuleLPR].Enabled {
		t.Fatalf("merged = %+v", m.Analytics)
	}
	back, _ := ParsePatch([]byte(`{"analytics":{"lpr":true}}`))
	m = Merge(m, back)
	if m.Analytics.LPR == nil || !*m.Analytics.LPR || len(m.Analytics.Modules) != 0 {
		t.Fatalf("merged = %+v", m.Analytics)
	}
}

type recordingLocal struct{ got map[string]bool }

func (r *recordingLocal) SetModuleEnabled(_ context.Context, m string, on bool) error {
	if r.got == nil {
		r.got = map[string]bool{}
	}
	r.got[m] = on
	return nil
}

func TestParsePatchLPRSettingsAreBoundedAndPerModule(t *testing.T) {
	for name, raw := range map[string]string{
		"enhancement too high": `{"analytics":{"modules":{"lpr":{"settings":{"enhancement":11}}}}}`,
		"enhancement negative": `{"analytics":{"modules":{"lpr":{"settings":{"enhancement":-1}}}}}`,
		"enhancement on face":  `{"analytics":{"modules":{"face_recognition":{"settings":{"enhancement":2}}}}}`,
		"threshold unknown":    `{"analytics":{"modules":{"face_recognition":{"settings":{"threshold":0.9}}}}}`,
		"settings on local":    `{"analytics":{"modules":{"vehicle_color":{"settings":{"min_area":5}}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var ve *ValidationError
			if _, err := ParsePatch([]byte(raw)); !errors.As(err, &ve) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	p, err := ParsePatch([]byte(`{"analytics":{"modules":{"lpr":{"settings":{"enhancement":4,"min_area":800}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := FrigateSections(p)
	if !reflect.DeepEqual(got, map[string]any{"lpr": map[string]any{"min_area": 800, "enhancement": 4}}) {
		t.Fatalf("sections = %v", got)
	}
}

func TestMergeKeepsEverySetting(t *testing.T) {
	a, _ := ParsePatch([]byte(`{"analytics":{"modules":{"lpr":{"settings":{"min_area":800}}}}}`))
	b, _ := ParsePatch([]byte(`{"analytics":{"modules":{"lpr":{"settings":{"enhancement":2}}}}}`))
	m := Merge(a, b).Analytics.Modules[ModuleLPR].Settings
	if m.MinArea == nil || *m.MinArea != 800 || m.Enhancement == nil || *m.Enhancement != 2 {
		t.Fatalf("merged = %+v", m)
	}
}

func TestEnhancementPlanAndDrift(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{"lpr":{"enabled":true,"enhancement":1}}`)
	p, _ := ParsePatch([]byte(`{"analytics":{"modules":{"lpr":{"settings":{"enhancement":5}}}}}`))
	rev, _ := FrigateAdapter{}.Plan(context.Background(), cam, p)
	if len(rev.Changes) != 1 || rev.Changes[0].Path != "analytics.modules.lpr.settings.enhancement" || rev.Changes[0].From != 1 || rev.Changes[0].To != 5 {
		t.Fatalf("changes = %+v", rev.Changes)
	}
	d := Diff(p, cam)
	if len(d) != 1 || d[0].Path != "analytics.modules.lpr.settings.enhancement" {
		t.Fatalf("drift = %+v", d)
	}
}

func openVMSCamera(t *testing.T) Camera {
	t.Helper()
	local := &LocalAnalytics{Enabled: map[string]bool{ModuleVehicleClassification: true, ModuleVehicleColor: true, ModuleClothingColor: false}}
	return modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c", Local: local}, `{}`)
}

func TestOpenVMSModulePatchGoesToTheLocalWriterNotFrigate(t *testing.T) {
	p, err := ParsePatch([]byte(`{"analytics":{"modules":{"vehicle_color":{"enabled":false},"clothing_color":{"enabled":true}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := FrigateSections(p); err != nil || len(got) != 0 {
		t.Fatalf("frigate sections = %v (%v)", got, err)
	}
	w, l := &recordingWriter{}, &recordingLocal{}
	if err := (FrigateAdapter{Writer: w, Local: l}).Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if w.calls != 0 || !reflect.DeepEqual(l.got, map[string]bool{ModuleVehicleColor: false, ModuleClothingColor: true}) {
		t.Fatalf("writer calls = %d, local = %v", w.calls, l.got)
	}
	if err := (FrigateAdapter{Writer: w}).Apply(context.Background(), p); err == nil {
		t.Fatal("without a local writer the apply must fail, not drop the change")
	}
}

func TestOpenVMSModulePlanHasNoEngineImpact(t *testing.T) {
	cam := openVMSCamera(t)
	p, _ := ParsePatch([]byte(`{"analytics":{"modules":{"clothing_color":{"enabled":true}}}}`))
	rev, err := FrigateAdapter{}.Plan(context.Background(), cam, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Changes) != 1 || rev.Changes[0].Path != "analytics.modules.clothing_color.enabled" || len(rev.Impact) != 0 || len(rev.Unsupported) != 0 {
		t.Fatalf("review = %+v", rev)
	}
	if err := CheckModules(cam, p); err != nil {
		t.Fatalf("writable: %v", err)
	}
	if d := Diff(p, cam); len(d) != 1 || d[0].Path != "analytics.modules.clothing_color.enabled" {
		t.Fatalf("drift = %+v", d)
	}
	same, _ := ParsePatch([]byte(`{"analytics":{"modules":{"clothing_color":{"enabled":false}}}}`))
	if d := Diff(same, cam); len(d) != 0 {
		t.Fatalf("no drift when it already matches: %+v", d)
	}
}

func TestLockedOpenVMSModuleIsRefused(t *testing.T) {
	local := &LocalAnalytics{Enabled: map[string]bool{ModuleVehicleClassification: false}, Locked: map[string]string{ModuleVehicleClassification: "Desactivado para todo el servidor"}}
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c", Local: local}, `{}`)
	p, _ := ParsePatch([]byte(`{"analytics":{"modules":{"vehicle_classification":{"enabled":true}}}}`))
	if err := CheckModules(cam, p); err == nil || !strings.Contains(err.Error(), "todo el servidor") {
		t.Fatalf("err = %v", err)
	}
}
