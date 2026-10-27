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
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/onvif"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// videoSecret is built at run time so the fixtures hold no credential-looking literal.
var videoSecret = "hunter" + "2-onvif"

type fakeDevice struct {
	streams    []onvif.ProfileStream
	streamsErr error
	setErr     error
	sets       int
}

func (f *fakeDevice) Streams(context.Context) ([]onvif.ProfileStream, error) {
	return f.streams, f.streamsErr
}
func (f *fakeDevice) Options(context.Context, string) (onvif.VideoOptions, error) {
	return onvif.VideoOptions{Codecs: []onvif.CodecOptions{{
		Codec: "H264", Resolutions: []onvif.VideoResolution{{Width: 1920, Height: 1080}, {Width: 640, Height: 360}},
		FPS: onvif.FloatRange{Min: 1, Max: 25}, Bitrate: onvif.IntRange{Min: 64, Max: 8192},
	}}}, nil
}
func (f *fakeDevice) Set(context.Context, string, onvif.VideoDesired) (onvif.ProfileStream, error) {
	f.sets++
	return onvif.ProfileStream{}, f.setErr
}

func newFakeDevice() *fakeDevice {
	return &fakeDevice{streams: []onvif.ProfileStream{
		{Token: "main", Name: "Main", Codec: "H264", Resolution: onvif.VideoResolution{Width: 1920, Height: 1080}, FPS: 15, BitrateKbps: 4096},
		{Token: "sub", Name: "Sub", Codec: "H264", Resolution: onvif.VideoResolution{Width: 640, Height: 360}, FPS: 5},
	}}
}

func linkFor(l camconfig.VideoLink, err error) videoLinkFunc {
	return func(context.Context, authz.Actor, db.GetCameraRow) (camconfig.VideoLink, error) { return l, err }
}

func videoHandlers(f videoLinkFunc) *Handlers {
	return &Handlers{camConfig: fakeCamConfigSource{ce: camConfigFixture(fakeFrigate{doc: map[string]any{
		"ffmpeg": map[string]any{"inputs": []any{
			map[string]any{"path": "rtsp://192.168.1.50:554/cam1", "roles": []any{"record"}},
			map[string]any{"path": "rtsp://192.168.1.50:554/cam1_sub", "roles": []any{"detect"}},
		}},
		"detect": map[string]any{"enabled": true, "width": 640.0, "height": 360.0, "fps": 5.0},
	}})}, camVideo: f}
}

