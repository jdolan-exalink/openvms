package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent/camproxy"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camweb"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type recordingForwarder struct {
	mu      sync.Mutex
	targets []string
	proxied bool
	status  int
	err     error
}

func (f *recordingForwarder) Forward(_ context.Context, _ authz.Actor, _ uuid.UUID, _ string, h http.Header, _ io.Reader) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targets = append(f.targets, h.Get(camproxy.HeaderTarget))
	if f.err != nil {
		return nil, f.err
	}
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
	if f.status != 0 {
		resp.StatusCode = f.status
	}
	if f.proxied {
		resp.Header.Set(camproxy.HeaderProxy, "1")
	}
	return resp, nil
}

func cameraWebHandlers(fwd camweb.Forwarder, authorize func(context.Context, authz.Actor, camweb.Camera) error, ready func(context.Context, authz.Actor, uuid.UUID) error) *Handlers {
	return &Handlers{CameraWeb: camweb.NewService(camweb.Config{}, fwd, authorize, ready)}
}

func okFn(context.Context, authz.Actor, uuid.UUID) error { return nil }

func okCam(context.Context, authz.Actor, camweb.Camera) error { return nil }

func TestCreateCameraWebSessionMapsOutcomes(t *testing.T) {
	cam := camweb.Camera{ID: uuid.New(), ServerID: uuid.New(), Host: "192.168.1.40"}
	a := authz.Actor{UserID: uuid.New()}
	fwd := &recordingForwarder{proxied: true}
	h := cameraWebHandlers(fwd, okCam, okFn)

	got, err := h.openCameraWeb(context.Background(), a, cam, nil)
	ok, isOK := got.(gen.CreateCameraWebSession200JSONResponse)
	if err != nil || !isOK || !strings.HasPrefix(ok.Url, "/camproxy/") || ok.ExpiresAt.IsZero() {
		t.Fatalf("default session = %#v, %v", got, err)
	}
	if fwd.targets[0] != "http://192.168.1.40:80" {
		t.Errorf("default target = %q", fwd.targets[0])
	}
	https := gen.CameraWebSessionRequestScheme("https")
	if _, err = h.openCameraWeb(context.Background(), a, cam, &gen.CreateCameraWebSessionJSONRequestBody{Scheme: &https}); err != nil || fwd.targets[1] != "https://192.168.1.40:443" {
		t.Errorf("https default port: %v %v", err, fwd.targets)
	}

	port22 := 22
	if got, _ := h.openCameraWeb(context.Background(), a, cam, &gen.CreateCameraWebSessionJSONRequestBody{Port: &port22}); !is[gen.CreateCameraWebSession400JSONResponse](got) {
		t.Errorf("port 22 = %#v, want 400", got)
	}
	noAddr := cam
	noAddr.Host = "127.0.0.1"
	if got, _ := h.openCameraWeb(context.Background(), a, noAddr, nil); !is[gen.CreateCameraWebSession404JSONResponse](got) {
		t.Errorf("loopback camera = %#v, want 404", got)
	}
	noAgent := cameraWebHandlers(fwd, okCam, func(context.Context, authz.Actor, uuid.UUID) error { return camweb.ErrNoAgent })
	if got, _ := noAgent.openCameraWeb(context.Background(), a, cam, nil); !is[gen.CreateCameraWebSession409JSONResponse](got) {
		t.Errorf("no agent = %#v, want 409", got)
	}
	old := cameraWebHandlers(&recordingForwarder{status: 404}, okCam, okFn) // an agent without the route: 404, no proxy marker
	if got, _ := old.openCameraWeb(context.Background(), a, cam, nil); !is[gen.CreateCameraWebSession409JSONResponse](got) {
		t.Errorf("old agent = %#v, want 409", got)
	}
	down := cameraWebHandlers(&recordingForwarder{err: errors.New("dial")}, okCam, okFn)
	if got, _ := down.openCameraWeb(context.Background(), a, cam, nil); !is[gen.CreateCameraWebSession502JSONResponse](got) {
		t.Errorf("unreachable = %#v, want 502", got)
	}
	deny := errors.New("forbidden")
	denied := cameraWebHandlers(fwd, func(context.Context, authz.Actor, camweb.Camera) error { return deny }, okFn)
	n := len(fwd.targets)
	if _, err := denied.openCameraWeb(context.Background(), a, cam, nil); !errors.Is(err, deny) {
		t.Errorf("permission error must pass through to the 403 mapper: %v", err)
	}
	if len(fwd.targets) != n {
		t.Errorf("the agent was contacted before the permission check passed")
	}
}

func is[T any](v any) bool { _, ok := v.(T); return ok }

func TestCamproxyRouteNeedsNoCookieButSessionsAreOpaque(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := cameraWebHandlers(&recordingForwarder{proxied: true}, okCam, okFn)
	h.Log = log
	router, err := NewRouter(h, log, Options{Queries: db.New(nil)}) // Authenticate is active: any use of Queries would panic
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/camproxy/" + strings.Repeat("a", 64) + "/", "/camproxy/", "/camproxy"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound || strings.TrimSpace(rec.Body.String()) != "" {
			t.Errorf("%s = %d %q, want a bare 404", path, rec.Code, rec.Body.String())
		}
		if csp := rec.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox ") || strings.Contains(csp, "allow-same-origin") {
			t.Errorf("%s: CSP = %q", path, csp)
		}
		if rec.Header().Get("X-Frame-Options") != "" {
			t.Errorf("%s: X-Frame-Options would block the iframe", path)
		}
	}
	// The session-creation API stays behind authentication.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/11111111-1111-1111-1111-111111111111/web/session", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated createCameraWebSession = %d, want 401", rec.Code)
	}
}
