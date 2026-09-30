package frigate

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/frigatemock"
)

func startEditableMock(t *testing.T, version string) (*httptest.Server, *frigatemock.Server) {
	t.Helper()
	cams, _ := frigatemock.ParseCameras("acceso_norte+lpr:entrada|salida,plaza:centro")
	srv := &frigatemock.Server{Version: version, Cameras: cams, Store: frigatemock.NewStore(10), User: "vms", Password: "pw", RequireAuth: true, StartedAt: time.Now()}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, srv
}

func connectMock(t *testing.T, ts *httptest.Server) Adapter {
	t.Helper()
	a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, Username: "vms", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestApplyCameraPatchLiveAndRestartSections(t *testing.T) {
	ts, mock := startEditableMock(t, "0.17.2-mock")
	a := connectMock(t, ts)
	ctx := context.Background()

	res, err := a.ApplyCameraPatch(ctx, "acceso_norte", map[string]any{
		"detect": map[string]any{"fps": 10.0},
		"onvif":  map[string]any{"port": 8080.0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Section != "detect" || !res[0].AppliedLive || res[0].RequiresRestart {
		t.Fatalf("detect must apply live: %+v", res)
	}
	if res[1].Section != "onvif" || res[1].AppliedLive || !res[1].RequiresRestart {
		t.Fatalf("onvif must need a restart: %+v", res)
	}
	calls := mock.SetCalls()
	if len(calls) != 2 {
		t.Fatalf("want one request per section, got %d", len(calls))
	}
	if calls[0].UpdateTopic != "config/cameras/acceso_norte/detect" || calls[0].RequiresRestart == nil || *calls[0].RequiresRestart != 0 {
		t.Errorf("detect call: %+v", calls[0])
	}
	if calls[1].UpdateTopic != "" || calls[1].RequiresRestart == nil || *calls[1].RequiresRestart != 1 {
		t.Errorf("onvif call: %+v", calls[1])
	}
	cfg, err := a.CameraConfig(ctx, "acceso_norte", false)
	if err != nil {
		t.Fatal(err)
	}
	if fps := cfg["detect"].(map[string]any)["fps"]; fps != 10.0 {
		t.Errorf("fps = %v", fps)
	}
	// Siblings inside the section survive the merge.
	if w := cfg["detect"].(map[string]any)["width"]; w != 1280.0 {
		t.Errorf("width lost: %v", w)
	}
}

func TestLegacyUpdateAppliesLive(t *testing.T) {
	ts, mock := startEditableMock(t, "0.17.2-mock")
	a := connectMock(t, ts)
	f := false
	if _, err := a.UpdateCameraConfig(context.Background(), "acceso_norte", CameraFrigateConfigUpdate{DetectEnabled: &f, TrackedObjects: []string{"car"}}); err != nil {
		t.Fatal(err)
	}
	for _, c := range mock.SetCalls() {
		if c.RequiresRestart == nil || *c.RequiresRestart != 0 || c.UpdateTopic == "" {
			t.Errorf("legacy update must be live with an update_topic: %+v", c)
		}
	}
}

func TestConfigEditGate(t *testing.T) {
	ts, mock := startEditableMock(t, "0.15.1")
	a := connectMock(t, ts)
	ctx := context.Background()
	if a.ConfigEditable() {
		t.Fatal("0.15 must not be editable")
	}
	if _, err := a.ApplyCameraPatch(ctx, "acceso_norte", map[string]any{"detect": map[string]any{"fps": 5.0}}); !errors.Is(err, ErrConfigEditUnsupported) {
		t.Errorf("patch on 0.15: %v", err)
	}
	if err := a.SaveRawConfig(ctx, "cameras: {}", false); !errors.Is(err, ErrConfigEditUnsupported) {
		t.Errorf("save on 0.15: %v", err)
	}
	if len(mock.SetCalls()) != 0 {
		t.Error("nothing may reach Frigate")
	}
	// Reads still work.
	if _, err := a.CameraConfig(ctx, "acceso_norte", false); err != nil {
		t.Errorf("read on 0.15: %v", err)
	}
}

func TestPatchOn016UsesQueryParamsAndFlagsRestart(t *testing.T) {
	ts, mock := startEditableMock(t, "0.16.0")
	a := connectMock(t, ts)
	res, err := a.ApplyCameraPatch(context.Background(), "acceso_norte", map[string]any{"detect": map[string]any{"fps": 8.0}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].AppliedLive || !res[0].RequiresRestart {
		t.Fatalf("0.16 cannot apply live: %+v", res)
	}
	c := mock.SetCalls()[0]
	if c.Query["cameras.acceso_norte.detect.fps"] != "8" || c.UpdateTopic != "" {
		t.Errorf("call: %+v", c)
	}
	// Lists of objects cannot be expressed as query parameters.
	_, err = a.ApplyCameraPatch(context.Background(), "acceso_norte", map[string]any{"ffmpeg": map[string]any{"inputs": []any{map[string]any{"path": "rtsp://x"}}}})
	var cv *ConfigValidationError
	if !errors.As(err, &cv) {
		t.Errorf("want validation error, got %v", err)
	}
}

func TestRedactedValuesAreNeverWritten(t *testing.T) {
	ts, mock := startEditableMock(t, "0.17.2-mock")
	a := connectMock(t, ts)
	ctx := context.Background()
	cfg, err := a.CameraConfig(ctx, "acceso_norte", false)
	if err != nil {
		t.Fatal(err)
	}
	masked := cfg["ffmpeg"].(map[string]any)["inputs"].([]any)[0].(map[string]any)["path"].(string)
	if !strings.Contains(masked, "://*:*@") {
		t.Fatalf("the mock must mask credentials, got %q", masked)
	}
	_, err = a.ApplyCameraPatch(ctx, "acceso_norte", map[string]any{"ffmpeg": map[string]any{"inputs": []any{map[string]any{"path": masked}}}})
	var cv *ConfigValidationError
	if !errors.As(err, &cv) {
		t.Fatalf("masked path must be rejected, got %v", err)
	}
	if _, err = a.ApplyCameraPatch(ctx, "acceso_norte", map[string]any{"onvif": map[string]any{"password": "*"}}); !errors.As(err, &cv) {
		t.Fatalf("masked password must be rejected, got %v", err)
	}
	if len(mock.SetCalls()) != 0 {
		t.Error("nothing may reach Frigate")
	}
	if !strings.Contains(mock.ConfigYAML(), "s3cret") {
		t.Error("the real secret must be untouched")
	}
}

func TestCameraConfigSecretsMerge(t *testing.T) {
	ts, _ := startEditableMock(t, "0.17.2-mock")
	a := connectMock(t, ts)
	cfg, err := a.CameraConfig(context.Background(), "acceso_norte", true)
	if err != nil {
		t.Fatal(err)
	}
	path := cfg["ffmpeg"].(map[string]any)["inputs"].([]any)[0].(map[string]any)["path"].(string)
	if !strings.Contains(path, "viewer:s3cret@") {
		t.Errorf("path = %q", path)
	}
	if pw := cfg["onvif"].(map[string]any)["password"]; pw != "onvifpass" {
		t.Errorf("onvif password = %v", pw)
	}
}

func TestSaveRawConfigValidationAndSchemaTrim(t *testing.T) {
	ts, mock := startEditableMock(t, "0.17.2-mock")
	a := connectMock(t, ts)
	ctx := context.Background()
	raw, err := a.RawConfig(ctx)
	if err != nil || !strings.Contains(raw, "acceso_norte") {
		t.Fatalf("raw: %v %q", err, raw)
	}
	err = a.SaveRawConfig(ctx, "cameras:\n  x:\n    ffmpeg:\n      inputs: []\n", false)
	var cv *ConfigValidationError
	if !errors.As(err, &cv) || !strings.Contains(cv.Message, "ffmpeg.inputs") {
		t.Fatalf("want Frigate's validation message, got %v", err)
	}
	if err := a.SaveRawConfig(ctx, raw, false); err != nil {
		t.Fatal(err)
	}
	if calls := mock.SaveCalls(); len(calls) != 1 || calls[0].Option != "saveonly" {
		t.Errorf("save calls: %+v", calls)
	}
	full, err := a.ConfigSchema(ctx)
	if err != nil {
		t.Fatal(err)
	}
	trimmed, err := TrimCameraSchema(full)
	if err != nil {
		t.Fatal(err)
	}
	s := string(trimmed)
	if !strings.Contains(s, "CameraConfig") || !strings.Contains(s, "DetectConfig") || strings.Contains(s, "MqttConfig") {
		t.Errorf("trimmed schema keeps only CameraConfig and what it references: %s", s)
	}
}
