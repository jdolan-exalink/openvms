//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/go2rtc"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/provisionnvr"
	"github.com/jdolan-exalink/openvms/internal/provisionnvr/nvmstest"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// fakeGo2RTC answers like the real container: PUT /api/streams registers a source, and the websocket
// /api/ws?src=<name> answers an MSE request with the codec line and a few binary segments.
type fakeGo2RTC struct {
	srv     *httptest.Server
	mu      sync.Mutex
	sources map[string]string
	asked   []string
	deleted []string
}

func newFakeGo2RTC(t *testing.T) *fakeGo2RTC {
	t.Helper()
	f := &fakeGo2RTC{sources: map[string]string{}}
	up := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/streams", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		if r.Method == http.MethodDelete {
			f.deleted = append(f.deleted, r.URL.Query().Get("src"))
		} else {
			f.sources[r.URL.Query().Get("name")] = r.URL.Query().Get("src")
		}
		f.mu.Unlock()
	})
	mux.HandleFunc("/api/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_, msg, err := c.ReadMessage()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.asked = append(f.asked, r.URL.Query().Get("src")+" "+string(msg))
		f.mu.Unlock()
		_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"mse","value":"video/mp4; codecs=\"avc1.640029\""}`))
		for i := 0; i < 3; i++ {
			_ = c.WriteMessage(websocket.BinaryMessage, []byte("segment"))
		}
		time.Sleep(200 * time.Millisecond)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGo2RTC) wasDeleted(name string) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, d := range f.deleted {
			if d == name {
				f.mu.Unlock()
				return true
			}
		}
		f.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func (f *fakeGo2RTC) onlyStreamWithPrefix(prefix string) (name, src string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, v := range f.sources {
		if strings.HasPrefix(k, prefix) {
			name, src, n = k, v, n+1
		}
	}
	return
}

func (f *fakeGo2RTC) source(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sources[name]
}

type liveEnv struct {
	*demofix.Env
	ts     *httptest.Server
	nvr    *nvmstest.Server
	server inventory.ServerView
	camera uuid.UUID
}

// setupLive registers a recorder, builds the router with the video provider that build returns (nil build
// leaves recorder video unconfigured), and returns the camera of channel 10.
func setupLive(t *testing.T, build func(env *demofix.Env) *media.NVRVideo) *liveEnv {
	t.Helper()
	env := demofix.Setup(t)
	var video *media.NVRVideo
	if build != nil {
		video = build(env)
	}
	nvr := nvmstest.New(t, "admin", "Live-Pass-4")
	site := env.Cameras["frigate-h01/plaza"].SiteID
	srv, err := env.Svc.CreateProvisionNVR(context.Background(), env.Admin, inventory.CreateNVRInput{
		SiteID: site, Name: "NVR live", Conn: nvrConn(t, nvr.URL, "Live-Pass-4"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var cam uuid.UUID
	err = env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT id FROM cameras WHERE server_id = $1 AND remote_name LIKE '0000000A%'`, srv.ID).Scan(&cam)
	})
	if err != nil {
		t.Fatal(err)
	}
	adapters := inventory.NewAdapters(env.Svc)
	mediaSvc := &media.Service{Store: env.Store, Adapters: adapters, Log: pgtest.Discard(), NVR: video}
	h := &api.Handlers{Inv: env.Svc, Media: mediaSvc, Log: pgtest.Discard()}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{
		Queries: db.New(env.Pool),
		Media:   (&media.Gateway{Svc: mediaSvc, Actor: api.ActorFrom, NVR: video}).Routes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return &liveEnv{Env: env, ts: ts, nvr: nvr, server: srv, camera: cam}
}

func nvrConn(t *testing.T, nvrURL, password string) inventory.NVRConnInput {
	t.Helper()
	u, err := url.Parse(nvrURL)
	if err != nil {
		t.Fatal(err)
	}
	port := 0
	for _, c := range u.Port() {
		port = port*10 + int(c-'0')
	}
	return inventory.NVRConnInput{Host: u.Hostname(), Port: port, Username: "admin", Password: password}
}

