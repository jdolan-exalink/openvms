package camconfig

import (
	"context"
	"testing"
)

func modulesOfDoc(t *testing.T, inv Inventory, doc string) Camera {
	t.Helper()
	eng := fakeEngine{docErr: nil}
	if doc != "" {
		eng.doc = parse(t, doc)
	}
	cam, err := FrigateAdapter{Engine: eng}.Read(context.Background(), inv)
	if err != nil {
		t.Fatal(err)
	}
	return cam
}

func mustModule(t *testing.T, c Camera, id string) AnalyticsModule {
	t.Helper()
	m, ok := c.Analytics.Module(id)
	if !ok {
		t.Fatalf("module %s missing", id)
	}
	return m
}

func TestModulesListedInOrderWithLabels(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{}`)
	want := []string{"Matrículas (LPR)", "Detección facial", "Reconocimiento facial", "Clasificación de vehículo", "Color de vehículo", "Color de ropa", "Buscar similares cuando corresponda"}
	if len(cam.Analytics.Modules) != len(want) {
		t.Fatalf("modules = %d", len(cam.Analytics.Modules))
	}
	for i, m := range cam.Analytics.Modules {
		if m.Label != want[i] {
			t.Fatalf("module %d label = %q", i, m.Label)
		}
	}
}

func TestFrigateLPRModuleFromEngineSection(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{"lpr":{"enabled":true,"min_area":1500,"expire_time":3}}`)
	m := mustModule(t, cam, ModuleLPR)
	if !m.Available || !m.Enabled || !m.Writable || m.Engine != ModuleEngineFrigate || m.Reason != "" {
		t.Fatalf("lpr = %+v", m)
	}
	if m.Settings.MinArea == nil || *m.Settings.MinArea != 1500 {
		t.Fatalf("settings = %+v", m.Settings)
	}
	found := false
	for _, d := range m.Advanced {
		found = found || (d.Label == "expire_time" && d.Value == "3")
	}
	if !found {
		t.Fatalf("advanced = %+v", m.Advanced)
	}
}

func TestFrigateLPRDisabledStaysAvailable(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{"lpr":{"enabled":false}}`)
	m := mustModule(t, cam, ModuleLPR)
	if !m.Available || m.Enabled {
		t.Fatalf("lpr = %+v", m)
	}
}

func TestProvisionLPRModuleIsReadOnly(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c", Tags: []string{"lpr", "provision"}}, `{}`)
	m := mustModule(t, cam, ModuleLPR)
	if !m.Available || !m.Enabled || m.Writable || m.Engine != ModuleEngineProvision || m.Reason != ReasonConfiguredOnCam {
		t.Fatalf("lpr = %+v", m)
	}
	cam = modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c", Tags: []string{"lpr_provision"}}, `{"lpr":{"enabled":false}}`)
	if m := mustModule(t, cam, ModuleLPR); m.Engine != ModuleEngineProvision {
		t.Fatalf("explicit provision tag must win: %+v", m)
	}
}

func TestModulesWithoutEvidenceAreUnavailable(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{"detect":{"enabled":true}}`)
	for _, m := range cam.Analytics.Modules {
		if m.Available || m.Enabled || m.Writable || m.Engine != ModuleEngineNone || m.Reason != "No disponible con este motor" {
			t.Fatalf("%s must be unavailable: %+v", m.ID, m)
		}
	}
}

func TestVehicleAndClothingModulesNeverClaimed(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c", LPR: true, Tags: []string{"lpr"}}, `{"lpr":{"enabled":true},"face_recognition":{"enabled":true}}`)
	for _, id := range []string{ModuleFaceDetection, ModuleVehicleClassification, ModuleVehicleColor, ModuleClothingColor, ModuleSimilarSearch} {
		if m := mustModule(t, cam, id); m.Available {
			t.Fatalf("%s has no backing and must not be available: %+v", id, m)
		}
	}
}

func TestFaceRecognitionModuleFromEngineSection(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{"face_recognition":{"enabled":true,"min_area":900}}`)
	m := mustModule(t, cam, ModuleFaceRecognition)
	if !m.Available || !m.Enabled || !m.Writable || m.Engine != ModuleEngineFrigate || *m.Settings.MinArea != 900 {
		t.Fatalf("face_recognition = %+v", m)
	}
	if d := mustModule(t, cam, ModuleFaceDetection); d.Available {
		t.Fatalf("detection and recognition are separate: %+v", d)
	}
}

func TestEngineDownKeepsInventoryEvidenceOnly(t *testing.T) {
	cam, err := FrigateAdapter{}.Read(context.Background(), Inventory{CameraID: "c", RemoteName: "c", Tags: []string{"lpr_frigate"}})
	if err != nil {
		t.Fatal(err)
	}
	m := mustModule(t, cam, ModuleLPR)
	if !m.Available || m.Writable || m.Engine != ModuleEngineFrigate {
		t.Fatalf("lpr without engine = %+v", m)
	}
	if f := mustModule(t, cam, ModuleFaceRecognition); f.Available {
		t.Fatalf("no document, no face evidence: %+v", f)
	}
}
