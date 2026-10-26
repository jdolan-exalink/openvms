package camconfig

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/onvif"
)

// fakeVideo is an in-memory ONVIF device: two profiles, H.264 up to 1920x1080 and 15 fps.
type fakeVideo struct {
	streams    []onvif.ProfileStream
	options    onvif.VideoOptions
	streamsErr error
	setErr     error
	sets       []string
	desired    []onvif.VideoDesired
}

func newFakeVideo() *fakeVideo {
	return &fakeVideo{
		streams: []onvif.ProfileStream{
			{Token: "main", Name: "Main", Codec: "H264", Resolution: onvif.VideoResolution{Width: 1920, Height: 1080}, FPS: 15, BitrateKbps: 4096, GOP: 30},
			{Token: "sub", Name: "Sub", Codec: "H264", Resolution: onvif.VideoResolution{Width: 640, Height: 360}, FPS: 5, BitrateKbps: 512, GOP: 10},
		},
		options: onvif.VideoOptions{Codecs: []onvif.CodecOptions{{
			Codec:       "H264",
			Resolutions: []onvif.VideoResolution{{Width: 1920, Height: 1080}, {Width: 1280, Height: 720}, {Width: 640, Height: 360}},
			FPS:         onvif.FloatRange{Min: 1, Max: 25}, Bitrate: onvif.IntRange{Min: 64, Max: 8192}, GOP: onvif.IntRange{Min: 1, Max: 120},
		}}},
	}
}

func (f *fakeVideo) Streams(context.Context) ([]onvif.ProfileStream, error) {
	return f.streams, f.streamsErr
}
func (f *fakeVideo) Options(context.Context, string) (onvif.VideoOptions, error) {
	return f.options, nil
}
func (f *fakeVideo) Set(_ context.Context, token string, d onvif.VideoDesired) (onvif.ProfileStream, error) {
	f.sets = append(f.sets, token)
	f.desired = append(f.desired, d)
	return onvif.ProfileStream{}, f.setErr
}

func ip(n int) *int         { return &n }
func fp(n float64) *float64 { return &n }
func sp(s string) *string   { return &s }
func link(f VideoDevice) *VideoLink {
	return &VideoLink{Device: f, PrimaryToken: "main", DetectionToken: "sub"}
}