func (l *liveEnv) dial(t *testing.T, token, quality string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(l.ts.URL, "http") + "/media/v1/cameras/" + l.camera.String() + "/live?quality=" + quality
	c, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Authorization": {"Bearer " + token}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func (l *liveEnv) dialPlayback(t *testing.T, token, query string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(l.ts.URL, "http") + "/media/v1/cameras/" + l.camera.String() + "/playback?" + query
	c, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Authorization": {"Bearer " + token}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// read collects what the browser would receive until the socket closes.
func read(t *testing.T, c *websocket.Conn) (text []string, binary int) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		mt, data, err := c.ReadMessage()
		if err != nil {
			return text, binary
		}
		if mt == websocket.TextMessage {
			text = append(text, string(data))
		} else {
			binary++
		}
	}
}

func errorCode(frames []string) string {
	for _, f := range frames {
		var e struct{ Type, Code string }
		if json.Unmarshal([]byte(f), &e) == nil && e.Type == "error" {
			return e.Code
		}
	}
	return ""
}

func probeReturning(codec string, err error) func(context.Context, string, int, string, string, int, provisionnvr.Stream) (*provisionnvr.RTSPFacts, error) {
	return func(_ context.Context, _ string, _ int, _, _ string, ch int, st provisionnvr.Stream) (*provisionnvr.RTSPFacts, error) {
		if err != nil {
			return nil, err
		}
		return &provisionnvr.RTSPFacts{Channel: ch, Stream: st, Codec: codec}, nil
	}
}

func newVideo(t *testing.T, env *demofix.Env, g2 *fakeGo2RTC, probe func(context.Context, string, int, string, string, int, provisionnvr.Stream) (*provisionnvr.RTSPFacts, error)) *media.NVRVideo {
	t.Helper()
	client, err := go2rtc.New(g2.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &media.NVRVideo{Sealer: env.Sealer, G2: client, Probe: probe}
}

func TestRecorderCameraPlaysThroughGo2RTC(t *testing.T) {
	g2 := newFakeGo2RTC(t)
	l := setupLive(t, func(env *demofix.Env) *media.NVRVideo { return newVideo(t, env, g2, probeReturning("H265", nil)) })

	c := l.dial(t, l.AdminToken, "sub")
	if err := c.WriteMessage(websocket.TextMessage, []byte(`{"type":"mse","value":"avc1.640029,avc1.64002A"}`)); err != nil {
		t.Fatal(err)
	}
	text, binary := read(t, c)
	// The codec line first, then the segments; when go2rtc ends the stream the gateway adds its own end frame.
	if binary != 3 || len(text) == 0 || !strings.Contains(text[0], `"type":"mse"`) || (len(text) > 1 && errorCode(text[1:]) != "camera_offline") {
		t.Fatalf("the browser got text=%v binary=%d, want the codec line and 3 segments", text, binary)
	}

	name := "nvr-" + l.camera.String() + "-sub"
	src := g2.source(name)
	if !strings.HasPrefix(src, "ffmpeg:rtsp://admin:") || !strings.HasSuffix(src, "#video=h264") || !strings.Contains(src, "/chID=10&streamType=sub&linkType=tcp") {
		t.Fatalf("go2rtc source = %q: H.265 must be converted to H.264, channel 10 sub", src)
	}
	g2.mu.Lock()
	asked := append([]string(nil), g2.asked...)
	g2.mu.Unlock()
	if len(asked) != 1 || !strings.HasPrefix(asked[0], name+" {") {
		t.Fatalf("the browser's request did not reach go2rtc for the right stream: %v", asked)
	}
	// Nothing the browser saw carries the recorder's address or password.
	all := strings.Join(text, " ")
	for _, secret := range []string{"Live-Pass-4", "admin", "127.0.0.1", "rtsp://"} {
		if strings.Contains(all, secret) {
			t.Fatalf("the browser received %q", secret)
		}
	}
}

func TestRecorderCameraWithoutAVideoGatewayStaysUnavailable(t *testing.T) {
	l := setupLive(t, nil)
	c := l.dial(t, l.AdminToken, "sub")
	text, binary := read(t, c)
	if errorCode(text) != "video_unavailable" || binary != 0 {
		t.Fatalf("text=%v binary=%d, want a video_unavailable frame", text, binary)
	}
}

func TestRecorderCameraFailuresAreReportedWithTheRightCode(t *testing.T) {
	t.Run("the recorder rejects the stored password", func(t *testing.T) {
		g2 := newFakeGo2RTC(t)
		l := setupLive(t, func(env *demofix.Env) *media.NVRVideo {
			return newVideo(t, env, g2, probeReturning("", &provisionnvr.Error{Class: provisionnvr.ClassAuthFailed, Err: provisionnvr.ErrRTSPAuth}))
		})
		text, _ := read(t, l.dial(t, l.AdminToken, "main"))
		if errorCode(text) != "upstream_unreachable" {
			t.Fatalf("frames = %v", text)
		}
		if g2.source("nvr-"+l.camera.String()+"-main") != "" {
			t.Fatal("nothing may be registered in go2rtc when the recorder refused the credentials")
		}
	})
	t.Run("go2rtc is down", func(t *testing.T) {
		g2 := newFakeGo2RTC(t)
		g2.srv.Close()
		l := setupLive(t, func(env *demofix.Env) *media.NVRVideo { return newVideo(t, env, g2, probeReturning("H264", nil)) })
		text, _ := read(t, l.dial(t, l.AdminToken, "main"))
		if errorCode(text) != "upstream_unreachable" {
			t.Fatalf("frames = %v", text)
		}
		for _, f := range text {
			if strings.Contains(f, "Live-Pass-4") || strings.Contains(f, "rtsp://") {
				t.Fatalf("the error frame carries a secret: %s", f)
			}
		}
	})
	t.Run("a bad credential is refused at the handshake", func(t *testing.T) {
		g2 := newFakeGo2RTC(t)
		l := setupLive(t, func(env *demofix.Env) *media.NVRVideo { return newVideo(t, env, g2, probeReturning("H264", nil)) })
		wsURL := "ws" + strings.TrimPrefix(l.ts.URL, "http") + "/media/v1/cameras/" + l.camera.String() + "/live?quality=sub"
		_, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Authorization": {"Bearer not-a-token"}})
		if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("a bad token must be refused at the handshake with 401, got resp=%v err=%v", resp, err)
		}
		if g2.source("nvr-"+l.camera.String()+"-sub") != "" {
			t.Fatal("a refused viewer must not make go2rtc register a stream")
		}
	})
}

