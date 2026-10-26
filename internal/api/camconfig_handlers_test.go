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