func TestParsePatchAcceptsAndNormalizesStreams(t *testing.T) {
	p, err := ParsePatch([]byte(`{"streams":{"primary":{"resolution":{"width":1280,"height":720},"fps":10,"codec":" H265 ","bitrate_kbps":2048,"gop":20}}}`))
	if err != nil {
		t.Fatal(err)
	}
	v := p.Streams.Primary
	if *v.Codec != "h265" || v.Resolution.Width != 1280 || *v.FPS != 10 || *v.BitrateKbps != 2048 || *v.GOP != 20 {
		t.Fatalf("%+v", v)
	}
	for _, bad := range []string{
		`{"streams":{"primary":{"fps":0}}}`, `{"streams":{"primary":{"codec":"vp9"}}}`,
		`{"streams":{"primary":{"resolution":{"width":0,"height":720}}}}`, `{"streams":{"primary":{"bitrate_kbps":-1}}}`,
		`{"streams":{"recording":{"fps":5}}}`, `{"streams":{"primary":{"quality":3}}}`, `{"streams":{}}`,
	} {
		if _, err := ParsePatch([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestMergeStreamsKeepsOlderFields(t *testing.T) {
	a := Patch{Streams: &StreamsPatch{Primary: &VideoPatch{FPS: fp(10), GOP: ip(20)}}}
	b := Patch{Streams: &StreamsPatch{Primary: &VideoPatch{FPS: fp(12)}, Detection: &VideoPatch{FPS: fp(5)}}}
	m := Merge(a, b).Streams
	if *m.Primary.FPS != 12 || *m.Primary.GOP != 20 || *m.Detection.FPS != 5 {
		t.Fatalf("%+v %+v", m.Primary, m.Detection)
	}
}

func TestPlanVideoListsChangesAndTheRestartSentence(t *testing.T) {
	f := newFakeVideo()
	a := FrigateAdapter{Video: link(f)}
	rev, err := a.Plan(context.Background(), Camera{}, Patch{Streams: &StreamsPatch{Primary: &VideoPatch{
		Resolution: &VideoResolution{Width: 1280, Height: 720}, FPS: fp(15), Codec: sp("h264"), BitrateKbps: ip(2048)}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Unsupported) != 0 {
		t.Fatalf("unsupported: %v", rev.Unsupported)
	}
	paths := []string{}
	for _, c := range rev.Changes {
		paths = append(paths, c.Path)
	}
	// FPS and codec equal the current values: they are not changes.
	if !reflect.DeepEqual(paths, []string{"streams.primary.resolution", "streams.primary.bitrate_kbps"}) {
		t.Fatalf("paths = %v", paths)
	}
	if rev.Changes[0].From != "1920×1080" || rev.Changes[0].To != "1280×720" {
		t.Fatalf("change: %+v", rev.Changes[0])
	}
	if len(rev.Impact) != 1 || rev.Impact[0].Code != ImpactStreamRestart ||
		rev.Impact[0].Message != "Este cambio reiniciará el stream durante aproximadamente algunos segundos." {
		t.Fatalf("impact: %+v", rev.Impact)
	}
	if len(f.sets) != 0 {
		t.Fatal("review wrote to the device")
	}
}

func TestPlanVideoRejectsUnsupportedCombinationsInSpanish(t *testing.T) {
	a := FrigateAdapter{Video: link(newFakeVideo())}
	rev, _ := a.Plan(context.Background(), Camera{}, Patch{Streams: &StreamsPatch{Primary: &VideoPatch{
		Resolution: &VideoResolution{Width: 3840, Height: 2160}, FPS: fp(60), BitrateKbps: ip(99999)}}})
	if len(rev.Unsupported) != 3 {
		t.Fatalf("unsupported: %v", rev.Unsupported)
	}
	for _, u := range rev.Unsupported {
		if !strings.HasPrefix(u, "Stream principal: ") {
			t.Errorf("not a plain Spanish reason: %q", u)
		}
	}
	rev, _ = a.Plan(context.Background(), Camera{}, Patch{Streams: &StreamsPatch{Primary: &VideoPatch{Codec: sp("h265")}}})
	if len(rev.Unsupported) != 1 || !strings.Contains(rev.Unsupported[0], "H265") {
		t.Fatalf("codec not mentioned: %v", rev.Unsupported)
	}
}

func TestPlanVideoDegradesWithoutDevice(t *testing.T) {
	for reason, want := range map[string]string{
		VideoNoONVIF: "ONVIF configurado", VideoNoCredential: "credenciales", VideoUnreachable: "No se pudo consultar",
	} {
		a := FrigateAdapter{Video: &VideoLink{Reason: reason}}
		rev, _ := a.Plan(context.Background(), Camera{}, Patch{Streams: &StreamsPatch{Primary: &VideoPatch{FPS: fp(10)}}})
		if len(rev.Unsupported) != 1 || !strings.Contains(rev.Unsupported[0], want) || len(rev.Changes) != 0 {
			t.Errorf("%s: %+v", reason, rev)
		}
	}
	// An unreadable device and an unidentified profile are rejections, never errors.
	f := newFakeVideo()
	f.streamsErr = errors.New("dial tcp 10.0.0.5: refused")
	rev, err := (FrigateAdapter{Video: link(f)}).Plan(context.Background(), Camera{}, Patch{Streams: &StreamsPatch{Primary: &VideoPatch{FPS: fp(10)}}})
	if err != nil || len(rev.Unsupported) != 1 || strings.Contains(rev.Unsupported[0], "10.0.0.5") {
		t.Fatalf("%v %+v", err, rev)
	}
	rev, _ = (FrigateAdapter{Video: &VideoLink{Device: newFakeVideo()}}).Plan(context.Background(), Camera{}, Patch{Streams: &StreamsPatch{Primary: &VideoPatch{FPS: fp(10)}}})
	if len(rev.Unsupported) != 1 || !strings.Contains(rev.Unsupported[0], "perfil ONVIF") {
		t.Fatalf("%+v", rev)
	}
}

func TestDetectionWithoutOwnProfileGoesThroughFrigate(t *testing.T) {
	w := &recordingWriter{}
	f := newFakeVideo()
	a := FrigateAdapter{Writer: w, Video: &VideoLink{Device: f, PrimaryToken: "main"}}
	cur := Camera{Streams: Streams{Detection: &Stream{Width: 640, Height: 360, FPS: 5}}}
	patch := Patch{Streams: &StreamsPatch{Detection: &VideoPatch{Resolution: &VideoResolution{Width: 1280, Height: 720}, FPS: fp(8)}}}
	rev, _ := a.Plan(context.Background(), cur, patch)
	if len(rev.Unsupported) != 0 || len(rev.Changes) != 2 || rev.Changes[0].From != "640×360" {
		t.Fatalf("%+v", rev)
	}
	if len(rev.Impact) != 1 || rev.Impact[0].Code != ImpactEngineReload {
		t.Fatalf("impact: %+v", rev.Impact)
	}
	if err := a.Apply(context.Background(), patch); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"detect": map[string]any{"width": 1280, "height": 720, "fps": 8}}
	if !reflect.DeepEqual(w.sections, want) || len(f.sets) != 0 {
		t.Fatalf("sections %#v, device sets %v", w.sections, f.sets)
	}
	// Codec, bitrate and GOP have nowhere to go without a profile of their own.
	rev, _ = a.Plan(context.Background(), cur, Patch{Streams: &StreamsPatch{Detection: &VideoPatch{BitrateKbps: ip(300)}}})
	if len(rev.Unsupported) != 1 {
		t.Fatalf("%+v", rev)
	}
}

func TestApplyVideoWritesTheDeviceThenFrigate(t *testing.T) {
	w := &recordingWriter{}
	f := newFakeVideo()
	a := FrigateAdapter{Writer: w, Video: link(f)}
	err := a.Apply(context.Background(), Patch{
		Recording: &RecordingPatch{Enabled: bp(true)},
		Streams:   &StreamsPatch{Primary: &VideoPatch{FPS: fp(10)}, Detection: &VideoPatch{GOP: ip(8)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.sets, []string{"main", "sub"}) || *f.desired[0].FPS != 10 || *f.desired[1].GOP != 8 {
		t.Fatalf("sets %v %+v", f.sets, f.desired)
	}
	if w.calls != 1 {
		t.Fatalf("frigate calls = %d", w.calls)
	}
	// A video-only patch does not touch Frigate.
	w = &recordingWriter{}
	if err := (FrigateAdapter{Writer: w, Video: link(newFakeVideo())}).Apply(context.Background(), Patch{Streams: &StreamsPatch{Primary: &VideoPatch{FPS: fp(10)}}}); err != nil || w.calls != 0 {
		t.Fatalf("%v calls=%d", err, w.calls)
	}
}

func TestApplyVideoFailureStopsBeforeFrigateAndHidesDeviceText(t *testing.T) {
	w := &recordingWriter{}
	f := newFakeVideo()
	secret := "pass" + "word=hunter2 at http://10.0.0.5/onvif"
	f.setErr = errors.New(secret)
	err := (FrigateAdapter{Writer: w, Video: link(f)}).Apply(context.Background(), Patch{
		Recording: &RecordingPatch{Enabled: bp(true)}, Streams: &StreamsPatch{Primary: &VideoPatch{FPS: fp(10)}}})
	if err == nil || w.calls != 0 || strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "10.0.0.5") {
		t.Fatalf("err=%v calls=%d", err, w.calls)
	}
	f.setErr = &onvif.VerifyError{Fields: []string{"fps"}}
	if err := (FrigateAdapter{Video: link(f)}).Apply(context.Background(), Patch{Streams: &StreamsPatch{Primary: &VideoPatch{FPS: fp(10)}}}); err == nil || !strings.Contains(err.Error(), "fps") {
		t.Fatalf("verify error lost: %v", err)
	}
	// Without access the apply is refused outright.
	if err := (FrigateAdapter{Video: &VideoLink{Reason: VideoNoCredential}}).Apply(context.Background(), Patch{Streams: &StreamsPatch{Primary: &VideoPatch{FPS: fp(10)}}}); err == nil {
		t.Fatal("applied without a device")
	}
}
