package frigate

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/frigatemock"
)

func startMock(t *testing.T, version string) *httptest.Server {
	t.Helper()
	cams, _ := frigatemock.ParseCameras("acceso_norte+lpr:entrada|salida,plaza:centro")
	store := frigatemock.NewStore(100)
	g := &frigatemock.Generator{Cameras: cams, Store: store}
	g.Seed(10, time.Hour, time.Now())
	srv := &frigatemock.Server{Version: version, Cameras: cams, Store: store, User: "vms", Password: "pw", RequireAuth: true, StartedAt: time.Now()}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestConnectDiscoversCamerasAndCapabilities(t *testing.T) {
	ts := startMock(t, "0.17.2-mock")
	ctx := context.Background()
	a, err := Connect(ctx, ConnInfo{BaseURL: ts.URL, Username: "vms", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name() != "v017" || a.Version() != "0.17.2-mock" {
		t.Fatalf("adapter %s version %s", a.Name(), a.Version())
	}
	cams, err := a.ListCameras(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 2 || cams[0].Name != "acceso_norte" || !cams[0].LPR || len(cams[0].Zones) != 2 {
		t.Fatalf("unexpected cameras: %+v", cams)
	}
	if cams[0].LiveStream != "acceso_norte_sub" || cams[0].HQStream != "acceso_norte" {
		t.Errorf("stream selection: live=%s hq=%s", cams[0].LiveStream, cams[0].HQStream)
	}
	caps, err := a.Capabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !caps.LPR || !caps.Review {
		t.Errorf("capabilities: %+v", caps)
	}
	stats, err := a.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Recordings == nil || stats.Cameras["plaza"].CameraFPS == 0 {
		t.Errorf("stats: %+v", stats)
	}
	reviews, err := a.Reviews(ctx, ReviewQuery{Cameras: []string{"plaza"}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reviews {
		if r.Camera != "plaza" {
			t.Errorf("camera filter ignored: %s", r.Camera)
		}
	}
}

func TestConnectPicksAdapterByVersion(t *testing.T) {
	ts := startMock(t, "0.18.0-rc1")
	a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, Username: "vms", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name() != "v018" {
		t.Errorf("want v018, got %s", a.Name())
	}
	old := startMock(t, "0.13.2")
	if _, err := Connect(context.Background(), ConnInfo{BaseURL: old.URL, Username: "vms", Password: "pw"}); err == nil {
		t.Error("0.13 must be rejected")
	}
}

func TestWrongPassword(t *testing.T) {
	ts := startMock(t, "0.17.2")
	_, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, Username: "vms", Password: "nope"})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestValidateBaseURL(t *testing.T) {
	for _, bad := range []string{"10.0.0.1:8971", "ftp://x", "http://x:5000", "https://user:pw@x:8971", "http://x:8971/?a=1"} {
		if _, err := ValidateBaseURL(bad, AuthCredentials); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	if _, err := ValidateBaseURL("http://x:5000", AuthCredentials); !errors.Is(err, ErrUnauthenticatedPort) {
		t.Error("port 5000 with credentials must be rejected with ErrUnauthenticatedPort")
	}
	if _, err := ValidateBaseURL("http://x:5000", AuthNone); err != nil {
		t.Errorf("port 5000 without login must be accepted: %v", err)
	}
	if _, err := ValidateBaseURL("https://10.20.0.11:8971/", AuthCredentials); err != nil {
		t.Error(err)
	}
}

func TestParseVersion(t *testing.T) {
	cases := map[string][2]int{"0.17.2-abc": {0, 17}, "0.18.0": {0, 18}, "1.0": {1, 0}}
	for in, want := range cases {
		ma, mi, ok := parseVersion(in)
		if !ok || ma != want[0] || mi != want[1] {
			t.Errorf("parseVersion(%q) = %d.%d %v", in, ma, mi, ok)
		}
	}
	if _, _, ok := parseVersion("garbage"); ok {
		t.Error("garbage must not parse")
	}
}

// startOpenMock is a Frigate without login, like the internal port 5000.
func startOpenMock(t *testing.T) *httptest.Server {
	t.Helper()
	cams, _ := frigatemock.ParseCameras("ruta_1+lpr:ingreso,plaza")
	store := frigatemock.NewStore(100)
	g := &frigatemock.Generator{Cameras: cams, Store: store}
	g.Seed(20, time.Hour, time.Now())
	srv := &frigatemock.Server{Version: "0.18.0-test", Cameras: cams, Store: store, RequireAuth: false, StartedAt: time.Now()}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestAuthNoneSkipsLogin(t *testing.T) {
	ts := startOpenMock(t)
	a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name() != "v018" {
		t.Errorf("want v018, got %s", a.Name())
	}
	// Credentials against an authenticated Frigate without them must fail.
	auth := startMock(t, "0.18.0")
	if _, err := Connect(context.Background(), ConnInfo{BaseURL: auth.URL, AuthMode: AuthNone}); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("want ErrUnauthorized without credentials, got %v", err)
	}
}

func TestTrackedObjectsCarryPlates(t *testing.T) {
	ts := startOpenMock(t)
	ctx := context.Background()
	a, err := Connect(ctx, ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	objs, err := a.TrackedObjects(ctx, ObjectQuery{Cameras: []string{"ruta_1"}, After: float64(time.Now().Add(-2 * time.Hour).Unix()), Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) == 0 {
		t.Fatal("no tracked objects")
	}
	for i := 1; i < len(objs); i++ {
		if objs[i].StartTime < objs[i-1].StartTime {
			t.Fatal("objects must be oldest first")
		}
	}
	plates := 0
	for _, o := range objs {
		if o.Camera != "ruta_1" {
			t.Errorf("camera filter ignored: %s", o.Camera)
		}
		if o.Plate != "" {
			plates++
		}
	}
	if plates == 0 {
		t.Error("LPR camera objects should carry recognized plates")
	}
}

func TestRecordingsAndExports(t *testing.T) {
	ts := startOpenMock(t)
	ctx := context.Background()
	a, err := Connect(ctx, ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	now := float64(time.Now().Unix())
	spans, err := a.Recordings(ctx, "plaza", now-7200, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) == 0 || len(spans) > 4 {
		t.Fatalf("10 s chunks should merge into a few spans, got %d", len(spans))
	}
	id, err := a.StartExport(ctx, "plaza", now-120, now-60, "test")
	if err != nil {
		t.Fatal(err)
	}
	info, err := a.Export(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !info.InProgress {
		t.Error("a fresh export should be in progress")
	}
	if _, err := a.StartExport(ctx, "nope", now-120, now-60, "x"); err == nil {
		t.Error("export of an unknown camera must fail")
	}
}

func TestMediaURLPath(t *testing.T) {
	if p, err := MediaURLPath("/media/frigate/clips/review/thumb-a-1.webp"); err != nil || p != "/clips/review/thumb-a-1.webp" {
		t.Errorf("got %q %v", p, err)
	}
	for _, bad := range []string{"/etc/passwd", "/media/frigate/../x", "clips/x"} {
		if _, err := MediaURLPath(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestCameraConfigGetAndUpdate(t *testing.T) {
	ts := startMock(t, "0.17.2-mock")
	ctx := context.Background()
	a, err := Connect(ctx, ConnInfo{BaseURL: ts.URL, Username: "vms", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Initial read
	cfg, err := a.GetCameraConfig(ctx, "acceso_norte")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DetectEnabled || !cfg.LPREnabled {
		t.Fatalf("expected detect=true, lpr=true, got %+v", cfg)
	}
	if len(cfg.Zones) != 2 || cfg.Zones[0] != "entrada" || cfg.Zones[1] != "salida" {
		t.Fatalf("unexpected zones: %+v", cfg.Zones)
	}

	// 2. Update config: turn off detect and change tracked objects
	f := false
	updated, err := a.UpdateCameraConfig(ctx, "acceso_norte", CameraFrigateConfigUpdate{
		DetectEnabled:  &f,
		TrackedObjects: []string{"car", "motorcycle"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DetectEnabled {
		t.Errorf("expected detect=false, got %+v", updated)
	}
	if len(updated.TrackedObjects) != 2 || updated.TrackedObjects[0] != "car" || updated.TrackedObjects[1] != "motorcycle" {
		t.Errorf("expected car and motorcycle, got %+v", updated.TrackedObjects)
	}

	// 3. Not found for unknown camera
	if _, err := a.GetCameraConfig(ctx, "unknown"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRestart(t *testing.T) {
	ts := startMock(t, "0.17.2-mock")
	ctx := context.Background()
	a, err := Connect(ctx, ConnInfo{BaseURL: ts.URL, Username: "vms", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Restart(ctx); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
}

func TestReviewByID(t *testing.T) {
	cams, _ := frigatemock.ParseCameras("plaza:centro")
	store := frigatemock.NewStore(10)
	end := float64(1700000100)
	store.Put(frigatemock.Review{ID: "r1", Camera: "plaza", StartTime: 1700000000, EndTime: &end, Severity: "alert",
		Data: frigatemock.ReviewData{Detections: []string{"d1"}, Objects: []string{"person"}}})
	srv := &frigatemock.Server{Version: "0.17.2-mock", Cameras: cams, Store: store, User: "vms", Password: "pw", RequireAuth: true, StartedAt: time.Now()}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	ctx := context.Background()
	a, err := Connect(ctx, ConnInfo{BaseURL: ts.URL, Username: "vms", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Review(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "r1" || got.EndTime == nil || *got.EndTime != end || len(got.Data.Detections) != 1 {
		t.Errorf("decoded review: %+v", got)
	}
	if _, err := a.Review(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing review: want ErrNotFound, got %v", err)
	}
}
