package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// userinfo is built at run time so the fixtures hold no credential-looking literal.
const userinfo = "admin" + ":" + "hunter2" + "@"

// fakeFrigate overrides only the adapter methods the camera config model reads.
type fakeFrigate struct {
	frigate.Adapter
	doc    map[string]any
	docErr error
}

func (f fakeFrigate) Version() string { return "0.17.0" }
func (f fakeFrigate) CameraConfig(context.Context, string, bool) (map[string]any, error) {
	return f.doc, f.docErr
}
func (f fakeFrigate) StreamCodecs(context.Context) (map[string]string, error) {
	return map[string]string{"cam1": "h265", "cam1_sub": "h264"}, nil
}

type fakeCamConfigSource struct {
	ce  inventory.CameraEngine
	err error
}

func (f fakeCamConfigSource) OpenCameraEngine(context.Context, authz.Actor, uuid.UUID) (inventory.CameraEngine, error) {
	return f.ce, f.err
}

func camConfigFixture(ad frigate.Adapter) inventory.CameraEngine {
	endpoint := "http://192.168.1.50/onvif/device_service"
	return inventory.CameraEngine{
		ServerName: "Site A",
		Adapter:    ad,
		Camera: db.GetCameraRow{
			ID: uuid.New(), ServerID: uuid.New(), RemoteName: "cam1", DisplayName: "Front door", Enabled: true,
			HqStream: "cam1", LiveStream: "cam1_sub", Tags: []string{"lpr_frigate"}, OnvifEndpoint: &endpoint,
			OnvifCapabilities: json.RawMessage(`{"manufacturer":"Axis","model":"P3245","has_ptz":false}`),
		},
	}
}

func getCamConfig(t *testing.T, src fakeCamConfigSource) (gen.CameraConfig, []byte) {
	t.Helper()
	h := &Handlers{camConfig: src}
	res, err := h.GetCameraConfig(WithActor(context.Background(), authz.Actor{UserID: uuid.New()}), gen.GetCameraConfigRequestObject{CameraId: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	ok, isOK := res.(gen.GetCameraConfig200JSONResponse)
	if !isOK {
		t.Fatalf("unexpected response %T", res)
	}
	raw, _ := json.Marshal(ok)
	return gen.CameraConfig(ok), raw
}

func TestGetCameraConfigResponseShape(t *testing.T) {
	doc := map[string]any{
		"ffmpeg": map[string]any{"inputs": []any{
			map[string]any{"path": "rtsp://" + userinfo + "192.168.1.50:554/cam1?pass=hunter2", "roles": []any{"record"}},
			map[string]any{"path": "rtsp://" + userinfo + "192.168.1.50:554/cam1_sub", "roles": []any{"detect"}},
		}},
		"detect": map[string]any{"enabled": true, "width": 640.0, "height": 360.0, "fps": 5.0},
	}
	src := fakeCamConfigSource{ce: camConfigFixture(fakeFrigate{doc: doc})}
	cfg, raw := getCamConfig(t, src)

	if cfg.Origin.Engine != "frigate" || cfg.Origin.ServerName == nil || *cfg.Origin.ServerName != "Site A" {
		t.Fatalf("origin: %+v", cfg.Origin)
	}
	if cfg.Streams.Detection == nil || *cfg.Streams.Detection.Width != 640 || *cfg.Streams.Detection.Codec != "h264" {
		t.Fatalf("detection stream: %+v", cfg.Streams.Detection)
	}
	if cfg.Streams.Primary == nil || *cfg.Streams.Primary.Url != "rtsp://192.168.1.50:554/cam1" {
		t.Fatalf("primary stream: %+v", cfg.Streams.Primary)
	}
	if cfg.Streams.Recording != nil {
		t.Fatal("recording must be omitted when it is the primary stream")
	}
	if cfg.Device.Manufacturer == nil || *cfg.Device.Manufacturer != "Axis" {
		t.Fatalf("device: %+v", cfg.Device)
	}
	caps := make([]string, 0, len(cfg.Capabilities))
	for _, c := range cfg.Capabilities {
		caps = append(caps, string(c))
	}
	if got := strings.Join(caps, ","); got != "onvif,lpr,object_detection" {
		t.Fatalf("capabilities = %s", got)
	}
	if strings.Contains(string(raw), "hunter2") || strings.Contains(string(raw), "admin:") {
		t.Fatalf("credentials leaked: %s", raw)
	}
}

func TestGetCameraConfigEngineUnreachableStillAnswers(t *testing.T) {
	cases := map[string]frigate.Adapter{
		"no connection": nil,
		"read failure":  fakeFrigate{docErr: errors.New("dial tcp 10.0.0.1:5000: refused")},
	}
	for name, ad := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, _ := getCamConfig(t, fakeCamConfigSource{ce: camConfigFixture(ad)})
			if len(cfg.Sources) == 0 || cfg.Sources[0].Name != "frigate" || cfg.Sources[0].Status != gen.CameraConfigSourcesStatusUnavailable {
				t.Fatalf("sources: %+v", cfg.Sources)
			}
			if cfg.Identity.Name != "Front door" || cfg.Streams.Primary == nil || *cfg.Streams.Primary.Name != "cam1" {
				t.Fatalf("inventory data missing: %+v", cfg)
			}
		})
	}
}

func TestGetCameraConfigPermissionErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
	}{
		"forbidden": {access.ErrForbidden, http.StatusForbidden},
		"not found": {store.ErrNotFound, http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			h := &Handlers{camConfig: fakeCamConfigSource{err: tc.err}}
			_, err := h.GetCameraConfig(WithActor(context.Background(), authz.Actor{UserID: uuid.New()}), gen.GetCameraConfigRequestObject{CameraId: uuid.New()})
			if status, _, _ := statusFor(err); status != tc.status {
				t.Fatalf("status = %d, want %d (err %v)", status, tc.status, err)
			}
		})
	}
}

func TestGetCameraConfigRequiresActor(t *testing.T) {
	h := &Handlers{camConfig: fakeCamConfigSource{}}
	_, err := h.GetCameraConfig(context.Background(), gen.GetCameraConfigRequestObject{})
	if status, _, _ := statusFor(err); status != http.StatusUnauthorized {
		t.Fatalf("status = %d", status)
	}
}

type fakeCamConfigWriter struct {
	review      camconfig.Review
	apply       inventory.ConfigApplyResult
	err         error
	gotPatch    camconfig.Patch
	gotApply    inventory.ConfigApplyInput
	reviewCalls int
	applyCalls  int
}

func (f *fakeCamConfigWriter) ReviewCameraConfig(_ context.Context, _ authz.Actor, _ uuid.UUID, p camconfig.Patch) (camconfig.Review, error) {
	f.reviewCalls++
	f.gotPatch = p
	return f.review, f.err
}

func (f *fakeCamConfigWriter) ApplyCameraConfig(_ context.Context, _ authz.Actor, _ uuid.UUID, in inventory.ConfigApplyInput) (inventory.ConfigApplyResult, error) {
	f.applyCalls++
	f.gotApply = in
	return f.apply, f.err
}

func writerCtx() context.Context {
	return WithActor(context.Background(), authz.Actor{UserID: uuid.New()})
}

func TestReviewCameraConfigRejectsUnknownFieldsBeforeTheService(t *testing.T) {
	w := &fakeCamConfigWriter{}
	h := &Handlers{camConfigWrite: w}
	for name, patch := range map[string]map[string]any{
		"unknown field": {"streams": map[string]any{"primary": map[string]any{"width": 1}}},
		"nested field":  {"detection": map[string]any{"sensitivity": 3}},
		"empty":         {},
	} {
		t.Run(name, func(t *testing.T) {
			body := gen.ReviewCameraConfigJSONRequestBody(patch)
			_, err := h.ReviewCameraConfig(writerCtx(), gen.ReviewCameraConfigRequestObject{CameraId: uuid.New(), Body: &body})
			if status, _, _ := statusFor(err); status != http.StatusBadRequest {
				t.Fatalf("status = %d (%v)", status, err)
			}
		})
	}
	if w.reviewCalls != 0 {
		t.Fatalf("service reached %d times", w.reviewCalls)
	}
}

