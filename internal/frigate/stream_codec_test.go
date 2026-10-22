package frigate

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/frigatemock"
)

func TestCodecFromMedias(t *testing.T) {
	for _, tc := range []struct {
		name   string
		medias []string
		want   string
	}{
		{"h265 with audio and onvif metadata", []string{"video, recvonly, H265", "audio, recvonly, PCMA/8000", "application, recvonly, VND.ONVIF.METADATA"}, "h265"},
		{"h264", []string{"video, recvonly, H264", "application, recvonly, VND.ONVIF.METADATA"}, "h264"},
		{"hevc spelled out", []string{"video, recvonly, HEVC"}, "h265"},
		{"lowercase", []string{"video, recvonly, h265"}, "h265"},
		{"mjpeg is kept as is", []string{"video, recvonly, JPEG"}, "jpeg"},
		{"audio only", []string{"audio, recvonly, PCMA/8000"}, ""},
		{"no medias", nil, ""},
		{"video is the first media that counts", []string{"audio, recvonly, AAC", "video, recvonly, H264", "video, recvonly, H265"}, "h264"},
	} {
		if got := codecFromMedias(tc.medias); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func startMockWithCodecs(t *testing.T, spec string, streams []string, codecs map[string]string) *httptest.Server {
	t.Helper()
	cams, err := frigatemock.ParseCameras(spec)
	if err != nil {
		t.Fatal(err)
	}
	srv := &frigatemock.Server{Version: "0.18.0-mock", Cameras: cams, Store: frigatemock.NewStore(10), StartedAt: time.Now(), Go2RTCStreams: streams, Go2RTCCodecs: codecs}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestListCamerasReportsTheCodecOfEachStream(t *testing.T) {
	ts := startMockWithCodecs(t, "Cementerio,Banco",
		[]string{"cementerio_sub", "cementerio_main", "banco_sub", "banco_main"},
		map[string]string{"cementerio_sub": "H265", "cementerio_main": "H265", "banco_sub": "H264", "banco_main": "H264"})
	a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	cams, err := a.ListCameras(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][2]string{}
	for _, c := range cams {
		got[c.Name] = [2]string{c.LiveCodec, c.HQCodec}
	}
	if got["Cementerio"] != [2]string{"h265", "h265"} || got["Banco"] != [2]string{"h264", "h264"} {
		t.Fatalf("codecs: %+v", got)
	}
}

func TestListCamerasCodecIsUnknownWhenGo2RTCHasNoProducer(t *testing.T) {
	ts := startMockWithCodecs(t, "Banco", []string{"banco_sub", "banco_main"}, nil)
	a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	cams, err := a.ListCameras(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 1 || cams[0].LiveStream != "banco_sub" || cams[0].LiveCodec != "" || cams[0].HQCodec != "" {
		t.Fatalf("unexpected cameras: %+v", cams)
	}
}

func TestStreamCodecsListsEveryProducingStream(t *testing.T) {
	ts := startMockWithCodecs(t, "Banco", []string{"banco_sub", "banco_main", "idle"}, map[string]string{"banco_sub": "H265", "banco_main": "H264"})
	a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.StreamCodecs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["banco_sub"] != "h265" || got["banco_main"] != "h264" {
		t.Fatalf("StreamCodecs = %v", got)
	}
}

func TestStreamCodecsFailsWhenGo2RTCIsUnavailable(t *testing.T) {
	ts := startMockWithCodecs(t, "Banco", nil, nil)
	a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := a.StreamCodecs(context.Background()); err == nil {
		t.Fatalf("want an error so callers keep the stored codecs, got %v", got)
	}
}
