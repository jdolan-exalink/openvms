package media

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/go2rtc"
	"github.com/jdolan-exalink/openvms/internal/provisionnvr"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const nvrTestPassword = "Rec0rder&Pass#1"

type fakeG2 struct {
	srv *httptest.Server
	mu  sync.Mutex
	put []url.Values
}

func newFakeG2(t *testing.T, status int) *fakeG2 {
	t.Helper()
	f := &fakeG2{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		q.Set("_method", r.Method)
		f.mu.Lock()
		f.put = append(f.put, q)
		f.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// sourceOf returns the source registered under name.
func (f *fakeG2) sourceOf(t *testing.T, name string) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, q := range f.put {
		if q.Get("_method") == "PUT" && q.Get("name") == name {
			return q.Get("src")
		}
	}
	t.Fatalf("go2rtc has no stream named %q", name)
	return ""
}

func (f *fakeG2) puts() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.put...)
}

type probeCall struct {
	host     string
	port     int
	user     string
	password string
	channel  int
	stream   provisionnvr.Stream
}

func recorderCamera(t *testing.T, sealer *secrets.Sealer, capabilities string) Camera {
	t.Helper()
	serverID := uuid.New()
	sealed, err := sealer.Seal([]byte(nvrTestPassword), serverID[:])
	if err != nil {
		t.Fatal(err)
	}
	return Camera{
		ID: uuid.New(), RemoteName: "0000000A-0000-0000-0000-000000000000",
		Server: db.FrigateServer{ID: serverID, Kind: "provision_nvr", BaseUrl: "http://192.0.2.50:80", Username: "admin", PasswordSealed: sealed, Capabilities: []byte(capabilities)},
	}
}

