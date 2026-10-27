package camconfig

import (
	"context"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/frigate"
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
		if m.Available || m.Enabled || m.Writable || m.Engine != ModuleEngineNone || m.Reason == "" {
			t.Fatalf("%s must be unavailable: %+v", m.ID, m)
		}
	}
}

func TestOpenVMSModulesNeedTheirSwitchesLoaded(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c", LPR: true, Tags: []string{"lpr"}}, `{"lpr":{"enabled":true},"face_recognition":{"enabled":true}}`)
	for _, id := range []string{ModuleVehicleClassification, ModuleVehicleColor, ModuleClothingColor, ModuleSimilarSearch} {
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
}

func TestFaceDetectionIsImplicitInRecognition(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{"face_recognition":{"enabled":true}}`)
	d := mustModule(t, cam, ModuleFaceDetection)
	if !d.Available || !d.Enabled || d.Writable || d.Engine != ModuleEngineFrigate || d.Reason != ReasonFaceDetectionImplicit {
		t.Fatalf("detection = %+v", d)
	}
	off := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{"face_recognition":{"enabled":false}}`)
	if d := mustModule(t, off, ModuleFaceDetection); d.Enabled {
		t.Fatalf("detection follows recognition: %+v", d)
	}
	none := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{}`)
	if d := mustModule(t, none, ModuleFaceDetection); d.Available || d.Reason != ReasonNotAvailable {
		t.Fatalf("no face section, no detection: %+v", d)
	}
	p, _ := ParsePatch([]byte(`{"analytics":{"modules":{"face_detection":{"enabled":true}}}}`))
	if err := CheckModules(cam, p); err == nil || !strings.Contains(err.Error(), "interruptor de detección") {
		t.Fatalf("detection is not writable: %v", err)
	}
}

func TestLPRAndFaceSettingsCarryRealFrigateFields(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c"}, `{"lpr":{"enabled":true,"min_area":900,"enhancement":3},"face_recognition":{"enabled":true}}`)
	l := mustModule(t, cam, ModuleLPR)
	if l.Settings.MinArea == nil || *l.Settings.MinArea != 900 || l.Settings.Enhancement == nil || *l.Settings.Enhancement != 3 {
		t.Fatalf("lpr settings = %+v", l.Settings)
	}
	if f := mustModule(t, cam, ModuleFaceRecognition); f.Settings.Enhancement != nil {
		t.Fatalf("enhancement is LPR only: %+v", f.Settings)
	}
	storage := false
	for _, d := range l.Advanced {
		storage = storage || d.Label == "Resultados"
	}
	if !storage {
		t.Fatalf("storage fact missing: %+v", l.Advanced)
	}
}

type faceEngine struct{ fakeEngine }

func (faceEngine) FaceRecognitionSettings(context.Context) (frigate.FaceRecognitionSettings, error) {
	return frigate.FaceRecognitionSettings{ModelSize: "large", DetectionThreshold: 0.7, RecognitionThreshold: 0.9}, nil
}

func TestFaceRecognitionShowsServerWideSettingsAndListFact(t *testing.T) {
	eng := faceEngine{fakeEngine{doc: parse(t, `{"face_recognition":{"enabled":true}}`)}}
	cam, err := FrigateAdapter{Engine: eng}.Read(context.Background(), Inventory{CameraID: "c", RemoteName: "c"})
	if err != nil {
		t.Fatal(err)
	}
	f := mustModule(t, cam, ModuleFaceRecognition)
	got := map[string]string{}
	for _, d := range f.Advanced {
		got[d.Label] = d.Value
	}
	if got["Modelo (servidor)"] != "large" || got["Umbral de reconocimiento (servidor)"] != "0.9" || !strings.Contains(got["Base de rostros"], "no hay una lista por cámara") {
		t.Fatalf("advanced = %v", got)
	}
}

func TestOpenVMSModulesFromLocalSwitches(t *testing.T) {
	local := &LocalAnalytics{
		Enabled: map[string]bool{ModuleVehicleClassification: false, ModuleVehicleColor: true, ModuleClothingColor: true},
		Locked:  map[string]string{ModuleVehicleClassification: "Desactivado para todo el servidor"},
	}
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c", Local: local}, `{}`)
	c := mustModule(t, cam, ModuleVehicleColor)
	if !c.Available || !c.Enabled || !c.Writable || c.Engine != ModuleEngineOpenVMS || c.Reason != "" {
		t.Fatalf("vehicle color = %+v", c)
	}
	k := mustModule(t, cam, ModuleVehicleClassification)
	if !k.Available || k.Enabled || k.Writable || k.Reason != "Desactivado para todo el servidor" {
		t.Fatalf("classification = %+v", k)
	}
	if s := mustModule(t, cam, ModuleSimilarSearch); s.Available || s.Reason != ReasonSimilarSearch {
		t.Fatalf("similar = %+v", s)
	}
}

func TestEveryModuleIsEitherRealOrExplained(t *testing.T) {
	cam := modulesOfDoc(t, Inventory{CameraID: "c", RemoteName: "c", Local: &LocalAnalytics{Enabled: map[string]bool{}}}, `{}`)
	for _, m := range cam.Analytics.Modules {
		if !m.Available && m.Reason == "" {
			t.Fatalf("%s is unavailable without a reason", m.ID)
		}
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
