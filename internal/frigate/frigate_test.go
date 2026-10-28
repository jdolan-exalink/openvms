package frigate

import (
	"context"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
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
	ok := startMock(t, "0.17.0")
	a, err = Connect(context.Background(), ConnInfo{BaseURL: ok.URL, Username: "vms", Password: "pw"})
	if err != nil || a.Name() != "v017" {
		t.Errorf("0.17 must connect as v017: %v", err)
	}
}

func TestConnectRejectsVersionsBelow017(t *testing.T) {
	for _, version := range []string{"0.13.2", "0.14.1", "0.15.1", "0.16.0", "0.16.3-abc123"} {
		ts := startMock(t, version)
		a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, Username: "vms", Password: "pw"})
		if err == nil {
			t.Errorf("%s must be rejected", version)
			continue
		}
		if a != nil {
			t.Errorf("%s: no adapter may be returned", version)
		}
		if !errors.Is(err, ErrUnsupportedVersion) {
			t.Errorf("%s: want ErrUnsupportedVersion, got %v", version, err)
		}
		if msg := err.Error(); !strings.Contains(msg, version) || !strings.Contains(msg, "0.17") {
			t.Errorf("%s: error must name the detected version and the 0.17 minimum: %q", version, msg)
		}
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

func TestRecordingDaysListsTheDaysTheServerHasVideoOldestFirst(t *testing.T) {
	ts := startOpenMock(t)
	ctx := context.Background()
	a, err := Connect(ctx, ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	days, err := a.RecordingDays(ctx, "America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 3 || days[0] >= days[1] || days[1] >= days[2] {
		t.Fatalf("days = %v, want three dates, oldest first", days)
	}
	if want := time.Now().In(time.FixedZone("ART", -3*3600)).Format("2006-01-02"); days[2] != want {
		t.Fatalf("last day = %s, want today in the requested zone (%s)", days[2], want)
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

func labelsAdapter(t *testing.T, srv *frigatemock.Server) *v017 {
	t.Helper()
	srv.Version, srv.User, srv.Password, srv.RequireAuth = "0.17.2", "vms", "pw", true
	srv.StartedAt = time.Now()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, Username: "vms", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	return a.(*v017)
}

func TestModelLabelsDefaultIsCOCO(t *testing.T) {
	cams, _ := frigatemock.ParseCameras("a")
	labels, err := labelsAdapter(t, &frigatemock.Server{Cameras: cams, Store: frigatemock.NewStore(1)}).ModelLabels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 80 {
		t.Fatalf("COCO has 80 labels, got %d", len(labels))
	}
	for _, want := range []string{"person", "car", "motorcycle", "bus", "truck", "bicycle", "dog", "backpack"} {
		if !slices.Contains(labels, want) {
			t.Errorf("missing %q", want)
		}
	}
	if slices.Contains(labels, "???") || slices.Contains(labels, "license_plate") {
		t.Error("placeholders and non-COCO labels must not appear")
	}
}

func TestModelLabelsAppliesInlineLabelmap(t *testing.T) {
	cams, _ := frigatemock.ParseCameras("a")
	srv := &frigatemock.Server{Cameras: cams, Store: frigatemock.NewStore(1),
		ModelLabelmap: map[string]string{"0": "persona", "91": "package"}}
	labels, err := labelsAdapter(t, srv).ModelLabels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(labels, "person") || !slices.Contains(labels, "persona") || !slices.Contains(labels, "package") {
		t.Errorf("overrides not applied: %v", labels)
	}
}

func TestModelLabelsCustomFileAndUnreachableAreUnknown(t *testing.T) {
	cams, _ := frigatemock.ParseCameras("a")
	srv := &frigatemock.Server{Cameras: cams, Store: frigatemock.NewStore(1), ModelLabelmapPath: "/config/labels.txt"}
	a := labelsAdapter(t, srv)
	if _, err := a.ModelLabels(context.Background()); !errors.Is(err, ErrLabelsUnknown) {
		t.Fatalf("custom labelmap file: want ErrLabelsUnknown, got %v", err)
	}
	srv2 := &frigatemock.Server{Cameras: cams, Store: frigatemock.NewStore(1)}
	b := labelsAdapter(t, srv2)
	srv2.Offline.Store(true)
	if _, err := b.ModelLabels(context.Background()); err == nil {
		t.Fatal("unreachable frigate must return an error")
	}
}
