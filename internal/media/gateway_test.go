package media

import (
	"errors"
	"testing"
)

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