func getOptions(t *testing.T, h *Handlers) (gen.CameraVideoOptions, []byte) {
	t.Helper()
	res, err := h.GetCameraVideoOptions(writerCtx(), gen.GetCameraVideoOptionsRequestObject{CameraId: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	ok := res.(gen.GetCameraVideoOptions200JSONResponse)
	raw, _ := json.Marshal(ok)
	return gen.CameraVideoOptions(ok), raw
}

func TestVideoOptionsShape(t *testing.T) {
	h := videoHandlers(linkFor(camconfig.VideoLink{Device: newFakeDevice(), PrimaryToken: "main", DetectionToken: "sub"}, nil))
	out, raw := getOptions(t, h)
	if !out.Available || out.Reason != nil || len(out.Streams) != 2 {
		t.Fatalf("%s", raw)
	}
	p := out.Streams[0]
	if p.Role != "primary" || !p.Available || *p.Current.Width != 1920 || *p.Current.Codec != "h264" || *p.Current.Fps != 15 {
		t.Fatalf("primary: %s", raw)
	}
	c := p.Codecs[0]
	if c.Codec != "h264" || len(c.Resolutions) != 2 || c.Fps.Max != 25 || c.BitrateKbps.Min != 64 {
		t.Fatalf("codec: %+v", c)
	}
	if string(p.ApplyVia.Resolution) != "onvif" {
		t.Fatalf("apply_via: %+v", p.ApplyVia)
	}
	for _, leak := range []string{"192.168", "rtsp:", "password", "stream_uri"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("%q leaked: %s", leak, raw)
		}
	}
}

func TestVideoOptionsDegradeWithReasonCodes(t *testing.T) {
	for _, reason := range []string{"no_onvif", "no_credentials", "unreachable", "unsupported"} {
		out, _ := getOptions(t, videoHandlers(linkFor(camconfig.VideoLink{Reason: reason}, nil)))
		if out.Available || out.Reason == nil || string(*out.Reason) != reason || len(out.Streams) != 2 || out.Streams[0].Available {
			t.Errorf("%s: %+v", reason, out)
		}
	}
	// A camera that fails mid-way is unreachable and the failure text (which may quote the
	// device or credentials) never reaches the answer.
	dev := newFakeDevice()
	dev.streamsErr = errors.New("Post http://admin:" + videoSecret + "@192.168.1.50/onvif: refused")
	out, raw := getOptions(t, videoHandlers(linkFor(camconfig.VideoLink{Device: dev, PrimaryToken: "main"}, nil)))
	if out.Available || string(*out.Reason) != "unreachable" || strings.Contains(string(raw), videoSecret) {
		t.Fatalf("%s", raw)
	}
}

func TestVideoOptionsPermissionDenied(t *testing.T) {
	h := videoHandlers(linkFor(camconfig.VideoLink{}, access.ErrForbidden))
	_, err := h.GetCameraVideoOptions(writerCtx(), gen.GetCameraVideoOptionsRequestObject{CameraId: uuid.New()})
	if status, _, _ := statusFor(err); status != http.StatusForbidden {
		t.Fatalf("status = %d (%v)", status, err)
	}
}

func TestConfigReadFillsPrimaryFromTheCamera(t *testing.T) {
	h := videoHandlers(linkFor(camconfig.VideoLink{Device: newFakeDevice(), PrimaryToken: "main", DetectionToken: "sub"}, nil))
	res, err := h.GetCameraConfig(writerCtx(), gen.GetCameraConfigRequestObject{CameraId: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	cfg := res.(gen.GetCameraConfig200JSONResponse)
	p := cfg.Streams.Primary
	if *p.Width != 1920 || *p.Height != 1080 || *p.Fps != 15 || *p.BitrateKbps != 4096 {
		t.Fatalf("primary: %+v", p)
	}
	// Frigate's detect values stay for the detection stream.
	if d := cfg.Streams.Detection; *d.Width != 640 || *d.Fps != 5 {
		t.Fatalf("detection: %+v", d)
	}
	// Without permission to use the credentials the read still works, without the values.
	h = videoHandlers(linkFor(camconfig.VideoLink{}, access.ErrForbidden))
	res, err = h.GetCameraConfig(writerCtx(), gen.GetCameraConfigRequestObject{CameraId: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	cfg = res.(gen.GetCameraConfig200JSONResponse)
	var onvifSource *string
	for _, s := range cfg.Sources {
		if s.Name == "onvif" && s.Detail != nil {
			onvifSource = s.Detail
		}
	}
	if cfg.Streams.Primary.Width != nil || onvifSource == nil || *onvifSource != "no_credentials" {
		t.Fatalf("degraded read: %+v %v", cfg.Streams.Primary, onvifSource)
	}
}

// videoWriter is a service that reviews and applies stream changes the way the inventory
// service does: it reaches the camera only through the linker the handler hands over.
type videoWriter struct {
	fakeCamConfigWriter
	cam     db.GetCameraRow
	adapter func(camconfig.VideoLink) camconfig.FrigateAdapter
	linked  int
}

func (w *videoWriter) ReviewCameraConfigWith(ctx context.Context, _ authz.Actor, _ uuid.UUID, p camconfig.Patch, link inventory.VideoLinker) (camconfig.Review, error) {
	l, err := link(ctx, w.cam)
	if err != nil {
		return camconfig.Review{}, err
	}
	w.linked++
	return w.adapter(l).Plan(ctx, camconfig.Camera{}, p)
}

func (w *videoWriter) ApplyCameraConfigWith(ctx context.Context, _ authz.Actor, _ uuid.UUID, in inventory.ConfigApplyInput, link inventory.VideoLinker) (inventory.ConfigApplyResult, error) {
	l, err := link(ctx, w.cam)
	if err != nil {
		return inventory.ConfigApplyResult{}, err
	}
	applyErr := w.adapter(l).Apply(ctx, in.Patch)
	out := camconfig.DecideOutcome(applyErr, camconfig.Camera{Sources: []camconfig.Source{{Name: "frigate", Status: camconfig.SourceOK}}}, in.Patch, in.Patch)
	return inventory.ConfigApplyResult{Outcome: out, State: camconfig.State{SyncState: out.SyncState, LastError: out.LastError, Drift: []camconfig.Drift{}}}, nil
}

func newVideoWriter() *videoWriter {
	return &videoWriter{adapter: func(l camconfig.VideoLink) camconfig.FrigateAdapter { return camconfig.FrigateAdapter{Video: &l} }}
}

func TestReviewStreamChangeReportsRestartAndRejectsUnsupported(t *testing.T) {
	dev := newFakeDevice()
	w := newVideoWriter()
	h := &Handlers{camConfigWrite: w, camVideo: linkFor(camconfig.VideoLink{Device: dev, PrimaryToken: "main", DetectionToken: "sub"}, nil)}
	body := gen.ReviewCameraConfigJSONRequestBody{"streams": map[string]any{"primary": map[string]any{
		"resolution": map[string]any{"width": 640, "height": 360}}}}
	res, err := h.ReviewCameraConfig(writerCtx(), gen.ReviewCameraConfigRequestObject{CameraId: uuid.New(), Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	out := res.(gen.ReviewCameraConfig200JSONResponse)
	if w.linked != 1 || len(out.Impact) != 1 || string(out.Impact[0].Code) != "stream_restart" ||
		out.Impact[0].Message != "Este cambio reiniciará el stream durante aproximadamente algunos segundos." {
		t.Fatalf("%+v", out)
	}
	if out.Changes[0].Path != "streams.primary.resolution" || out.Changes[0].To != "640×360" || len(out.Unsupported) != 0 {
		t.Fatalf("%+v", out)
	}
	bad := gen.ReviewCameraConfigJSONRequestBody{"streams": map[string]any{"primary": map[string]any{"fps": 60}}}
	res, _ = h.ReviewCameraConfig(writerCtx(), gen.ReviewCameraConfigRequestObject{CameraId: uuid.New(), Body: &bad})
	out = res.(gen.ReviewCameraConfig200JSONResponse)
	if len(out.Unsupported) != 1 || !strings.HasPrefix(out.Unsupported[0], "Stream principal: Los FPS deben estar entre") {
		t.Fatalf("%+v", out.Unsupported)
	}
	if dev.sets != 0 {
		t.Fatal("review wrote to the camera")
	}
}

func TestStreamChangesNeedTheCredentialPermissions(t *testing.T) {
	h := &Handlers{camConfigWrite: newVideoWriter(), camVideo: linkFor(camconfig.VideoLink{}, access.ErrForbidden)}
	body := gen.ReviewCameraConfigJSONRequestBody{"streams": map[string]any{"primary": map[string]any{"fps": 10}}}
	_, err := h.ReviewCameraConfig(writerCtx(), gen.ReviewCameraConfigRequestObject{CameraId: uuid.New(), Body: &body})
	if status, _, _ := statusFor(err); status != http.StatusForbidden {
		t.Fatalf("review status = %d", status)
	}
	ab := gen.ApplyCameraConfigJSONRequestBody{Patch: gen.CameraConfigPatch{"streams": map[string]any{"primary": map[string]any{"fps": 10}}}}
	_, err = h.ApplyCameraConfig(writerCtx(), gen.ApplyCameraConfigRequestObject{CameraId: uuid.New(), Body: &ab})
	if status, _, _ := statusFor(err); status != http.StatusForbidden {
		t.Fatalf("apply status = %d", status)
	}
}

func TestApplyStreamFailureEndsInErrorStateWithSanitizedText(t *testing.T) {
	dev := newFakeDevice()
	dev.setErr = errors.New("http://admin:" + videoSecret + "@192.168.1.50/onvif: 500")
	h := &Handlers{camConfigWrite: newVideoWriter(), camVideo: linkFor(camconfig.VideoLink{Device: dev, PrimaryToken: "main"}, nil)}
	ab := gen.ApplyCameraConfigJSONRequestBody{Patch: gen.CameraConfigPatch{"streams": map[string]any{"primary": map[string]any{"fps": 10}}}}
	res, err := h.ApplyCameraConfig(writerCtx(), gen.ApplyCameraConfigRequestObject{CameraId: uuid.New(), Body: &ab})
	if err != nil {
		t.Fatal(err)
	}
	out := res.(gen.ApplyCameraConfig200JSONResponse)
	raw, _ := json.Marshal(out)
	if out.Result != gen.CameraConfigApplyResultResultError || out.State.SyncState != gen.CameraConfigStateSyncStateError ||
		out.State.LastError == "" || dev.sets != 1 || strings.Contains(string(raw), videoSecret) || strings.Contains(string(raw), "192.168") {
		t.Fatalf("%s", raw)
	}
}

func TestDefaultVideoLinkDegradesBeforeAnyNetworkCall(t *testing.T) {
	h := &Handlers{}
	cam := camConfigFixture(nil).Camera
	l, err := h.cameraVideoLink(context.Background(), authz.Actor{}, cam)
	if err != nil || l.Device != nil || l.Reason != camconfig.VideoNoCredential {
		t.Fatalf("no inventory service: %+v %v", l, err)
	}
	cam.OnvifEndpoint = nil
	if l, _ = h.cameraVideoLink(context.Background(), authz.Actor{}, cam); l.Reason != camconfig.VideoNoONVIF {
		t.Fatalf("no endpoint: %+v", l)
	}
}

func TestLazyVideoDeviceFailureIsSanitized(t *testing.T) {
	d := &lazyVideoDevice{endpoint: "http://192.168.1.50/onvif/device_service", user: "admin", pass: videoSecret,
		transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("dial " + videoSecret) })}
	rep := camconfig.BuildVideoOptions(context.Background(), "c1", &camconfig.VideoLink{Device: d, PrimaryToken: "main"})
	raw, _ := json.Marshal(rep)
	if rep.Available || rep.Reason != camconfig.VideoUnreachable || strings.Contains(string(raw), videoSecret) {
		t.Fatalf("%s", raw)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fakeAgentVideo struct {
	registered bool
	err        error
	calls      []onvifdiscover.VideoDeviceRequest
}

func (f *fakeAgentVideo) HasRegisteredAgent(context.Context, authz.Actor, uuid.UUID) bool {
	return f.registered
}

func (f *fakeAgentVideo) ExecuteOnvifVideoDevice(_ context.Context, _ authz.Actor, _ uuid.UUID, req onvifdiscover.VideoDeviceRequest) (onvifdiscover.VideoDeviceResult, int, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return onvifdiscover.VideoDeviceResult{}, 0, f.err
	}
	return onvifdiscover.VideoDeviceResult{Success: true, Streams: []onvif.ProfileStream{{Token: "main"}}}, 200, nil
}

func TestVideoDevicePrefersTheAgentAndFallsBackToTheDirectDialWithoutOne(t *testing.T) {
	cam := db.GetCameraRow{ServerID: uuid.New()}
	agent := &fakeAgentVideo{registered: true}
	h := &Handlers{camAgentVideo: agent}
	dev := h.videoDeviceFor(context.Background(), authz.Actor{}, cam, "http://192.168.1.50/onvif/device_service", "admin", videoSecret)
	streams, err := dev.Streams(context.Background())
	if _, ok := dev.(*camconfig.AgentVideoDevice); !ok || err != nil || len(streams) != 1 || len(agent.calls) != 1 || agent.calls[0].Op != "profile_streams" {
		t.Fatalf("device %T streams %v err %v calls %+v", dev, streams, err, agent.calls)
	}

	agent.registered = false
	if dev := h.videoDeviceFor(context.Background(), authz.Actor{}, cam, "http://192.168.1.50/x", "admin", videoSecret); func() bool { _, ok := dev.(*lazyVideoDevice); return !ok }() {
		t.Fatalf("no agent: want the direct dial, got %T", dev)
	}
	if dev := (&Handlers{}).videoDeviceFor(context.Background(), authz.Actor{}, cam, "http://192.168.1.50/x", "admin", videoSecret); func() bool { _, ok := dev.(*lazyVideoDevice); return !ok }() {
		t.Fatalf("no provision service: want the direct dial, got %T", dev)
	}
}

func TestVideoDeviceThroughAnOldAgentReadsAsAgentOutdated(t *testing.T) {
	agent := &fakeAgentVideo{registered: true, err: provision.ErrAgentOpUnsupported}
	h := &Handlers{camAgentVideo: agent}
	dev := h.videoDeviceFor(context.Background(), authz.Actor{}, db.GetCameraRow{ServerID: uuid.New()}, "http://192.168.1.50/x", "admin", videoSecret)
	link := camconfig.VideoLink{Device: dev, PrimaryToken: "main"}
	rep := camconfig.BuildVideoOptions(context.Background(), "cam", &link)
	if rep.Available || rep.Reason != "agent_outdated" {
		t.Fatalf("report %+v", rep)
	}
	// Other agent failures keep reading as an unreachable camera.
	agent.err = errors.New("registered agent probe unavailable")
	rep = camconfig.BuildVideoOptions(context.Background(), "cam", &link)
	if rep.Reason != "unreachable" {
		t.Fatalf("report %+v", rep)
	}
}