func newNVRVideo(t *testing.T, g2 *fakeG2, codec string, probeErr error, calls *[]probeCall) *NVRVideo {
	t.Helper()
	sealer, err := secrets.NewSealer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	client, err := go2rtc.New(g2.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &NVRVideo{Sealer: sealer, G2: client,
		Probe: func(_ context.Context, host string, port int, user, pw string, ch int, st provisionnvr.Stream) (*provisionnvr.RTSPFacts, error) {
			if calls != nil {
				*calls = append(*calls, probeCall{host, port, user, pw, ch, st})
			}
			if probeErr != nil {
				return nil, probeErr
			}
			return &provisionnvr.RTSPFacts{Channel: ch, Stream: st, Codec: codec}, nil
		}}
}

func sealerCamera(t *testing.T, n *NVRVideo, capabilities string) Camera {
	return recorderCamera(t, n.Sealer, capabilities)
}

func TestOpenCopiesAnH264StreamWithoutTranscoding(t *testing.T) {
	g2 := newFakeG2(t, 200)
	var calls []probeCall
	n := newNVRVideo(t, g2, "H264", nil, &calls)
	cam := sealerCamera(t, n, `{"provision_nvr":{"rtsp_port":8554}}`)
	live, err := n.Open(context.Background(), cam, "main")
	if err != nil {
		t.Fatal(err)
	}
	if live.Transcoded {
		t.Fatal("an H.264 stream must be copied, not transcoded")
	}
	if len(calls) != 1 || calls[0].host != "192.0.2.50" || calls[0].port != 8554 || calls[0].channel != 10 || calls[0].stream != provisionnvr.StreamMain || calls[0].password != nvrTestPassword {
		t.Fatalf("probe = %+v", calls)
	}
	puts := g2.puts()
	if len(puts) != 1 || puts[0].Get("name") != live.Name || live.Name != "nvr-"+cam.ID.String()+"-main" {
		t.Fatalf("go2rtc got %+v, stream %q", puts, live.Name)
	}
	src := puts[0].Get("src")
	if !strings.HasPrefix(src, "ffmpeg:rtsp://") || !strings.HasSuffix(src, "#video=copy") || !strings.Contains(src, "@192.0.2.50:8554/chID=10&streamType=main&linkType=tcp") {
		t.Fatalf("source = %q", src)
	}
	if !strings.HasSuffix(live.Target, "/api/ws?src="+live.Name) {
		t.Fatalf("target = %q", live.Target)
	}
}

func TestOpenTranscodesWhatABrowserMayNotPlay(t *testing.T) {
	for _, codec := range []string{"H265", "HEVC", ""} {
		g2 := newFakeG2(t, 200)
		n := newNVRVideo(t, g2, codec, nil, nil)
		live, err := n.Open(context.Background(), sealerCamera(t, n, `{}`), "sub")
		if err != nil {
			t.Fatal(err)
		}
		if !live.Transcoded || !strings.HasSuffix(g2.puts()[0].Get("src"), "#video=h264") {
			t.Fatalf("codec %q must be converted to H.264, source %q", codec, g2.puts()[0].Get("src"))
		}
	}
}

func TestOpenDefaultsTheRTSPPortAndPicksSubUnlessMainIsAsked(t *testing.T) {
	g2 := newFakeG2(t, 200)
	var calls []probeCall
	n := newNVRVideo(t, g2, "H264", nil, &calls)
	for _, q := range []string{"", "sub", "anything"} {
		if _, err := n.Open(context.Background(), sealerCamera(t, n, `{}`), q); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range calls {
		if c.port != 554 || c.stream != provisionnvr.StreamSub {
			t.Fatalf("probe %+v, want port 554 and the sub stream", c)
		}
	}
}

func TestOpenNeverLeaksThePasswordToTheCallerOrTheLive(t *testing.T) {
	g2 := newFakeG2(t, 200)
	n := newNVRVideo(t, g2, "H264", nil, nil)
	live, err := n.Open(context.Background(), sealerCamera(t, n, `{}`), "main")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(live.Name, nvrTestPassword) || strings.Contains(live.Target, nvrTestPassword) || strings.Contains(live.Target, "admin") {
		t.Fatalf("the returned live carries a secret: %+v", live)
	}
	// The password is percent-encoded in the source, because it has characters that end a URL's user info.
	src := g2.puts()[0].Get("src")
	if strings.Contains(src, nvrTestPassword) || !strings.Contains(src, "Rec0rder&Pass%231") && !strings.Contains(src, "Rec0rder%26Pass%231") {
		t.Fatalf("source = %q", src)
	}

	failing := newFakeG2(t, 500)
	n2 := newNVRVideo(t, failing, "H264", nil, nil)
	_, err = n2.Open(context.Background(), sealerCamera(t, n2, `{}`), "main")
	if err == nil || strings.Contains(err.Error(), nvrTestPassword) || strings.Contains(err.Error(), "admin") {
		t.Fatalf("a go2rtc failure must not carry the credentials: %v", err)
	}
}

func TestOpenMapsRecorderFailuresToLiveCodes(t *testing.T) {
	cases := map[string]struct {
		err  error
		code string
	}{
		"rejected credentials": {&provisionnvr.Error{Class: provisionnvr.ClassAuthFailed, Err: provisionnvr.ErrRTSPAuth}, CodeUpstreamUnreachable},
		"recorder unreachable": {&provisionnvr.Error{Class: provisionnvr.ClassOffline, Err: errors.New("down")}, CodeUpstreamUnreachable},
		"not an RTSP server":   {&provisionnvr.Error{Class: provisionnvr.ClassUnsupported, Err: errors.New("x")}, CodeUpstreamUnreachable},
		"no such stream (404)": {&provisionnvr.Error{Class: provisionnvr.ClassDegraded, Err: errors.New("DESCRIBE answered 404")}, CodeCameraOffline},
	}
	for name, tc := range cases {
		g2 := newFakeG2(t, 200)
		n := newNVRVideo(t, g2, "", tc.err, nil)
		_, err := n.Open(context.Background(), sealerCamera(t, n, `{}`), "main")
		var ve *NVRVideoError
		if !errors.As(err, &ve) || ve.Code != tc.code {
			t.Fatalf("%s: error = %v, want code %s", name, err, tc.code)
		}
		if len(g2.puts()) != 0 {
			t.Fatalf("%s: nothing may be registered when the recorder refused", name)
		}
	}
}

func TestOpenRefusesWhatIsNotARecorderChannel(t *testing.T) {
	g2 := newFakeG2(t, 200)
	n := newNVRVideo(t, g2, "H264", nil, nil)
	cam := sealerCamera(t, n, `{}`)
	cam.RemoteName = "birdseye"
	var ve *NVRVideoError
	if _, err := n.Open(context.Background(), cam, "main"); !errors.As(err, &ve) || ve.Code != CodeCameraOffline {
		t.Fatalf("a camera with no channel number = %v", err)
	}
	cam = sealerCamera(t, n, `{}`)
	cam.Server.PasswordSealed = []byte("garbage")
	if _, err := n.Open(context.Background(), cam, "main"); !errors.As(err, &ve) || ve.Code != CodeServerError {
		t.Fatalf("an unreadable password = %v", err)
	}
}

func TestTheCodecIsRememberedUntilItExpires(t *testing.T) {
	g2 := newFakeG2(t, 200)
	var calls []probeCall
	n := newNVRVideo(t, g2, "H265", nil, &calls)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	n.Now = func() time.Time { return now }
	cam := sealerCamera(t, n, `{}`)
	for range 3 {
		if _, err := n.Open(context.Background(), cam, "sub"); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("the recorder was asked %d times, want 1 while the answer is fresh", len(calls))
	}
	if _, err := n.Open(context.Background(), cam, "main"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("main and sub have their own codec: asked %d times, want 2", len(calls))
	}
	now = now.Add(codecCacheTTL + time.Second)
	if _, err := n.Open(context.Background(), cam, "sub"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatalf("after the TTL the recorder must be asked again, asked %d times", len(calls))
	}
	if got := len(g2.puts()); got != 5 {
		t.Fatalf("go2rtc was registered %d times, want one per session (5)", got)
	}
}
