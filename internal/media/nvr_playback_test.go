package media

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/provisionnvr"
	"github.com/jdolan-exalink/openvms/internal/provisionnvr/nvmstest"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const recorderPass = "pw-ok"

// recorderAt is a camera of the fake recorder srv: its stored password is sealed the way the server stores it.
func recorderAt(t *testing.T, sealer *secrets.Sealer, srv *nvmstest.Server) Camera {
	t.Helper()
	serverID := uuid.New()
	sealed, err := sealer.Seal([]byte(recorderPass), serverID[:])
	if err != nil {
		t.Fatal(err)
	}
	return Camera{
		ID: uuid.New(), RemoteName: "0000000A-0000-0000-0000-000000000000",
		Server: db.FrigateServer{ID: serverID, Kind: "provision_nvr", BaseUrl: srv.URL, Username: "admin", PasswordSealed: sealed},
	}
}

func playbackVideo(t *testing.T, g2 *fakeG2) (*NVRVideo, *secrets.Sealer) {
	t.Helper()
	v := newNVRVideo(t, g2, "H264", nil, nil)
	return v, v.Sealer
}

func TestRecordingsComeFromTheRecordersLogAndShareOneSession(t *testing.T) {
	srv := nvmstest.New(t, "admin", recorderPass)
	n, sealer := playbackVideo(t, newFakeG2(t, 200))
	cam := recorderAt(t, sealer, srv)
	from := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	to := from.Add(4 * time.Hour)

	spans, err := n.Recordings(context.Background(), cam, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 3 || spans[0].Motion != 1 || spans[2].Motion != 0 {
		t.Fatalf("spans = %+v, want 3 with motion on the MOTION ones only", spans)
	}
	if want := time.Date(2026, 10, 28, 10, 27, 41, 0, time.UTC); !spans[1].Start.Equal(want) {
		t.Fatalf("second span starts %v, want %v (UTC)", spans[1].Start, want)
	}
	if _, err := n.Recordings(context.Background(), cam, from, to); err != nil {
		t.Fatal(err)
	}
	if srv.Logins() != 1 {
		t.Fatalf("logins = %d, want 1: the session must be reused", srv.Logins())
	}
}

func TestRecordingsReportRecorderFailuresWithTheirClass(t *testing.T) {
	srv := nvmstest.New(t, "admin", "another-password")
	n, sealer := playbackVideo(t, newFakeG2(t, 200))
	cam := recorderAt(t, sealer, srv) // the stored password is not the recorder's
	from := time.Now().Add(-time.Hour)
	_, err := n.Recordings(context.Background(), cam, from, from.Add(time.Hour))
	var ne *inventory.NVRError
	if !errors.As(err, &ne) || ne.Class != provisionnvr.ClassAuthFailed {
		t.Fatalf("a rejected password = %v, want an NVRError of class auth_failed", err)
	}
	if strings.Contains(err.Error(), recorderPass) {
		t.Fatalf("the error carries the password: %v", err)
	}

	dead := nvmstest.New(t, "admin", recorderPass)
	cam2 := recorderAt(t, sealer, dead)
	dead.Close()
	if _, err := n.Recordings(context.Background(), cam2, from, from.Add(time.Hour)); !errors.As(err, &ne) || ne.Class != provisionnvr.ClassOffline {
		t.Fatalf("an unreachable recorder = %v, want class offline", err)
	}
}

func TestReplayUsesTheRecordersLocalTimeAndItsOwnStream(t *testing.T) {
	srv := nvmstest.New(t, "admin", recorderPass)
	g2 := newFakeG2(t, 200)
	n, sealer := playbackVideo(t, g2)
	cam := recorderAt(t, sealer, srv)
	// The segment proved on the real unit: 10:27:41 UTC is 07:27:41 for a recorder three hours behind (the fixture's clock).
	start := time.Date(2026, 10, 28, 10, 27, 41, 0, time.UTC)

	a, err := n.OpenReplay(context.Background(), cam, start, start.Add(37*time.Second), "main")
	if err != nil {
		t.Fatal(err)
	}
	b, err := n.OpenReplay(context.Background(), cam, start, start.Add(37*time.Second), "main")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name == b.Name || !strings.HasPrefix(a.Name, "nvr-"+cam.ID.String()+"-rec-") {
		t.Fatalf("each session needs its own stream: %q and %q", a.Name, b.Name)
	}
	src := g2.sourceOf(t, a.Name)
	for _, want := range []string{"ffmpeg:rtsp://", "/chID=10&date=2026-10-28&time=07:27:41&timelen=37&streamType=main&linkType=tcp", "#video=copy"} {
		if !strings.Contains(src, want) {
			t.Fatalf("source %q lacks %q", src, want)
		}
	}
	if !strings.HasSuffix(a.Target, "/api/ws?src="+a.Name) {
		t.Fatalf("target = %q", a.Target)
	}
	for _, secret := range []string{recorderPass, "admin", "rtsp://"} {
		if strings.Contains(a.Name, secret) || strings.Contains(a.Target, secret) {
			t.Fatalf("the live carries %q", secret)
		}
	}
	// The clock offset is asked once for the two sessions.
	timeCfg := 0
	for _, c := range srv.Calls() {
		if c == "queryTimeCfg" {
			timeCfg++
		}
	}
	if timeCfg != 1 {
		t.Fatalf("queryTimeCfg was asked %d times, want 1 (cached)", timeCfg)
	}
}

func TestReplayConvertsWhatABrowserMayNotPlayAndCleansUp(t *testing.T) {
	srv := nvmstest.New(t, "admin", recorderPass)
	g2 := newFakeG2(t, 200)
	n, sealer := playbackVideo(t, g2)
	n.Probe = func(_ context.Context, _ string, _ int, _, _ string, ch int, st provisionnvr.Stream) (*provisionnvr.RTSPFacts, error) {
		return &provisionnvr.RTSPFacts{Channel: ch, Stream: st, Codec: "H265"}, nil
	}
	cam := recorderAt(t, sealer, srv)
	start := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	live, err := n.OpenReplay(context.Background(), cam, start, start.Add(time.Hour), "sub")
	if err != nil {
		t.Fatal(err)
	}
	if !live.Transcoded || !strings.HasSuffix(g2.sourceOf(t, live.Name), "#video=h264") || !strings.Contains(g2.sourceOf(t, live.Name), "streamType=sub") {
		t.Fatalf("an H.265 sub replay must be converted: %q", g2.sourceOf(t, live.Name))
	}
	if err := n.Close(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	var deleted bool
	for _, p := range g2.puts() {
		if p.Get("_method") == "DELETE" && p.Get("src") == live.Name {
			deleted = true
		}
	}
	if !deleted {
		t.Fatalf("Close must delete the stream, go2rtc saw %+v", g2.puts())
	}
	if err := n.Close(context.Background(), nil); err != nil {
		t.Fatalf("closing nothing is not an error: %v", err)
	}
}

func TestReplayRefusesBadRangesAndFailuresRegisterNothing(t *testing.T) {
	srv := nvmstest.New(t, "admin", recorderPass)
	g2 := newFakeG2(t, 200)
	n, sealer := playbackVideo(t, g2)
	cam := recorderAt(t, sealer, srv)
	now := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	var ve *NVRVideoError
	for name, r := range map[string][2]time.Time{
		"empty":     {now, now},
		"backwards": {now, now.Add(-time.Hour)},
		"too long":  {now, now.Add(25 * time.Hour)},
	} {
		if _, err := n.OpenReplay(context.Background(), cam, r[0], r[1], "main"); !errors.As(err, &ve) {
			t.Fatalf("%s range must be refused, got %v", name, err)
		}
	}
	if len(g2.puts()) != 0 {
		t.Fatal("a refused range must not register anything in go2rtc")
	}

	bad := newNVRVideo(t, g2, "", &provisionnvr.Error{Class: provisionnvr.ClassDegraded, Err: errors.New("DESCRIBE answered 404")}, nil)
	camBad := recorderAt(t, bad.Sealer, srv)
	if _, err := bad.OpenReplay(context.Background(), camBad, now, now.Add(time.Minute), "main"); !errors.As(err, &ve) || ve.Code != CodeCameraOffline {
		t.Fatalf("a recorder without the stream = %v, want camera_offline", err)
	}
	if len(g2.puts()) != 0 {
		t.Fatal("nothing may be registered when the recorder has no stream")
	}
}
