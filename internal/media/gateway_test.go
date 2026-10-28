package media

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func serverOfKind(kind string) db.FrigateServer { return db.FrigateServer{Kind: kind} }

// TestLiveStreamPicksSubOrMain covers the ordinary cases: sub for grids (default and
// quality=sub), main for the single view (quality=main).
func TestLiveStreamPicksSubOrMain(t *testing.T) {
	cam := Camera{LiveStream: "cam_sub", HQStream: "cam_main"}
	if s, err := liveStream(cam, ""); err != nil || s != "cam_sub" {
		t.Fatalf("default quality: got %q, %v", s, err)
	}
	if s, err := liveStream(cam, "sub"); err != nil || s != "cam_sub" {
		t.Fatalf("quality=sub: got %q, %v", s, err)
	}
	if s, err := liveStream(cam, "main"); err != nil || s != "cam_main" {
		t.Fatalf("quality=main: got %q, %v", s, err)
	}
}

// TestLiveStreamFallsBackToHQWhenLiveEmpty covers a camera with only one go2rtc stream
// (LiveStream == HQStream, or LiveStream unset): the sub-quality request still gets it.
func TestLiveStreamFallsBackToHQWhenLiveEmpty(t *testing.T) {
	cam := Camera{HQStream: "cam_live"}
	if s, err := liveStream(cam, ""); err != nil || s != "cam_live" {
		t.Fatalf("got %q, %v", s, err)
	}
}

// TestLiveStreamNoRestreamFailsFast is the fix under test: a camera whose discovery
// (frigate.pickStreams) found no matching go2rtc stream must fail with errNoStream
// instead of the old behaviour of dialing Frigate with the bare camera name.
func TestLiveStreamNoRestreamFailsFast(t *testing.T) {
	cam := Camera{RemoteName: "Banco"}
	s, err := liveStream(cam, "")
	if s != "" || !errors.Is(err, errNoStream) {
		t.Fatalf("want (\"\", errNoStream), got (%q, %v)", s, err)
	}
}

func TestLiveKindRefusal(t *testing.T) {
	var recorder Camera
	recorder.Server.Kind = "provision_nvr"
	code, msg, refused := liveKindRefusal(recorder, false)
	if !refused || code != CodeVideoUnavailable || msg == "" {
		t.Fatalf("recorder camera without a video gateway: got (%q, %q, %v), want a video_unavailable refusal", code, msg, refused)
	}
	if _, _, refused := liveKindRefusal(recorder, true); refused {
		t.Error("a recorder camera must not be refused when the video gateway is configured")
	}
	for name, cam := range map[string]Camera{"frigate": {Server: serverOfKind("frigate")}, "kind not set (partial row)": {}} {
		if _, _, refused := liveKindRefusal(cam, false); refused {
			t.Errorf("%s must not be refused", name)
		}
	}
}

func TestLiveSignalRefusal(t *testing.T) {
	zero, five := float32(0), float32(5)
	code, msg, refused := liveSignalRefusal(Camera{Status: "offline", FPS: &zero})
	if !refused || code != CodeCameraNoSignal || msg == "" {
		t.Fatalf("no signal: got (%q, %q, %v)", code, msg, refused)
	}
	for name, cam := range map[string]Camera{
		"unknown state (no stats yet)": {Status: "unknown"},
		"unknown fps":                  {Status: "offline"},
		"online":                       {Status: "online", FPS: &five},
		"server unreachable":           {Status: "unknown", FPS: &zero},
	} {
		if _, _, refused := liveSignalRefusal(cam); refused {
			t.Errorf("%s must not be refused", name)
		}
	}
}

func TestReplayWindow(t *testing.T) {
	req := func(q string) *http.Request { return httptest.NewRequest(http.MethodGet, "/x?"+q, nil) }
	start, end, ok := replayWindow(req("start=1792000000&end=1792003600"))
	if !ok || !start.Equal(time.Unix(1792000000, 0).UTC()) || end.Sub(start) != time.Hour {
		t.Fatalf("a valid window: %v %v %v", start, end, ok)
	}
	if _, _, ok := replayWindow(req("start=1792000000.5&end=1792003600.9")); !ok {
		t.Fatal("fractional seconds are accepted")
	}
	for name, q := range map[string]string{
		"empty":       "",
		"no end":      "start=1792000000",
		"not numbers": "start=a&end=b",
		"equal":       "start=1792000000&end=1792000000",
		"backwards":   "start=1792003600&end=1792000000",
		"a day plus":  "start=1792000000&end=1792086401",
		"too early":   "start=5&end=100",
		"too late":    "start=1792000000&end=9999999999",
	} {
		if _, _, ok := replayWindow(req(q)); ok {
			t.Errorf("%s must be refused", name)
		}
	}
	if _, _, ok := replayWindow(req("start=1792000000&end=1792086400")); !ok {
		t.Error("exactly a day is allowed")
	}
}
