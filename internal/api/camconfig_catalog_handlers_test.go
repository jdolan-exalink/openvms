package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

type fakeLabels struct{ labels []string }

func (f fakeLabels) ServerModelLabels(context.Context, authz.Actor, uuid.UUID) ([]string, error) {
	return f.labels, nil
}

type labelFrigate struct {
	fakeFrigate
	labels []string
}

func (l labelFrigate) ModelLabels(context.Context) ([]string, error) { return l.labels, nil }

func getCatalog(t *testing.T, h *Handlers, p gen.GetObjectCatalogParams) gen.ObjectCatalog {
	t.Helper()
	res, err := h.GetObjectCatalog(writerCtx(), gen.GetObjectCatalogRequestObject{Params: p})
	if err != nil {
		t.Fatal(err)
	}
	return gen.ObjectCatalog(res.(gen.GetObjectCatalog200JSONResponse))
}

func TestObjectCatalogIsOrderedAndSupportIsUnknownWithoutLabels(t *testing.T) {
	out := getCatalog(t, &Handlers{}, gen.GetObjectCatalogParams{})
	var ids []string
	for _, c := range out.Categories {
		ids = append(ids, c.Id)
	}
	if out.Engine != "frigate" || len(ids) < 5 || ids[0] != "people" || ids[1] != "vehicle" || out.Categories[1].Label != "Vehículos" {
		t.Fatalf("catalog = %v / %q", ids, out.Engine)
	}
	for _, o := range out.Categories[1].Objects {
		switch o.Id {
		case "vehicle.truck":
			if o.Supported != nil || o.Label != "Camión" {
				t.Fatalf("truck = %+v", o)
			}
		case "vehicle.pickup":
			if o.Supported == nil || *o.Supported {
				t.Fatalf("pickup has no frigate label, supported = %v", o.Supported)
			}
		}
	}
}

func TestObjectCatalogFlagsSupportFromServerLabels(t *testing.T) {
	h := &Handlers{catalogLabels: fakeLabels{labels: []string{"person", "car"}}}
	sid := uuid.New()
	out := getCatalog(t, h, gen.GetObjectCatalogParams{ServerId: &sid})
	got := map[string]*bool{}
	for _, c := range out.Categories {
		for _, o := range c.Objects {
			got[o.Id] = o.Supported
		}
	}
	if got["person"] == nil || !*got["person"] || got["vehicle.truck"] == nil || *got["vehicle.truck"] {
		t.Fatalf("support = person %v truck %v", got["person"], got["vehicle.truck"])
	}
}

func TestObjectCatalogRequiresActor(t *testing.T) {
	_, err := (&Handlers{}).GetObjectCatalog(context.Background(), gen.GetObjectCatalogRequestObject{})
	if status, _, _ := statusFor(err); status != http.StatusUnauthorized {
		t.Fatalf("status = %d (%v)", status, err)
	}
}

func TestApplyResolvesSelectionToEngineLabels(t *testing.T) {
	w := &fakeCamConfigWriter{}
	h := &Handlers{camConfigWrite: w, camConfig: fakeCamConfigSource{ce: camConfigFixture(fakeFrigate{})}}
	body := gen.ApplyCameraConfigJSONRequestBody{Patch: gen.CameraConfigPatch{"detection": map[string]any{"selection": []any{"vehicle.car", "vehicle.truck"}}}}
	if _, err := h.ApplyCameraConfig(writerCtx(), gen.ApplyCameraConfigRequestObject{CameraId: uuid.New(), Body: &body}); err != nil {
		t.Fatal(err)
	}
	d := w.gotApply.Patch.Detection
	if d == nil || d.Selection != nil || d.Objects == nil || !reflect.DeepEqual(*d.Objects, []string{"car", "truck"}) {
		t.Fatalf("patch = %+v", w.gotApply.Patch)
	}
}

func TestApplyRejectsSelectionTheModelCannotHonor(t *testing.T) {
	w := &fakeCamConfigWriter{}
	ce := camConfigFixture(labelFrigate{labels: []string{"person", "car"}})
	h := &Handlers{camConfigWrite: w, camConfig: fakeCamConfigSource{ce: ce}}
	body := gen.ApplyCameraConfigJSONRequestBody{Patch: gen.CameraConfigPatch{"detection": map[string]any{"selection": []any{"person", "vehicle.truck"}}}}
	_, err := h.ApplyCameraConfig(writerCtx(), gen.ApplyCameraConfigRequestObject{CameraId: uuid.New(), Body: &body})
	if status, _, _ := statusFor(err); status != http.StatusBadRequest || w.applyCalls != 0 {
		t.Fatalf("status = %d calls = %d (%v)", status, w.applyCalls, err)
	}
}

func TestApplyRejectsSelectionMixedWithObjects(t *testing.T) {
	w := &fakeCamConfigWriter{}
	h := &Handlers{camConfigWrite: w}
	body := gen.ApplyCameraConfigJSONRequestBody{Patch: gen.CameraConfigPatch{"detection": map[string]any{"selection": []any{"person"}, "objects": []any{"car"}}}}
	_, err := h.ApplyCameraConfig(writerCtx(), gen.ApplyCameraConfigRequestObject{CameraId: uuid.New(), Body: &body})
	if status, _, _ := statusFor(err); status != http.StatusBadRequest || w.applyCalls != 0 {
		t.Fatalf("status = %d (%v)", status, err)
	}
}

func TestApplySelectionPropagatesPermissionErrors(t *testing.T) {
	boom := errors.New("boom")
	h := &Handlers{camConfigWrite: &fakeCamConfigWriter{}, camConfig: fakeCamConfigSource{err: boom}}
	body := gen.ApplyCameraConfigJSONRequestBody{Patch: gen.CameraConfigPatch{"detection": map[string]any{"selection": []any{"person"}}}}
	if _, err := h.ApplyCameraConfig(writerCtx(), gen.ApplyCameraConfigRequestObject{CameraId: uuid.New(), Body: &body}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadModelExposesCatalogSelection(t *testing.T) {
	doc := map[string]any{"objects": map[string]any{"track": []any{"person", "car", "zorro"}}}
	cfg, _ := getCamConfig(t, fakeCamConfigSource{ce: camConfigFixture(labelFrigate{fakeFrigate: fakeFrigate{doc: doc}, labels: []string{"person", "car", "zorro"}})})
	d := cfg.Detection
	if d.Selection == nil || !reflect.DeepEqual(*d.Selection, []string{"person", "vehicle.car"}) || d.Raw == nil || !reflect.DeepEqual(*d.Raw, []string{"zorro"}) ||
		d.AvailableLabels == nil || len(*d.AvailableLabels) != 3 {
		t.Fatalf("detection = %+v", d)
	}
}
