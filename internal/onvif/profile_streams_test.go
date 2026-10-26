package onvif

import (
	"context"
	"strings"
	"testing"
)

func TestProfileStreamsReportsVideoAndAudioPerProfile(t *testing.T) {
	d := newFakeMedia2Device(t)
	streams, err := ProfileStreams(context.Background(), d.conn(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(streams) != 2 {
		t.Fatalf("streams = %+v", streams)
	}
	m := streams[0]
	if m.Token != "prof_main" || m.Name != "Main" || m.Codec != CodecH265 || m.Resolution != (VideoResolution{2560, 1440}) ||
		m.FPS != 20 || m.BitrateKbps != 6144 || m.GOP != 50 || m.BitrateMode != BitrateModeVariable || !m.HasAudioEncoder || m.Service != "media2" {
		t.Fatalf("main = %+v", m)
	}
	meta := streams[1]
	if meta.Codec != "" || meta.EncoderToken != "" || meta.HasAudioEncoder || meta.GOP != 0 {
		t.Fatalf("a profile without encoders must stay empty: %+v", meta)
	}
}

func TestProfileStreamsNoAudio(t *testing.T) {
	d := newFakeMedia2Device(t)
	d.audio = false
	streams, err := ProfileStreams(context.Background(), d.conn(t, nil))
	if err != nil || streams[0].HasAudioEncoder {
		t.Fatalf("streams=%+v err=%v", streams, err)
	}
}

func TestProfileStreamsRequestsFullConfigurationsOnMedia2(t *testing.T) {
	d := newFakeMedia2Device(t)
	if _, err := ProfileStreams(context.Background(), d.conn(t, nil)); err != nil {
		t.Fatal(err)
	}
	if got := d.lastBody("GetProfiles"); !strings.Contains(got, "<tr2:Type>All</tr2:Type>") {
		t.Fatalf("Media2 GetProfiles must ask for all configurations: %s", got)
	}
}
