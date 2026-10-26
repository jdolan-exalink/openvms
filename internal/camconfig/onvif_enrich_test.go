package camconfig

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const mainURI, subURI = "rtsp://10.0.0.5/main", "rtsp://10.0.0.5/sub"

func enrichInv() Inventory {
	in := baseInv()
	in.ONVIFCapabilities = []byte(`{"main_stream_url":"` + mainURI + `","sub_stream_url":"` + subURI + `","media_profiles":[` +
		`{"token":"main","stream_uri":"` + mainURI + `"},{"token":"sub","stream_uri":"` + subURI + `"}]}`)
	return in
}

func TestProfileTokens(t *testing.T) {
	p, d := ProfileTokens(enrichInv())
	if p != "main" || d != "sub" {
		t.Fatalf("%q %q", p, d)
	}
	// Ambiguous (two profiles share the main URI) leaves the stream unmatched.
	in := baseInv()
	in.ONVIFCapabilities = []byte(`{"main_stream_url":"u","sub_stream_url":"u","media_profiles":[{"token":"a","stream_uri":"u"},{"token":"b","stream_uri":"u"}]}`)
	if p, d := ProfileTokens(in); p != "" || d != "" {
		t.Fatalf("ambiguous match: %q %q", p, d)
	}
	// A single-stream camera has no detection profile of its own.
	in.ONVIFCapabilities = []byte(`{"main_stream_url":"u","sub_stream_url":"u","media_profiles":[{"token":"a","stream_uri":"u"}]}`)
	if p, d := ProfileTokens(in); p != "a" || d != "" {
		t.Fatalf("%q %q", p, d)
	}
	in.ONVIFCapabilities = nil
	if p, d := ProfileTokens(in); p != "" || d != "" {
		t.Fatal("no caps")
	}
}

func readWith(t *testing.T, l *VideoLink) Camera {
	t.Helper()
	eng := fakeEngine{doc: parse(t, twoInputDoc), codecs: map[string]string{"101": "h265", "102": "h264"}}
	cam, err := FrigateAdapter{Engine: eng, Video: l}.Read(context.Background(), enrichInv())
	if err != nil {
		t.Fatal(err)
	}
	return cam
}

func TestReadFillsPrimaryFromONVIFAndKeepsFrigateDetect(t *testing.T) {
	cam := readWith(t, link(newFakeVideo()))
	p := cam.Streams.Primary
	if p.Width != 1920 || p.Height != 1080 || p.FPS != 15 || p.BitrateKbps != 4096 || p.GOP != 30 {
		t.Fatalf("primary: %+v", p)
	}
	d := cam.Streams.Detection
	if d.Width != 640 || d.Height != 360 || d.FPS != 5 || d.BitrateKbps != 512 {
		t.Fatalf("detection: %+v", d)
	}
	var found bool
	for _, s := range cam.Sources {
		found = found || (s.Name == "onvif" && s.Status == SourceOK)
	}
	if !found {
		t.Fatalf("sources: %+v", cam.Sources)
	}
}

func TestReadDegradesWithAReasonAndNeverInvents(t *testing.T) {
	for _, l := range []*VideoLink{{Reason: VideoNoCredential}, {Reason: VideoNoONVIF}} {
		cam := readWith(t, l)
		if cam.Streams.Primary.Width != 0 || cam.Streams.Primary.FPS != 0 {
			t.Fatalf("invented: %+v", cam.Streams.Primary)
		}
		last := cam.Sources[len(cam.Sources)-1]
		if last.Name != "onvif" || last.Status != SourceUnavailable || last.Detail != l.Reason {
			t.Fatalf("source: %+v", last)
		}
	}
	f := newFakeVideo()
	f.streamsErr = errors.New("dial tcp 10.0.0.5:80: refused")
	cam := readWith(t, link(f))
	last := cam.Sources[len(cam.Sources)-1]
	if last.Detail != VideoUnreachable || strings.Contains(last.Detail, "10.0.0.5") || cam.Streams.Primary.Width != 0 {
		t.Fatalf("%+v", last)
	}
	// Unmatched primary profile: reachable, but nothing filled.
	cam = readWith(t, &VideoLink{Device: newFakeVideo()})
	if cam.Streams.Primary.Width != 0 {
		t.Fatalf("filled without a match: %+v", cam.Streams.Primary)
	}
	// No link at all leaves the read exactly as before.
	if cam = readWith(t, nil); len(cam.Sources) != 2 {
		t.Fatalf("sources: %+v", cam.Sources)
	}
}

func TestBuildVideoOptionsShapeAndDegradation(t *testing.T) {
	rep := BuildVideoOptions(context.Background(), "c1", link(newFakeVideo()))
	if !rep.Available || len(rep.Streams) != 2 {
		t.Fatalf("%+v", rep)
	}
	p := rep.Streams[0]
	if p.Role != "primary" || p.ProfileToken != "main" || p.Current.Width != 1920 || p.Current.Codec != "h264" || len(p.Codecs) != 1 {
		t.Fatalf("%+v", p)
	}
	c := p.Codecs[0]
	if c.Codec != "h264" || len(c.Resolutions) != 3 || c.FPS.Max != 25 || c.BitrateKbps.Max != 8192 || c.GOP.Max != 120 || c.BitrateModes == nil {
		t.Fatalf("%+v", c)
	}
	if p.ApplyVia["resolution"] != ViaONVIF || rep.Streams[1].ApplyVia["gop"] != ViaONVIF {
		t.Fatalf("via: %+v", p.ApplyVia)
	}
	for _, reason := range []string{VideoNoONVIF, VideoNoCredential, VideoUnreachable, VideoUnsupported} {
		r := BuildVideoOptions(context.Background(), "c1", &VideoLink{Reason: reason})
		if r.Available || r.Reason != reason || len(r.Streams) != 2 || r.Streams[0].Available || r.Streams[0].Reason != reason {
			t.Errorf("%s: %+v", reason, r)
		}
		if r.Streams[1].ApplyVia["resolution"] != ViaFrigate || r.Streams[0].ApplyVia["resolution"] != ViaNone {
			t.Errorf("%s via: %+v", reason, r.Streams)
		}
	}
	f := newFakeVideo()
	f.streamsErr = errors.New("boom")
	if r := BuildVideoOptions(context.Background(), "c1", link(f)); r.Reason != VideoUnreachable {
		t.Fatalf("%+v", r)
	}
	f = newFakeVideo()
	f.options.Codecs = nil
	if r := BuildVideoOptions(context.Background(), "c1", link(f)); r.Available || r.Reason != VideoUnsupported {
		t.Fatalf("%+v", r)
	}
}
