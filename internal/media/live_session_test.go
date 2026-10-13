package media

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestViewTrackerCollapsesReconnects(t *testing.T) {
	var vt viewTracker
	k := viewKey{uuid.New(), uuid.New()}
	now := time.Now()
	w := 5 * time.Minute
	if !vt.begin(k, now, w) {
		t.Fatal("first view must be new")
	}
	if vt.begin(k, now.Add(4*time.Minute), w) {
		t.Fatal("reconnect inside the window must not be new")
	}
	// the window slides: 4m after the previous connection is still the same session
	if vt.begin(k, now.Add(8*time.Minute), w) {
		t.Fatal("sliding window must keep the session")
	}
	if !vt.begin(k, now.Add(14*time.Minute), w) {
		t.Fatal("after an idle gap longer than the window the view is new")
	}
	if !vt.begin(viewKey{k.user, uuid.New()}, now, w) {
		t.Fatal("another camera is a new view")
	}
	vt.forget(k)
	if !vt.begin(k, now.Add(15*time.Minute), w) {
		t.Fatal("forget must let the next connect audit again")
	}
}

func TestViewTrackerConcurrentConnectsAuditOnce(t *testing.T) {
	var vt viewTracker
	k := viewKey{uuid.New(), uuid.New()}
	var news atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if vt.begin(k, time.Now(), time.Minute) {
				news.Add(1)
			}
		}()
	}
	wg.Wait()
	if news.Load() != 1 {
		t.Fatalf("new views = %d, want 1", news.Load())
	}
}

func TestUpstreamError(t *testing.T) {
	if e, ok := upstreamError([]byte(`{"type":"error","value":"streams: codec not supported"}`)); !ok || e.code != CodeCodecUnsupported {
		t.Fatalf("got %+v %v", e, ok)
	}
	if e, ok := upstreamError([]byte(`{"type":"error","value":"x"}`)); !ok || e.code != CodeCameraOffline {
		t.Fatalf("got %+v %v", e, ok)
	}
	if _, ok := upstreamError([]byte(`{"type":"mse","value":"video/mp4"}`)); ok {
		t.Fatal("mse frame is not an error")
	}
}

func TestUpstreamErrorKeepsRedactedDetailForLogs(t *testing.T) {
	e, ok := upstreamError([]byte(`{"type":"error","value":"streams: rtsp://admin:s3cret@10.0.0.5:554/profile2 401 Unauthorized"}`))
	if !ok || e.code != CodeCameraOffline {
		t.Fatalf("got %+v %v", e, ok)
	}
	if strings.Contains(e.detail, "s3cret") || strings.Contains(e.detail, "admin:") {
		t.Fatalf("detail leaks credentials: %q", e.detail)
	}
	if !strings.Contains(e.detail, "rtsp://***@10.0.0.5:554/profile2 401 Unauthorized") {
		t.Fatalf("detail = %q, want the redacted go2rtc message", e.detail)
	}
	if strings.Contains(e.msg, "rtsp") {
		t.Fatalf("browser message must stay generic, got %q", e.msg)
	}
}

func TestRedactURLCredentials(t *testing.T) {
	cases := map[string]string{
		"rtsp://u:p@host/a":                  "rtsp://***@host/a",
		"dial rtsps://user@cam:322/x failed": "dial rtsps://***@cam:322/x failed",
		"no credentials http://host/x":       "no credentials http://host/x",
		"two rtsp://a:b@h1/x rtsp://c:d@h2":  "two rtsp://***@h1/x rtsp://***@h2",
	}
	for in, want := range cases {
		if got := redactURLCredentials(in); got != want {
			t.Errorf("redactURLCredentials(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("x", 2000)
	if got := redactURLCredentials(long); len(got) > maxUpstreamDetail {
		t.Errorf("detail not truncated: %d bytes", len(got))
	}
}
