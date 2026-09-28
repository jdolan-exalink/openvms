package frigate

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/frigatemock"
)

func liveCfg(streams map[string]string) cameraConfig {
	return cameraConfig{Live: &struct {
		Streams map[string]string `json:"streams"`
	}{Streams: streams}}
}

// TestPickStreamsConfiguredAndExists: the common, working case (e.g. the home Frigate):
// config declares live.streams and go2rtc actually has every referenced stream.
func TestPickStreamsConfiguredAndExists(t *testing.T) {
	cc := liveCfg(map[string]string{"Sub Stream": "Portones_sub", "Main Stream": "Portones_main"})
	available := map[string]bool{"Portones_sub": true, "Portones_main": true}
	live, hq := pickStreams("Portones", cc, available)
	if live != "Portones_sub" || hq != "Portones_main" {
		t.Fatalf("got live=%q hq=%q", live, hq)
	}
}

// TestPickStreamsConfiguredButMissing reproduces the helvecia bug: config (or its absence)
// points at a name go2rtc doesn't serve, so it must not be trusted blindly.
func TestPickStreamsConfiguredButMissing(t *testing.T) {
	cc := liveCfg(map[string]string{"Sub Stream": "Banco", "Main Stream": "Banco"})
	available := map[string]bool{"Banco_sub": true, "Banco_main": true}
	live, hq := pickStreams("Banco", cc, available)
	if live != "Banco_sub" || hq != "Banco_main" {
		t.Fatalf("configured-but-missing streams should resolve via go2rtc naming, got live=%q hq=%q", live, hq)
	}
}

// TestPickStreamsNoConfigNamingConvention: no live.streams at all, resolved purely from
// go2rtc's own <camera>_sub / <camera>_main convention.
func TestPickStreamsNoConfigNamingConvention(t *testing.T) {
	cc := cameraConfig{}
	available := map[string]bool{"Quincho_sub": true, "Quincho_main": true, "birdseye": true}
	live, hq := pickStreams("Quincho", cc, available)
	if live != "Quincho_sub" || hq != "Quincho_main" {
		t.Fatalf("got live=%q hq=%q", live, hq)
	}
}

// TestPickStreamsSingleLiveStream covers cameras with one go2rtc stream shared by both
// qualities (e.g. real "Cochera_live" on the home server), no _sub/_main pair.
func TestPickStreamsSingleLiveStream(t *testing.T) {
	cc := cameraConfig{}
	available := map[string]bool{"Cochera_live": true}
	live, hq := pickStreams("Cochera", cc, available)
	if live != "Cochera_live" || hq != "Cochera_live" {
		t.Fatalf("got live=%q hq=%q", live, hq)
	}
}

// TestPickStreamsCaseInsensitive: go2rtc stream names are matched ignoring case.
func TestPickStreamsCaseInsensitive(t *testing.T) {
	cc := cameraConfig{}
	available := map[string]bool{"banco_sub": true, "BANCO_MAIN": true}
	live, hq := pickStreams("Banco", cc, available)
	if live != "banco_sub" || hq != "BANCO_MAIN" {
		t.Fatalf("case-insensitive match failed: live=%q hq=%q", live, hq)
	}
}

// TestPickStreamsNothingMatches: this is the actual helvecia symptom once go2rtc has no
// restream at all for the camera. Both streams must be left empty, not fall back to the
// bare camera name (which is exactly the bug being fixed).
func TestPickStreamsNothingMatches(t *testing.T) {
	cc := cameraConfig{}
	available := map[string]bool{"other_camera_sub": true}
	live, hq := pickStreams("Banco", cc, available)
	if live != "" || hq != "" {
		t.Fatalf("want empty streams, got live=%q hq=%q", live, hq)
	}
}

// TestPickStreamsGo2RTCUnavailable: available == nil means the go2rtc endpoint failed
// (older Frigate, go2rtc disabled, 404). Discovery must not break: keep the legacy,
// unchecked behaviour instead of dropping streams that might well be correct.
func TestPickStreamsGo2RTCUnavailable(t *testing.T) {
	cc := cameraConfig{}
	live, hq := pickStreams("Banco", cc, nil)
	if live != "Banco" || hq != "Banco" {
		t.Fatalf("legacy fallback: want live=hq=%q, got live=%q hq=%q", "Banco", live, hq)
	}
}

// startMockWithGo2RTC starts a mock Frigate whose /api/go2rtc/streams reports exactly
// streamNames; nil disables the endpoint (404), simulating an older Frigate or go2rtc off.
func startMockWithGo2RTC(t *testing.T, spec string, streamNames []string) *httptest.Server {
	t.Helper()
	cams, err := frigatemock.ParseCameras(spec)
	if err != nil {
		t.Fatal(err)
	}
	store := frigatemock.NewStore(10)
	srv := &frigatemock.Server{Version: "0.18.0-mock", Cameras: cams, Store: store, RequireAuth: false, StartedAt: time.Now(), Go2RTCStreams: streamNames}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestListCamerasResolvesAgainstGo2RTC is the end-to-end reproduction of the helvecia bug:
// live.streams (and the no-config fallback) name the bare camera, which go2rtc does not
// serve; only the actual go2rtc stream names must be used.
func TestListCamerasResolvesAgainstGo2RTC(t *testing.T) {
	ts := startMockWithGo2RTC(t, "Banco", []string{"Banco_sub", "Banco_main"})
	a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	cams, err := a.ListCameras(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 1 || cams[0].LiveStream != "Banco_sub" || cams[0].HQStream != "Banco_main" {
		t.Fatalf("unexpected cameras: %+v", cams)
	}
}

// TestListCamerasNoGo2RTCRestream: go2rtc has streams, but none for this camera. The
// camera must come back with empty LiveStream/HQStream rather than the bare camera name.
func TestListCamerasNoGo2RTCRestream(t *testing.T) {
	ts := startMockWithGo2RTC(t, "Banco", []string{"otra_camara_main"})
	a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	cams, err := a.ListCameras(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 1 || cams[0].LiveStream != "" || cams[0].HQStream != "" {
		t.Fatalf("unexpected cameras: %+v", cams)
	}
}

// TestListCamerasGo2RTCEndpointMissing: the endpoint 404s (older Frigate / go2rtc
// disabled); discovery must not break and must keep trusting live.streams unchecked, as
// before this fix. (frigatemock's /api/config always declares live.streams
// main=<camera>, sub=<camera>_sub, so the legacy selection picks "_sub" for live.)
func TestListCamerasGo2RTCEndpointMissing(t *testing.T) {
	ts := startMockWithGo2RTC(t, "Banco", nil)
	a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	cams, err := a.ListCameras(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 1 || cams[0].LiveStream != "Banco_sub" || cams[0].HQStream != "Banco" {
		t.Fatalf("unexpected cameras: %+v", cams)
	}
}
