package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/provisionnvr/nvmstest"
)

func TestReplayURLUsesTheRecordersLocalTimeAndKeepsCredentialsOutOfTheError(t *testing.T) {
	srv := nvmstest.New(t, "admin", recorderPass)
	n, sealer := playbackVideo(t, newFakeG2(t, 200))
	cam := recorderAt(t, sealer, srv)
	start := time.Date(2026, 10, 28, 10, 27, 41, 0, time.UTC)
	got, err := n.ReplayURL(context.Background(), cam, start, start.Add(37*time.Second), "main")
	if err != nil {
		t.Fatal(err)
	}
	// 10:27:41 UTC is 07:27:41 in the fixture's zone (ART3); channel 10 is GUID 0000000A.
	for _, want := range []string{"rtsp://admin:" + recorderPass + "@", "/chID=10&date=2026-10-28&time=07:27:41&timelen=37&streamType=main&linkType=tcp"} {
		if !strings.Contains(got, want) {
			t.Fatalf("url = %q, want it to contain %q", got, want)
		}
	}
	sub, err := n.ReplayURL(context.Background(), cam, start, start.Add(time.Minute), "sub")
	if err != nil || !strings.Contains(sub, "streamType=sub") {
		t.Fatalf("sub url = %q, %v", sub, err)
	}
	bad := cam
	bad.RemoteName = "not-a-guid"
	if _, err := n.ReplayURL(context.Background(), bad, start, start.Add(time.Minute), "main"); err == nil {
		t.Fatal("a camera that is not a numbered channel must be refused")
	}
}

func TestHasRecordingTellsWhetherTheRecorderHoldsVideo(t *testing.T) {
	srv := nvmstest.New(t, "admin", recorderPass)
	n, sealer := playbackVideo(t, newFakeG2(t, 200))
	cam := recorderAt(t, sealer, srv)
	from := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	if ok, err := n.HasRecording(context.Background(), cam, from, from.Add(4*time.Hour)); err != nil || !ok {
		t.Fatalf("HasRecording = %v, %v; want true for the fixture's segments", ok, err)
	}
	srv.Override["queryChlRecLog"] = `<response><status>success</status><content><recList timeZone="UTC"></recList></content></response>`
	if ok, err := n.HasRecording(context.Background(), cam, from, from.Add(4*time.Hour)); err != nil || ok {
		t.Fatalf("HasRecording on an empty log = %v, %v; want false", ok, err)
	}
}

func TestFFmpegReplayArgsCopyTheVideoAndCapTheLength(t *testing.T) {
	args := ffmpegReplayArgs("rtsp://u:p@h:554/x", "/tmp/o.mp4", 37*time.Second)
	joined := strings.Join(args, " ")
	for _, want := range []string{"-rtsp_transport tcp", "-timeout 10000000", "-i rtsp://u:p@h:554/x", "-t 39", "-an", "-c:v copy", "-f mp4 /tmp/o.mp4"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q lack %q", joined, want)
		}
	}
	if a := ffmpegReplayArgs("rtsp://x", "o", 0); !strings.Contains(strings.Join(a, " "), "-t 3") {
		t.Fatalf("a zero length must still ask for at least a second: %v", a)
	}
}

func TestRedactRTSPHidesPasswordAndURL(t *testing.T) {
	u := "rtsp://admin:s3cr%40t@192.0.2.9:554/chID=1" // #nosec G101 -- fake credentials in a redaction test
	got := redactRTSP("Connection to "+u+" failed: bad password s3cr@t", u)
	if strings.Contains(got, "s3cr") || strings.Contains(got, "192.0.2.9") {
		t.Fatalf("not redacted: %q", got)
	}
}

// fakeFFmpeg writes a shell script that stands in for ffmpeg: it prints progress, writes the last argument as
// the output file and exits with the given status.
func fakeFFmpeg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil { // #nosec G306 -- test script in a temp dir
		t.Fatal(err)
	}
	return p
}

func TestCaptureReplayReportsProgressAndWritesTheFile(t *testing.T) {
	bin := fakeFFmpeg(t, `for last; do :; done
echo out_time_us=5000000
echo out_time_us=12000000
echo data > "$last"`)
	out := filepath.Join(t.TempDir(), "o.mp4")
	var seen []time.Duration
	err := captureReplay(context.Background(), bin, "rtsp://u:p@h/x", out, 30*time.Second, func(d time.Duration) { seen = append(seen, d) })
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[1] != 12*time.Second {
		t.Fatalf("progress = %v", seen)
	}
	if b, err := os.ReadFile(out); /* #nosec G304 -- temp dir */ err != nil || len(b) == 0 {
		t.Fatalf("output = %q, %v", b, err)
	}
}

func TestCaptureReplayAsksFFmpegToFinishOnceTheWholeClipIsWritten(t *testing.T) {
	// The fake keeps "streaming" until it reads q, like ffmpeg on a recorder that never closes the stream.
	bin := fakeFFmpeg(t, `for last; do :; done
echo out_time_us=39500000
read line
echo data > "$last"`)
	out := filepath.Join(t.TempDir(), "o.mp4")
	start := time.Now()
	if err := captureReplay(context.Background(), bin, "rtsp://u:p@h/x", out, 40*time.Second, nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("capture did not finish when the clip was complete")
	}
	if b, err := os.ReadFile(out); /* #nosec G304 -- temp dir */ err != nil || len(b) == 0 {
		t.Fatalf("output = %q, %v", b, err)
	}
}

func TestCaptureReplayFailureNeverLeaksTheRecordersPassword(t *testing.T) {
	bin := fakeFFmpeg(t, `echo "rtsp://admin:topsecret@192.0.2.9:554/x: Server returned 401 Unauthorized" >&2; exit 1`)
	err := captureReplay(context.Background(), bin, "rtsp://admin:topsecret@192.0.2.9:554/x", filepath.Join(t.TempDir(), "o.mp4"), time.Minute, nil)
	if err == nil {
		t.Fatal("a failing ffmpeg must fail the capture")
	}
	if strings.Contains(err.Error(), "topsecret") || strings.Contains(err.Error(), "192.0.2.9") {
		t.Fatalf("error leaks the credentials: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error should keep the useful part: %v", err)
	}
}

func TestCaptureReplayStopsWhenItsContextEnds(t *testing.T) {
	bin := fakeFFmpeg(t, `sleep 30`)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := captureReplay(ctx, bin, "rtsp://u:p@h/x", filepath.Join(t.TempDir(), "o.mp4"), time.Minute, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the context error", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("the capture did not stop with its context")
	}
}