func TestReviewCameraConfigReturnsChangesAndImpact(t *testing.T) {
	w := &fakeCamConfigWriter{review: camconfig.Review{
		Changes: []camconfig.Change{{Path: "detection.enabled", Label: "detección de objetos", From: false, To: true}},
		Impact:  []camconfig.ImpactItem{{Code: camconfig.ImpactEngineReload, Message: "Frigate recargará configuración;"}},
	}}
	h := &Handlers{camConfigWrite: w}
	body := gen.ReviewCameraConfigJSONRequestBody{"detection": map[string]any{"enabled": true}}
	res, err := h.ReviewCameraConfig(writerCtx(), gen.ReviewCameraConfigRequestObject{CameraId: uuid.New(), Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	out := res.(gen.ReviewCameraConfig200JSONResponse)
	if len(out.Changes) != 1 || out.Changes[0].Path != "detection.enabled" || out.Changes[0].To != true || out.Impact[0].Message != "Frigate recargará configuración;" {
		t.Fatalf("review = %+v", out)
	}
	if w.gotPatch.Detection == nil || !*w.gotPatch.Detection.Enabled {
		t.Fatalf("patch = %+v", w.gotPatch)
	}
}

func TestApplyCameraConfigMapsOriginAndErrors(t *testing.T) {
	body := gen.ApplyCameraConfigJSONRequestBody{
		Patch: gen.CameraConfigPatch{"recording": map[string]any{"enabled": true}}, ExpectedRevision: 4,
	}
	origin := gen.CameraConfigOrigin("profile")
	body.Origin = &origin

	t.Run("passes revision and origin on", func(t *testing.T) {
		w := &fakeCamConfigWriter{apply: inventory.ConfigApplyResult{
			Outcome: camconfig.Outcome{Result: camconfig.ResultOK},
			State:   camconfig.State{DesiredRevision: 5, AppliedRevision: 5, SyncState: camconfig.SyncSynced, Drift: []camconfig.Drift{}},
		}}
		h := &Handlers{camConfigWrite: w}
		res, err := h.ApplyCameraConfig(writerCtx(), gen.ApplyCameraConfigRequestObject{CameraId: uuid.New(), Body: &body})
		if err != nil {
			t.Fatal(err)
		}
		if w.gotApply.ExpectedRevision != 4 || w.gotApply.Origin != "profile" {
			t.Fatalf("input = %+v", w.gotApply)
		}
		out := res.(gen.ApplyCameraConfig200JSONResponse)
		if out.Result != gen.CameraConfigApplyResultResultOk || out.State.SyncState != gen.CameraConfigStateSyncStateSynced || out.State.DesiredRevision != 5 {
			t.Fatalf("out = %+v", out)
		}
	})
	for name, tc := range map[string]struct {
		err    error
		status int
	}{
		"stale revision":    {inventory.ErrRevisionConflict, http.StatusConflict},
		"permission denied": {access.ErrForbidden, http.StatusForbidden},
		"unknown camera":    {store.ErrNotFound, http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			h := &Handlers{camConfigWrite: &fakeCamConfigWriter{err: tc.err}}
			_, err := h.ApplyCameraConfig(writerCtx(), gen.ApplyCameraConfigRequestObject{CameraId: uuid.New(), Body: &body})
			if status, _, _ := statusFor(err); status != tc.status {
				t.Fatalf("status = %d, want %d", status, tc.status)
			}
		})
	}
	t.Run("unknown field never reaches the service", func(t *testing.T) {
		w := &fakeCamConfigWriter{}
		h := &Handlers{camConfigWrite: w}
		bad := gen.ApplyCameraConfigJSONRequestBody{Patch: gen.CameraConfigPatch{"ptz": map[string]any{"home": true}}}
		_, err := h.ApplyCameraConfig(writerCtx(), gen.ApplyCameraConfigRequestObject{CameraId: uuid.New(), Body: &bad})
		if status, _, _ := statusFor(err); status != http.StatusBadRequest || w.applyCalls != 0 {
			t.Fatalf("status = %d calls = %d", status, w.applyCalls)
		}
	})
}

func TestGetCameraConfigCarriesState(t *testing.T) {
	ce := camConfigFixture(fakeFrigate{doc: map[string]any{"detect": map[string]any{"enabled": false}}})
	// Desired says detection on, the engine reports it off: a drift the response must show.
	ce.State = &db.CameraConfigState{
		Desired: json.RawMessage(`{"detection":{"enabled":true}}`), DesiredRevision: 3, AppliedRevision: 3, SyncState: "synced",
	}
	cfg, _ := getCamConfig(t, fakeCamConfigSource{ce: ce})
	if cfg.State.SyncState != gen.CameraConfigStateSyncStateDrift || cfg.State.DesiredRevision != 3 || len(cfg.State.Drift) != 1 || cfg.State.Drift[0].Path != "detection.enabled" {
		t.Fatalf("state = %+v", cfg.State)
	}
}

func TestApplyCameraConfigGuardsAnalyticsModules(t *testing.T) {
	apply := func(t *testing.T, ce inventory.CameraEngine, patch map[string]any) (*fakeCamConfigWriter, error) {
		t.Helper()
		w := &fakeCamConfigWriter{apply: inventory.ConfigApplyResult{Outcome: camconfig.Outcome{Result: camconfig.ResultOK}}}
		h := &Handlers{camConfigWrite: w, camConfig: fakeCamConfigSource{ce: ce}}
		body := gen.ApplyCameraConfigJSONRequestBody{Patch: patch, ExpectedRevision: 1}
		_, err := h.ApplyCameraConfig(writerCtx(), gen.ApplyCameraConfigRequestObject{CameraId: uuid.New(), Body: &body})
		return w, err
	}
	module := func(id string) map[string]any {
		return map[string]any{"analytics": map[string]any{"modules": map[string]any{id: map[string]any{"enabled": true}}}}
	}
	frigateDoc := fakeFrigate{doc: map[string]any{"lpr": map[string]any{"enabled": false}}}

	t.Run("frigate lpr is applied", func(t *testing.T) {
		w, err := apply(t, camConfigFixture(frigateDoc), module("lpr"))
		if err != nil || w.applyCalls != 1 {
			t.Fatalf("err = %v, calls = %d", err, w.applyCalls)
		}
	})
	t.Run("provision lpr is refused with the reason", func(t *testing.T) {
		ce := camConfigFixture(frigateDoc)
		ce.Camera.Tags = []string{"lpr_provision"}
		w, err := apply(t, ce, module("lpr"))
		var ve *inventory.ValidationError
		if !errors.As(err, &ve) || !strings.Contains(ve.Msg, "Se configura en la cámara") || w.applyCalls != 0 {
			t.Fatalf("err = %v, calls = %d", err, w.applyCalls)
		}
	})
	t.Run("module without evidence is refused", func(t *testing.T) {
		w, err := apply(t, camConfigFixture(frigateDoc), module("clothing_color"))
		var ve *inventory.ValidationError
		if !errors.As(err, &ve) || w.applyCalls != 0 {
			t.Fatalf("err = %v, calls = %d", err, w.applyCalls)
		}
	})
	t.Run("unknown module is a 400 before the service", func(t *testing.T) {
		w, err := apply(t, camConfigFixture(frigateDoc), module("teleport"))
		var ve *inventory.ValidationError
		if !errors.As(err, &ve) || w.applyCalls != 0 {
			t.Fatalf("err = %v, calls = %d", err, w.applyCalls)
		}
	})
}