func TestRecorderCameraPlaysItsRecordingThroughGo2RTC(t *testing.T) {
	g2 := newFakeGo2RTC(t)
	l := setupLive(t, func(env *demofix.Env) *media.NVRVideo { return newVideo(t, env, g2, probeReturning("H264", nil)) })
	// The segment proved on the real unit: 10:27:41 UTC, 37 s. The recorder's clock (fixture) is three hours behind UTC.
	start := time.Date(2026, 10, 28, 10, 27, 41, 0, time.UTC)
	query := "quality=main&start=" + itoa(start.Unix()) + "&end=" + itoa(start.Add(37*time.Second).Unix())

	c := l.dialPlayback(t, l.AdminToken, query)
	if err := c.WriteMessage(websocket.TextMessage, []byte(`{"type":"mse","value":"avc1.640029,avc1.64002A"}`)); err != nil {
		t.Fatal(err)
	}
	text, binary := read(t, c)
	if binary != 3 || len(text) == 0 || !strings.Contains(text[0], `"type":"mse"`) {
		t.Fatalf("the browser got text=%v binary=%d, want the codec line and 3 segments", text, binary)
	}

	name, src, n := g2.onlyStreamWithPrefix("nvr-" + l.camera.String() + "-rec-")
	if n != 1 {
		t.Fatalf("go2rtc has %d replay streams for the camera, want 1", n)
	}
	for _, want := range []string{"/chID=10&date=2026-10-28&time=07:27:41&timelen=37&streamType=main&linkType=tcp", "#video=copy"} {
		if !strings.Contains(src, want) {
			t.Fatalf("source %q lacks %q: the replay must use the recorder's local time", src, want)
		}
	}
	if !g2.wasDeleted(name) {
		t.Fatalf("the replay stream %q must be removed when the session ends", name)
	}
	for _, secret := range []string{"Live-Pass-4", "127.0.0.1", "rtsp://"} {
		if strings.Contains(strings.Join(text, " "), secret) {
			t.Fatalf("the browser received %q", secret)
		}
	}
}

func TestRecorderPlaybackRefusals(t *testing.T) {
	g2 := newFakeGo2RTC(t)
	l := setupLive(t, func(env *demofix.Env) *media.NVRVideo { return newVideo(t, env, g2, probeReturning("H264", nil)) })
	now := time.Now()
	for name, query := range map[string]string{
		"no range":  "quality=main",
		"backwards": "start=" + itoa(now.Unix()) + "&end=" + itoa(now.Add(-time.Hour).Unix()),
		"too long":  "start=" + itoa(now.Add(-48*time.Hour).Unix()) + "&end=" + itoa(now.Unix()),
		"nonsense":  "start=a&end=b",
	} {
		text, _ := read(t, l.dialPlayback(t, l.AdminToken, query))
		if errorCode(text) != "camera_offline" {
			t.Fatalf("%s: frames = %v, want camera_offline", name, text)
		}
	}
	if _, _, n := g2.onlyStreamWithPrefix("nvr-"); n != 0 {
		t.Fatal("a refused range must not register a stream")
	}

	// A Frigate camera has no replay through this socket: it plays through HLS.
	frigateCam := l.Cameras["frigate-h01/plaza"].ID
	wsURL := "ws" + strings.TrimPrefix(l.ts.URL, "http") + "/media/v1/cameras/" + frigateCam.String() + "/playback?start=" + itoa(now.Add(-time.Hour).Unix()) + "&end=" + itoa(now.Unix())
	c, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Authorization": {"Bearer " + l.AdminToken}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if text, _ := read(t, c); errorCode(text) != "video_unavailable" {
		t.Fatalf("a Frigate camera on the playback socket: frames = %v, want video_unavailable", text)
	}

	// Without a video gateway the recorder camera says so, and a bad credential never reaches the upgrade.
	bare := setupLive(t, nil)
	text, _ := read(t, bare.dialPlayback(t, bare.AdminToken, "start="+itoa(now.Add(-time.Hour).Unix())+"&end="+itoa(now.Unix())))
	if errorCode(text) != "video_unavailable" {
		t.Fatalf("without go2rtc: frames = %v, want video_unavailable", text)
	}
	wsURL = "ws" + strings.TrimPrefix(l.ts.URL, "http") + "/media/v1/cameras/" + l.camera.String() + "/playback?start=" + itoa(now.Add(-time.Hour).Unix()) + "&end=" + itoa(now.Unix())
	if _, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Authorization": {"Bearer nope"}}); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a bad token must be refused at the handshake with 401, got resp=%v err=%v", resp, err)
	}
}

func TestRecorderCameraTimelineComesFromTheRecordersLog(t *testing.T) {
	g2 := newFakeGo2RTC(t)
	l := setupLive(t, func(env *demofix.Env) *media.NVRVideo { return newVideo(t, env, g2, probeReturning("H264", nil)) })
	from := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC).Format(time.RFC3339)
	to := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	req, _ := http.NewRequest(http.MethodGet, l.ts.URL+"/api/v1/cameras/"+l.camera.String()+"/recordings?from="+from+"&to="+to, nil)
	req.Header.Set("Authorization", "Bearer "+l.AdminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Items []struct {
			StartTime time.Time `json:"start_time"`
			EndTime   time.Time `json:"end_time"`
			Motion    int       `json:"motion"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, %v", resp.StatusCode, err)
	}
	if len(body.Items) != 3 || body.Items[1].EndTime.Sub(body.Items[1].StartTime) != 37*time.Second || body.Items[1].Motion != 1 {
		t.Fatalf("timeline = %+v", body.Items)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
