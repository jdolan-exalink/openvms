package camweb

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent/camproxy"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

// fakeAgent stands in for the edge agent route: it applies the wire contract of camproxy to a fake
// camera (an httptest server), so the whole chain browser -> central -> agent -> camera is exercised.
type fakeAgent struct {
	camera *httptest.Server
	mu     sync.Mutex
	seen   []http.Header // headers of every call, as the agent received them
	err    error
	status int // when set, answers this status without the proxy marker (an old agent)
}

func (f *fakeAgent) Forward(ctx context.Context, _ authz.Actor, _ uuid.UUID, method string, header http.Header, body io.Reader) (*http.Response, error) {
	f.mu.Lock()
	f.seen = append(f.seen, header.Clone())
	err, status := f.err, f.status
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if status != 0 {
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("nope"))}, nil
	}
	uri := header.Get(camproxy.HeaderTargetURI)
	req, _ := http.NewRequestWithContext(ctx, method, f.camera.URL+uri, body)
	for k, vs := range header {
		if !strings.HasPrefix(strings.ToLower(k), "x-openvms-") && k != "Authorization" {
			req.Header[k] = vs
		}
	}
	if v := header.Get(camproxy.HeaderForwardAuthorization); v != "" {
		req.Header.Set("Authorization", v)
	}
	if v := header.Get(camproxy.HeaderForwardCookie); v != "" {
		req.Header.Set("Cookie", v)
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{DisableCompression: true}}).Do(req)
	if err != nil {
		return nil, err
	}
	for _, c := range resp.Header.Values("Set-Cookie") {
		resp.Header.Add(camproxy.HeaderSetCookie, c)
	}
	resp.Header.Del("Set-Cookie")
	resp.Header.Set(camproxy.HeaderProxy, "1")
	return resp, nil
}

func (f *fakeAgent) last() http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[len(f.seen)-1]
}

type env struct {
	t     *testing.T
	svc   *Service
	agent *fakeAgent
	cam   *http.ServeMux
	actor authz.Actor
	now   time.Time
	authz error
	nowMu sync.Mutex
}

func newEnv(t *testing.T, cfg Config) *env {
	t.Helper()
	e := &env{t: t, cam: http.NewServeMux(), now: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
	e.actor = authz.Actor{UserID: uuid.New(), Username: "ana"}
	camSrv := httptest.NewServer(e.cam)
	t.Cleanup(camSrv.Close)
	e.agent = &fakeAgent{camera: camSrv}
	cfg.Now = func() time.Time { e.nowMu.Lock(); defer e.nowMu.Unlock(); return e.now }
	e.svc = NewService(cfg, e.agent,
		func(context.Context, authz.Actor, Camera) error { return e.authz },
		func(context.Context, authz.Actor, uuid.UUID) error { return nil })
	return e
}

func (e *env) advance(d time.Duration) { e.nowMu.Lock(); e.now = e.now.Add(d); e.nowMu.Unlock() }

func (e *env) open() (prefix string) {
	e.t.Helper()
	return e.openAs(e.actor)
}

func (e *env) openWithCreds(user, pass string) string {
	e.t.Helper()
	o, err := e.svc.Open(context.Background(), e.actor, Camera{ID: uuid.New(), ServerID: uuid.New(), Host: "10.0.0.5", Username: user, Password: pass}, "http", 80)
	if err != nil {
		e.t.Fatalf("Open: %v", err)
	}
	return strings.TrimSuffix(o.URL, "/")
}

func (e *env) openAs(a authz.Actor) string {
	e.t.Helper()
	o, err := e.svc.Open(context.Background(), a, Camera{ID: uuid.New(), ServerID: uuid.New(), Host: "10.0.0.5"}, "http", 80)
	if err != nil {
		e.t.Fatalf("Open: %v", err)
	}
	return strings.TrimSuffix(o.URL, "/")
}

func (e *env) get(prefix, rest string, hdr ...string) *httptest.ResponseRecorder {
	return e.do(http.MethodGet, prefix, rest, nil, hdr...)
}

func (e *env) do(method, prefix, rest string, body io.Reader, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, prefix+rest, body)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Add(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	e.svc.ServeHTTP(rec, req)
	return rec
}

const wantCSP = "sandbox allow-scripts allow-forms allow-popups allow-modals"

func assertHardened(t *testing.T, rec *httptest.ResponseRecorder, what string) {
	t.Helper()
	h := rec.Header()
	if h.Get("Content-Security-Policy") != wantCSP {
		t.Errorf("%s: CSP = %q", what, h.Get("Content-Security-Policy"))
	}
	if strings.Contains(h.Get("Content-Security-Policy"), "allow-same-origin") {
		t.Errorf("%s: allow-same-origin present", what)
	}
	for k, want := range map[string]string{"Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff", "Cache-Control": "no-store"} {
		if h.Get(k) != want {
			t.Errorf("%s: %s = %q, want %q", what, k, h.Get(k), want)
		}
	}
	for _, k := range []string{"X-Frame-Options", "Strict-Transport-Security", "Set-Cookie", "Clear-Site-Data", "Refresh"} {
		if h.Get(k) != "" {
			t.Errorf("%s: %s leaked: %q", what, k, h.Get(k))
		}
	}
}

func TestOpenChecksPermissionAddressAgentAndReachability(t *testing.T) {
	e := newEnv(t, Config{})
	e.cam.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) }) // reachable: any answer counts
	cam := func(host string) Camera { return Camera{ID: uuid.New(), ServerID: uuid.New(), Host: host} }
	ctx := context.Background()

	e.authz = errors.New("forbidden")
	if _, err := e.svc.Open(ctx, e.actor, cam("10.0.0.5"), "http", 80); !errors.Is(err, e.authz) {
		t.Fatalf("permission error not returned: %v", err)
	}
	e.authz = nil
	for _, host := range []string{"", "127.0.0.1", "169.254.169.254", "100.64.0.1", "camera.lan", "::1", "8.8.8.8"} {
		if _, err := e.svc.Open(ctx, e.actor, cam(host), "http", 80); !errors.Is(err, ErrNoAddress) {
			t.Errorf("host %q: err = %v, want ErrNoAddress", host, err)
		}
	}
	if _, err := e.svc.Open(ctx, e.actor, cam("10.0.0.5"), "ftp", 80); !errors.Is(err, ErrBadRequest) {
		t.Errorf("scheme ftp: %v", err)
	}
	for _, p := range []int{0, 70000, 22} {
		if _, err := e.svc.Open(ctx, e.actor, cam("10.0.0.5"), "http", p); !errors.Is(err, ErrBadRequest) {
			t.Errorf("port %d: %v", p, err)
		}
	}
	e.svc.agentReady = func(context.Context, authz.Actor, uuid.UUID) error { return ErrNoAgent }
	if _, err := e.svc.Open(ctx, e.actor, cam("10.0.0.5"), "http", 80); !errors.Is(err, ErrNoAgent) {
		t.Errorf("no agent: %v", err)
	}
	e.svc.agentReady = func(context.Context, authz.Actor, uuid.UUID) error { return nil }
	e.agent.err = errors.New("dial")
	if _, err := e.svc.Open(ctx, e.actor, cam("10.0.0.5"), "http", 80); !errors.Is(err, ErrUnreachable) {
		t.Errorf("unreachable: %v", err)
	}
	e.agent.err, e.agent.status = nil, 404
	if _, err := e.svc.Open(ctx, e.actor, cam("10.0.0.5"), "http", 80); !errors.Is(err, ErrAgentUnsupported) {
		t.Errorf("old agent: %v", err)
	}
	e.agent.status = 0
	o, err := e.svc.Open(ctx, e.actor, cam("192.168.1.20"), "https", 8443)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(o.URL, "/camproxy/") || !strings.HasSuffix(o.URL, "/") || len(o.URL) != len("/camproxy//")+64 {
		t.Errorf("url = %q", o.URL)
	}
	if !o.ExpiresAt.Equal(e.now.Add(30 * time.Minute)) {
		t.Errorf("expires = %v", o.ExpiresAt)
	}
	if got := e.agent.last().Get(camproxy.HeaderTarget); got != "https://192.168.1.20:8443" {
		t.Errorf("probe target = %q", got)
	}
}

func TestUnknownAndExpiredSessionsAre404WithNoDetail(t *testing.T) {
	e := newEnv(t, Config{})
	e.cam.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	prefix := e.open()
	if rec := e.get(prefix, "/"); rec.Code != 200 {
		t.Fatalf("live session = %d", rec.Code)
	}
	for _, p := range []string{"/camproxy/" + strings.Repeat("0", 64), "/camproxy/short", "/camproxy/", "/camproxy"} {
		rec := e.get(p, "/")
		if rec.Code != 404 || strings.TrimSpace(rec.Body.String()) != "" {
			t.Errorf("%s = %d %q", p, rec.Code, rec.Body.String())
		}
		assertHardened(t, rec, "404")
	}
	// Sliding expiry: use keeps the session alive past the first TTL.
	e.advance(20 * time.Minute)
	if rec := e.get(prefix, "/"); rec.Code != 200 {
		t.Fatalf("after 20m = %d", rec.Code)
	}
	e.advance(20 * time.Minute)
	if rec := e.get(prefix, "/"); rec.Code != 200 {
		t.Fatalf("sliding expiry did not extend: %d", rec.Code)
	}
	e.advance(31 * time.Minute)
	if rec := e.get(prefix, "/"); rec.Code != 404 {
		t.Fatalf("expired = %d", rec.Code)
	}
}

func TestAbsoluteLifetimeEndsBusySessions(t *testing.T) {
	e := newEnv(t, Config{AbsoluteTTL: time.Hour})
	e.cam.HandleFunc("/", func(_ http.ResponseWriter, _ *http.Request) {})
	prefix := e.open()
	for i := 0; i < 5; i++ {
		e.advance(14 * time.Minute)
		e.get(prefix, "/")
	}
	if rec := e.get(prefix, "/"); rec.Code != 404 {
		t.Fatalf("session older than the absolute lifetime = %d", rec.Code)
	}
}

func TestPerUserSessionCap(t *testing.T) {
	e := newEnv(t, Config{})
	e.cam.HandleFunc("/", func(_ http.ResponseWriter, _ *http.Request) {})
	var prefixes []string
	for i := 0; i < 5; i++ {
		prefixes = append(prefixes, e.open())
		e.advance(time.Second)
	}
	if rec := e.get(prefixes[0], "/"); rec.Code != 404 {
		t.Errorf("the oldest session survived the cap: %d", rec.Code)
	}
	for _, p := range prefixes[1:] {
		if rec := e.get(p, "/"); rec.Code != 200 {
			t.Errorf("a recent session was evicted: %d", rec.Code)
		}
	}
	other := authz.Actor{UserID: uuid.New(), Username: "bob"}
	op := e.openAs(other)
	if rec := e.get(op, "/"); rec.Code != 200 {
		t.Errorf("another user's session = %d", rec.Code)
	}
	if rec := e.get(prefixes[4], "/"); rec.Code != 200 {
		t.Errorf("a user's cap evicted by another user's session: %d", rec.Code)
	}
}

func TestSessionIsRevalidatedAgainstThePermission(t *testing.T) {
	e := newEnv(t, Config{ReauthEvery: time.Minute})
	e.cam.HandleFunc("/", func(_ http.ResponseWriter, _ *http.Request) {})
	prefix := e.open()
	e.authz = errors.New("revoked")
	if rec := e.get(prefix, "/"); rec.Code != 200 {
		t.Fatalf("within the revalidation window = %d", rec.Code)
	}
	e.advance(2 * time.Minute)
	if rec := e.get(prefix, "/"); rec.Code != 404 {
		t.Fatalf("revoked permission still served: %d", rec.Code)
	}
	e.authz = nil
	if rec := e.get(prefix, "/"); rec.Code != 404 {
		t.Fatalf("a session ended by revocation came back: %d", rec.Code)
	}
}

func TestForwardedRequestCarriesNoOpenVMSCredentials(t *testing.T) {
	e := newEnv(t, Config{})
	var got http.Header
	var gotURI string
	e.cam.HandleFunc("/", func(_ http.ResponseWriter, r *http.Request) { got, gotURI = r.Header.Clone(), r.URL.RequestURI() })
	prefix := e.open()
	e.get(prefix, "/cgi/x?a=1", "Cookie", "openvms_session=SECRET; other=1", "Authorization", "Bearer TOKEN",
		"X-OpenVMS-Request", "1", "X-Forwarded-For", "9.9.9.9", "Origin", "null", "Referer", "https://vms.example/camproxy/abc/", "Accept-Encoding", "gzip", "Connection", "X-Hop", "X-Hop", "1")
	if gotURI != "/cgi/x?a=1" {
		t.Errorf("uri = %q", gotURI)
	}
	for k, vs := range got {
		joined := strings.Join(vs, ",")
		if strings.Contains(joined, "SECRET") || strings.Contains(joined, "TOKEN") || strings.Contains(strings.ToLower(k), "openvms") {
			t.Errorf("OpenVMS credential reached the camera: %s: %s", k, joined)
		}
	}
	if got.Get("Authorization") != "" || got.Get("Cookie") != "" || got.Get("X-Forwarded-For") != "" || got.Get("X-Hop") != "" {
		t.Errorf("camera headers: %v", got)
	}
	if got.Get("Origin") != "http://10.0.0.5" && got.Get("Origin") != "http://10.0.0.5:80" {
		t.Errorf("Origin = %q, want the camera origin", got.Get("Origin"))
	}
	if strings.Contains(got.Get("Referer"), "vms.example") {
		t.Errorf("Referer leaked: %q", got.Get("Referer"))
	}
	sent := e.agent.last()
	if sent.Get("Accept-Encoding") != "identity" || sent.Get(camproxy.HeaderTarget) != "http://10.0.0.5:80" {
		t.Errorf("agent headers: %v", sent)
	}
	// The camera's own basic-auth credentials do pass, and only those.
	e.get(prefix, "/", "Authorization", "Basic YTpi")
	if e.agent.last().Get(camproxy.HeaderForwardAuthorization) != "Basic YTpi" {
		t.Errorf("basic credentials not forwarded: %v", e.agent.last())
	}
	e.get(prefix, "/", "Authorization", "Bearer abc")
	if e.agent.last().Get(camproxy.HeaderForwardAuthorization) != "" {
		t.Errorf("a bearer token was forwarded to the camera")
	}
}

func TestCameraCookiesStayInTheServerSideJar(t *testing.T) {
	e := newEnv(t, Config{})
	var cookie string
	e.cam.HandleFunc("/login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "SID=cam-secret; Path=/")
	})
	e.cam.HandleFunc("/next", func(_ http.ResponseWriter, r *http.Request) { cookie = r.Header.Get("Cookie") })
	prefix := e.open()
	rec := e.get(prefix, "/login")
	if rec.Header().Get("Set-Cookie") != "" || strings.Contains(fmt.Sprint(rec.Header()), "cam-secret") {
		t.Fatalf("camera cookie reached the browser: %v", rec.Header())
	}
	assertHardened(t, rec, "login")
	e.get(prefix, "/next")
	if cookie != "SID=cam-secret" {
		t.Fatalf("camera did not get its cookie back: %q", cookie)
	}
	// A second session has its own jar.
	other := e.open()
	cookie = "unset"
	e.get(other, "/next")
	if cookie != "" {
		t.Fatalf("jar shared between sessions: %q", cookie)
	}
}

func TestEveryResponseIsHardened(t *testing.T) {
	e := newEnv(t, Config{})
	e.cam.HandleFunc("/hostile", func(w http.ResponseWriter, _ *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src *; sandbox allow-same-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Strict-Transport-Security", "max-age=1")
		h.Set("Referrer-Policy", "unsafe-url")
		h.Set("Clear-Site-Data", `"cookies"`)
		h.Set("Refresh", "0;url=http://evil/")
		h.Set("Cache-Control", "public, max-age=999")
		h.Set("X-OpenVMS-Proxy-Error", "spoof")
		h.Set("Access-Control-Allow-Origin", "http://evil")
		h.Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("x"))
	})
	prefix := e.open()
	rec := e.get(prefix, "/hostile")
	assertHardened(t, rec, "hostile")
	if rec.Header().Get("X-OpenVMS-Proxy-Error") != "" || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("headers = %v", rec.Header())
	}
	e.agent.err = errors.New("boom")
	rec = e.get(prefix, "/x")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("agent error = %d", rec.Code)
	}
	assertHardened(t, rec, "502")
	e.agent.err = nil
	e.agent.status = 403 // the agent refused the token
	rec = e.get(prefix, "/x")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("answer without the proxy marker = %d, want 502", rec.Code)
	}
	assertHardened(t, rec, "unmarked")
}

func TestLocationRewriting(t *testing.T) {
	e := newEnv(t, Config{})
	locs := map[string]string{
		"/abs-same":   "http://10.0.0.5/login.html?x=1",
		"/abs-port":   "http://10.0.0.5:80/a/b",
		"/rootrel":    "/web/index.html",
		"/relative":   "next.html",
		"/other-host": "http://10.0.0.6/",
		"/public":     "http://evil.example/steal",
		"/other-port": "http://10.0.0.5:8080/",
		"/https":      "https://10.0.0.5/",
		"/protorel":   "//evil.example/x",
		"/protosame":  "//10.0.0.5/ok",
	}
	e.cam.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if loc, ok := locs[r.URL.Path]; ok {
			w.Header().Set("Location", loc)
			w.WriteHeader(http.StatusFound)
			return
		}
		prefixed := r.URL.Query().Get("prefixed")
		w.Header().Set("Location", prefixed)
		w.WriteHeader(http.StatusFound)
	})
	prefix := e.open()
	want := map[string]string{
		"/abs-same":  prefix + "/login.html?x=1",
		"/abs-port":  prefix + "/a/b",
		"/rootrel":   prefix + "/web/index.html",
		"/relative":  "next.html",
		"/protosame": prefix + "/ok",
	}
	for path, loc := range want {
		rec := e.get(prefix, path)
		if rec.Code != 302 || rec.Header().Get("Location") != loc {
			t.Errorf("%s: %d Location %q, want %q", path, rec.Code, rec.Header().Get("Location"), loc)
		}
		assertHardened(t, rec, path)
	}
	for _, path := range []string{"/other-host", "/public", "/other-port", "/https", "/protorel"} {
		rec := e.get(prefix, path)
		if rec.Code != http.StatusBadGateway || rec.Header().Get("Location") != "" {
			t.Errorf("%s: %d Location %q, want 502 and no Location", path, rec.Code, rec.Header().Get("Location"))
		}
		assertHardened(t, rec, path)
	}
	rec := e.get(prefix, "/x?prefixed="+prefix+"/already")
	if rec.Header().Get("Location") != prefix+"/already" {
		t.Errorf("double prefix: %q", rec.Header().Get("Location"))
	}
}

func TestHTMLAndCSSRewriting(t *testing.T) {
	e := newEnv(t, Config{})
	var body, ctype string
	e.cam.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ctype)
		_, _ = w.Write([]byte(body))
	})
	prefix := e.open()
	ctype = "text/html; charset=utf-8"
	body = `<html><head><title>x</title><base href="http://evil/"><script src="/a.js"></script><link rel=stylesheet href='/b.css'></head>` +
		`<body style="background:url(/c.png)"><form action="/set"><img src="` + prefix + `/done.png"><script src="//cdn.example/x.js"></script>` +
		`<img src="rel/y.png"><a href="/">home</a><style>@import "/d.css"; .a{background:url('/e.png')} .b{background:url("/f.png")}</style></body></html>`
	rec := e.get(prefix, "/ui/index.html")
	out := rec.Body.String()
	for _, want := range []string{
		`src="` + prefix + `/a.js"`, `href='` + prefix + `/b.css'`, `url(` + prefix + `/c.png)`, `action="` + prefix + `/set"`,
		`src="` + prefix + `/done.png"`, `src="//cdn.example/x.js"`, `src="rel/y.png"`, `href="` + prefix + `/"`,
		`@import "` + prefix + `/d.css"`, `url('` + prefix + `/e.png')`, `url("` + prefix + `/f.png")`,
		`<base href="` + prefix + `/ui/">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, prefix+prefix) || strings.Contains(out, "evil/") {
		t.Errorf("double prefix or camera base survived:\n%s", out)
	}
	if strings.Count(out, "<base ") != 1 || strings.Count(out, "(function(){var P=") != 1 {
		t.Errorf("base/script must be injected exactly once:\n%s", out)
	}
	if rec.Header().Get("Content-Length") != fmt.Sprint(len(out)) {
		t.Errorf("Content-Length %q, body %d", rec.Header().Get("Content-Length"), len(out))
	}
	assertHardened(t, rec, "html")

	// No <head>: still injected once, at the start.
	body = `<p><img src="/z.png"></p>`
	out = e.get(prefix, "/").Body.String()
	if strings.Count(out, "<base ") != 1 || !strings.Contains(out, `src="`+prefix+`/z.png"`) {
		t.Errorf("headless html:\n%s", out)
	}

	ctype = "text/css"
	body = `@import url(/x.css); .a{background:url(/y.png)} .b{background:url(//cdn/z.png)} .c{background:url(` + prefix + `/w.png)} .d{background:url(data:image/png;base64,AAA)}`
	out = e.get(prefix, "/css/site.css").Body.String()
	for _, want := range []string{`url(` + prefix + `/x.css)`, `url(` + prefix + `/y.png)`, `url(//cdn/z.png)`, `url(` + prefix + `/w.png)`, `url(data:image/png;base64,AAA)`} {
		if !strings.Contains(out, want) {
			t.Errorf("css missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "<base") {
		t.Errorf("css got html injection")
	}

	ctype = "application/javascript"
	body = `var u = "/api/x"; fetch('/y');`
	if got := e.get(prefix, "/app.js").Body.String(); got != body {
		t.Errorf("javascript must not be rewritten: %q", got)
	}
}

func TestJavaScriptLocationRewriting(t *testing.T) {
	e := newEnv(t, Config{})
	var body, ctype string
	e.cam.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ctype)
		_, _ = w.Write([]byte(body))
	})
	prefix := e.open()

	// JS file with Content-Type application/javascript
	ctype = "application/javascript"
	body = `var o=window.top.location.protocol; n=r(window.top.location.hostname); if(top.location!=self.location){desktop.location=1;$top.location=2;a.top.location=3;top.locations=4;}`
	rec := e.get(prefix, "/chunk.js")
	want := `var o=__camTopLoc.protocol; n=r(__camTopLoc.hostname); if(__camTopLoc!=self.location){desktop.location=1;$top.location=2;a.top.location=3;__camTopW().locations=4;}`
	if got := rec.Body.String(); got != want {
		t.Fatalf("chunk.js rewrite mismatch:\ngot:  %s\nwant: %s", got, want)
	}

	// JS file without Content-Type or generic application/octet-stream, identified by .js extension
	ctype = "application/octet-stream"
	body = `s=window.top.location.protocol+"//"+i;window.parent.location.href="/x";parent.location="/y";`
	rec = e.get(prefix, "/app.js")
	want = `s=__camTopLoc.protocol+"//"+i;__camTopLoc.href="/x";__camTopLoc.href = "/y";`
	if got := rec.Body.String(); got != want {
		t.Fatalf("app.js rewrite mismatch:\ngot:  %s\nwant: %s", got, want)
	}

	// Inline script in HTML
	ctype = "text/html"
	body = `<html><head><script>var p=window.top.location.port;</script></head><body></body></html>`
	rec = e.get(prefix, "/index.html")
	if !strings.Contains(rec.Body.String(), `var p=__camTopLoc.port;`) {
		t.Fatalf("inline script in HTML not rewritten:\n%s", rec.Body.String())
	}
}

func TestNonTextAndOversizedBodiesAreStreamedUntouched(t *testing.T) {
	e := newEnv(t, Config{RewriteLimit: 64})
	payload := `src="/a" url(/b)` + strings.Repeat("\x00\xff", 50)
	e.cam.HandleFunc("/img", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(payload))
	})
	big := `<html><head></head><body><img src="/a.png">` + strings.Repeat("x", 200) + `</body></html>`
	e.cam.HandleFunc("/big", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(big))
	})
	e.cam.HandleFunc("/chunked", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(big[:60]))
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte(big[60:]))
	})
	prefix := e.open()
	if rec := e.get(prefix, "/img"); rec.Body.String() != payload || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("binary body changed")
	}
	for _, p := range []string{"/big", "/chunked"} {
		rec := e.get(prefix, p)
		if rec.Body.String() != big {
			t.Errorf("%s: body over the rewrite limit must pass untouched, got %d bytes", p, rec.Body.Len())
		}
		assertHardened(t, rec, p)
	}
}

func TestPathCannotChangeTheTarget(t *testing.T) {
	e := newEnv(t, Config{})
	e.cam.HandleFunc("/", func(_ http.ResponseWriter, _ *http.Request) {})
	prefix := e.open()
	for _, rest := range []string{"/../x", "/a/../../x", "/..%2fx", "/%2e%2e/x", "/a/%2E%2E%2Fx", "/..\\x"} {
		rec := e.get(prefix, rest)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q = %d, want 400", rest, rec.Code)
		}
	}
	for rest, want := range map[string]string{"//evil.example/x": "/evil.example/x", "/a//b": "/a/b", "/p%20q?x=1": "/p%20q?x=1", "": "/"} {
		rec := e.get(prefix, rest)
		if rec.Code != 200 {
			t.Fatalf("%q = %d", rest, rec.Code)
		}
		h := e.agent.last()
		if h.Get(camproxy.HeaderTargetURI) != want || h.Get(camproxy.HeaderTarget) != "http://10.0.0.5:80" {
			t.Errorf("%q -> target %q uri %q, want uri %q", rest, h.Get(camproxy.HeaderTarget), h.Get(camproxy.HeaderTargetURI), want)
		}
	}
	// A host smuggled into the session segment is just an unknown session.
	if rec := e.get("/camproxy/evil.example", "/x"); rec.Code != 404 {
		t.Errorf("smuggled host = %d", rec.Code)
	}
}

func TestLimitsAndMethods(t *testing.T) {
	e := newEnv(t, Config{MaxRequestBody: 8, PerSessionConcurrency: 2, QueueTimeout: 20 * time.Millisecond})
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	e.cam.HandleFunc("/slow", func(_ http.ResponseWriter, _ *http.Request) { started <- struct{}{}; <-release })
	e.cam.HandleFunc("/", func(_ http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body) })
	prefix := e.open()
	for _, m := range []string{http.MethodTrace, http.MethodConnect, http.MethodPatch, "PROPFIND"} {
		rec := e.do(m, prefix, "/", nil)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", m, rec.Code)
		}
		assertHardened(t, rec, m)
	}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodHead, http.MethodOptions} {
		if rec := e.do(m, prefix, "/", strings.NewReader("1234")); rec.Code != 200 {
			t.Errorf("%s = %d, want 200", m, rec.Code)
		}
	}
	if rec := e.do(http.MethodPost, prefix, "/", strings.NewReader("123456789")); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body = %d, want 413", rec.Code)
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); e.get(prefix, "/slow") }()
	}
	<-started
	<-started
	rec := e.get(prefix, "/x")
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("over the per-session concurrency = %d, want 429", rec.Code)
	}
	assertHardened(t, rec, "429")
	close(release)
	wg.Wait()
}

func TestConcurrencyQueueing(t *testing.T) {
	e := newEnv(t, Config{PerSessionConcurrency: 1, QueueTimeout: 200 * time.Millisecond})
	unblock := make(chan struct{})
	e.cam.HandleFunc("/hold", func(_ http.ResponseWriter, _ *http.Request) {
		<-unblock
	})
	e.cam.HandleFunc("/quick", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	prefix := e.open()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = e.get(prefix, "/hold")
	}()

	time.Sleep(10 * time.Millisecond)

	// Second request arrives while first is holding the single slot. It queues.
	queuedDone := make(chan int)
	go func() {
		rec := e.get(prefix, "/quick")
		queuedDone <- rec.Code
	}()

	// Release first request after 30ms. The queued request should acquire the slot and succeed (200).
	time.Sleep(30 * time.Millisecond)
	close(unblock)
	<-done

	select {
	case code := <-queuedDone:
		if code != http.StatusOK {
			t.Errorf("queued request got %d, want 200", code)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("queued request timed out")
	}
}

func TestCORSPreflightIsAnsweredLocally(t *testing.T) {
	e := newEnv(t, Config{})
	hits := 0
	e.cam.HandleFunc("/", func(_ http.ResponseWriter, _ *http.Request) { hits++ })
	prefix := e.open()
	hits = 0 // opening probed the camera
	before := len(e.agent.seen)
	rec := e.do(http.MethodOptions, prefix, "/api", nil, "Origin", "null", "Access-Control-Request-Method", "POST", "Access-Control-Request-Headers", "x-requested-with, authorization")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers")), "authorization") {
		t.Fatalf("preflight = %d %v", rec.Code, rec.Header())
	}
	if len(e.agent.seen) != before || hits != 0 {
		t.Errorf("a preflight reached the camera")
	}
	assertHardened(t, rec, "preflight")
	if e.do(http.MethodOptions, prefix, "/", nil); hits != 1 {
		t.Errorf("a plain OPTIONS must be forwarded; hits=%d", hits)
	}
}

func TestBasicChallengeRealmIsScopedToTheSession(t *testing.T) {
	e := newEnv(t, Config{})
	e.cam.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="IPCam"`)
		w.WriteHeader(401)
	})
	a, b := e.open(), e.open()
	ra, rb := e.get(a, "/").Header().Get("WWW-Authenticate"), e.get(b, "/").Header().Get("WWW-Authenticate")
	if !strings.HasPrefix(ra, `Basic realm="IPCam`) || ra == rb || strings.Count(ra, "realm=") != 1 {
		t.Errorf("realms must differ per session so the browser never sends one camera's password to another: %q vs %q", ra, rb)
	}
}

func TestServiceNeedsNoBackgroundGoroutines(t *testing.T) {
	e := newEnv(t, Config{})
	e.cam.HandleFunc("/", func(_ http.ResponseWriter, _ *http.Request) {})
	for i := 0; i < 20; i++ {
		e.openAs(authz.Actor{UserID: uuid.New()})
	}
	e.advance(time.Hour)
	e.openAs(e.actor) // opening sweeps the expired ones
	if n := e.svc.count(); n != 1 {
		t.Fatalf("%d sessions kept after expiry", n)
	}
}

func TestBasicAuthAutoLoginWithStoredCredentials(t *testing.T) {
	e := newEnv(t, Config{})
	e.cam.HandleFunc("/protected", func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "admin" || p != "cam12345" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Camera"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("logged in"))
	})
	prefix := e.openWithCreds("admin", "cam12345")
	rec := e.get(prefix, "/protected")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected auto-login status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "logged in\n" && rec.Body.String() != "logged in" {
		t.Fatalf("expected 'logged in', got %q", rec.Body.String())
	}
}

func TestHTMLAutofillScriptInjected(t *testing.T) {
	e := newEnv(t, Config{})
	e.cam.HandleFunc("/login.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head></head><body><form><input type="text" name="user"><input type="password" name="pwd"></form></body></html>`))
	})
	prefix := e.openWithCreds("admin", "secretpass")
	rec := e.get(prefix, "/login.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "admin") || !strings.Contains(body, "secretpass") {
		t.Fatalf("expected injected autofill script containing credentials, got %s", body)
	}
}

func TestRewriteJSProvisionISRFixes(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "rewrite wssPort to safe fallback",
			input:    `f="http:"===location.protocol?c.config.port.wsPort.toString():c.config.port.wssPort.toString()`,
			contains: `(c.config.port.wssPort?c.config.port.wssPort.toString():(c.config.port.wsPort?c.config.port.wsPort.toString():"80"))`,
		},
		{
			name:     "rewrite location.protocol to __camLoc.protocol",
			input:    `"http:" === location.protocol`,
			contains: `"http:" === __camLoc.protocol`,
		},
		{
			name:     "rewrite window.location.href assignment",
			input:    `window.location.href="/index.html"`,
			contains: `__camLoc.href="/index.html"`,
		},
		{
			name:     "rewrite window.location.href root assignment",
			input:    `window.location.href="/"`,
			contains: `__camLoc.href="/"`,
		},
		{
			name:     "rewrite location assignment",
			input:    `location = "/login"`,
			contains: `__camLoc.href = "/login"`,
		},
		{
			name:     "rewrite location.replace",
			input:    `location.replace("/home")`,
			contains: `__camLoc.replace("/home")`,
		},
		{
			name:     "rewrite location.assign",
			input:    `location.assign("/home")`,
			contains: `__camLoc.assign("/home")`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := string(rewriteJS([]byte(tc.input)))
			if !strings.Contains(out, tc.contains) {
				t.Fatalf("expected output to contain %q, got %q", tc.contains, out)
			}
		})
	}
}

func TestRewriteJSPreservesNonLocationAssignments(t *testing.T) {
	input := `if (location == x || location === y) { other.location.href = z; }`
	out := string(rewriteJS([]byte(input)))
	if strings.Contains(out, "__camLoc.href =") {
		t.Fatalf("equality comparison should not be rewritten as assignment: %s", out)
	}
	if strings.Contains(out, "other.__camLoc.href") {
		t.Fatalf("other.location.href should not be rewritten: %s", out)
	}
}

func TestShimScriptInjectsCamLocAndCamProto(t *testing.T) {
	shim := shimScriptFor("/camproxy/test1234", "http", "admin", "secret")
	if !strings.Contains(shim, `var __camProto="http:";`) {
		t.Fatalf("expected __camProto to be injected with 'http:', got %s", shim)
	}
	if !strings.Contains(shim, `var __camLoc=window.__camLoc=`) {
		t.Fatalf("expected __camLoc definition in shim, got %s", shim)
	}
	// Verify that if username in form is already 'admin', autofill does NOT overwrite it with a different user 'frigate'
	shimFrigate := shimScriptFor("/camproxy/test1234", "http", "frigate", "secret")
	if !strings.Contains(shimFrigate, `u==="admin"`) && strings.Contains(shimFrigate, `un.value==="admin"`) {
		t.Fatalf("autofill must not overwrite existing admin username with a non-admin username like frigate")
	}
}

func TestRewriteJSFallsBackToSelfForCrossFrameReads(t *testing.T) {
	cases := []struct{ name, input, contains string }{
		{
			name:     "window.top shared global",
			input:    `$LAB.script("Scripts/Third/jquery.xmlns.js?v=" + window.top.global_version)`,
			contains: `__camTopW().global_version`,
		},
		{
			name:     "window.parent property read",
			input:    `var v = window.parent.someGlobal;`,
			contains: `__camParentW().someGlobal`,
		},
		{
			name:     "bare top read",
			input:    `var v = top.global_version;`,
			contains: `__camTopW().global_version`,
		},
		{
			name:     "window.top.location becomes the emulated camera top",
			input:    `if (window.top.location.href.indexOf("login.htm") > -1) { window.top.location.href = serverBase; }`,
			contains: `if (__camTopLoc.href.indexOf("login.htm") > -1) { __camTopLoc.href = serverBase; }`,
		},
		{
			name:     "top login frame retains its child document when available and otherwise uses itself",
			input:    `$(window.top.frames[0].document).click(hideMenus);`,
			contains: `$(__camFrame0Doc(__camTopW())).click(hideMenus);`,
		},
		{
			name:     "nested parent frame falls back without dereferencing the opaque ancestor",
			input:    `$(window.parent.frames[0].document).click(hideMenus);`,
			contains: `$(__camFrame0Doc(window.parent)).click(hideMenus);`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := string(rewriteJS([]byte(tc.input)))
			if !strings.Contains(out, tc.contains) {
				t.Fatalf("expected output to contain %q, got %q", tc.contains, out)
			}
		})
	}
}

func TestShimScriptDefinesEmulatedCameraTop(t *testing.T) {
	shim := shimScriptFor("/camproxy/test1234", "http", "", "")
	for _, want := range []string{`var __camLoc=window.__camLoc={get href(){return window.location.href}`, `window.__camTopLoc={get href(){return window.location.origin+__camRoot}`, `protocol:__camProto`} {
		if !strings.Contains(shim, want) {
			t.Fatalf("shim must define __camTopLoc with %q, got %s", want, shim)
		}
	}
}

// A camera login frame in the opaque sandbox cannot read its parent. Its "am I the top document?"
// check (top.location.href contains "login.htm") must therefore see the camera root, never the
// frame's own address, or the page redirects itself and aborts its initialization.
func TestShimScriptTopLocationIsCameraRootNotFrameAddress(t *testing.T) {
	shim := shimScriptFor("/camproxy/test1234", "http", "", "")
	for _, want := range []string{
		`get href(){return window.location.origin+__camRoot}`,
		`set href(u){__camNavTop(u)}`,
		`assign:function(u){__camNavTop(u)}`,
		`replace:function(u){__camNavTop(u)}`,
		`window.parent.postMessage({__camTopNav:t.href},"*")`,
	} {
		if !strings.Contains(shim, want) {
			t.Fatalf("camera top-location shim missing %q", want)
		}
	}
	if strings.Contains(shim, "__camProxyTop") {
		t.Fatal("shim must not read the parent frame's location")
	}
}

func TestCameraTopLocationRedirectStaysInsideProxy(t *testing.T) {
	input := `window.top.location="/Pages/main.htm";top.location.href="/Pages/home.htm";`
	want := `__camTopLoc.href = "/Pages/main.htm";__camTopLoc.href="/Pages/home.htm";`
	if got := string(rewriteJS([]byte(input))); got != want {
		t.Fatalf("camera login redirect must navigate within its camproxy session:\ngot:  %s\nwant: %s", got, want)
	}

	shim := shimScriptFor("/camproxy/test1234", "http", "", "")
	for _, want := range []string{`if(t.origin!==window.location.origin||(t.pathname!==P&&t.pathname.indexOf(P+"/")!==0))return;`} {
		if !strings.Contains(shim, want) {
			t.Fatalf("camera top-location navigation must be confined to the proxy session, missing %q", want)
		}
	}
}

func TestShimPrefixesBareRootInDynamicMarkup(t *testing.T) {
	shim := shimScriptFor("/camproxy/test1234", "http", "", "")
	if !strings.Contains(shim, `s.replace(/(src|href|action)=["'](\/(?!\/)[^"']*)["']/gi`) {
		t.Fatal("document.write markup rewriter must proxy a bare root URL without matching protocol-relative URLs")
	}
}

// A nested camera frame cannot read window.top, and the libraries the camera's top page loaded
// (md5, crypto-js) are not defined in it. The fallback must load them on demand, otherwise the
// second step of the camera's two-step login throws and the credentials are never sent.
func TestShimScriptFallbackLoadsTopLibrariesOnDemand(t *testing.T) {
	shim := shimScriptFor("/camproxy/test1234", "http", "", "")
	for _, want := range []string{`window.__camSelf=new Proxy(window,`, `\.script\(`, `(0,eval)(r.responseText)`} {
		if !strings.Contains(shim, want) {
			t.Fatalf("shim missing %q", want)
		}
	}
	got := string(rewriteJS([]byte(`h=window.top.hex_md5(p);`)))
	if want := `h=__camTopW().hex_md5(p);`; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

// The camera UI keeps its login token in sessionStorage, which does not exist in the opaque-origin
// iframe. The proxy stores it per session so every frame, and every later page, sees the same values.
func TestSessionStateIsSharedWithinSessionAndNeverReachesCamera(t *testing.T) {
	e := newEnv(t, Config{})
	var reached bool
	e.cam.HandleFunc("/__camstate", func(http.ResponseWriter, *http.Request) { reached = true })
	a, b := e.open(), e.open()

	if rec := e.do(http.MethodPost, a, "/__camstate", strings.NewReader(`{"op":"set","k":"s:token_ipc","v":"T1"}`)); rec.Code != http.StatusNoContent {
		t.Fatalf("set: %d %s", rec.Code, rec.Body.String())
	}
	e.do(http.MethodPost, a, "/__camstate", strings.NewReader(`{"op":"set","k":"l:lang","v":"en"}`))
	got := e.get(a, "/__camstate")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"s:token_ipc":"T1"`) {
		t.Fatalf("state not readable by a later frame: %d %s", got.Code, got.Body.String())
	}
	if other := e.get(b, "/__camstate").Body.String(); strings.Contains(other, "T1") {
		t.Fatalf("state leaked across sessions: %s", other)
	}
	e.do(http.MethodPost, a, "/__camstate", strings.NewReader(`{"op":"clear","p":"s:"}`))
	after := e.get(a, "/__camstate").Body.String()
	if strings.Contains(after, "token_ipc") || !strings.Contains(after, "l:lang") {
		t.Fatalf("clear must drop only the sessionStorage prefix: %s", after)
	}
	if rec := e.do(http.MethodPost, a, "/__camstate", strings.NewReader(`{"op":"set","k":"s:big","v":"`+strings.Repeat("x", maxStateValue+1)+`"}`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized value must be refused, got %d", rec.Code)
	}
	if reached {
		t.Fatal("state requests must not be forwarded to the camera")
	}
}

func TestShimBacksStorageWithSessionState(t *testing.T) {
	shim := shimScriptFor("/camproxy/test1234", "http", "", "")
	for _, want := range []string{`x.open(m,P+"/__camstate",false)`, `var ss=S("s:")`, `var ls=S("l:")`} {
		if !strings.Contains(shim, want) {
			t.Fatalf("shim missing %q", want)
		}
	}
}

// tunnelAgent adds tunnels to fakeAgent: the "camera" end of each tunnel is a pipe handed to cam.
type tunnelAgent struct {
	*fakeAgent
	cam    func(header http.Header, c net.Conn)
	opened chan http.Header
}

func (a *tunnelAgent) Tunnel(_ context.Context, _ authz.Actor, _ uuid.UUID, header http.Header) (io.ReadWriteCloser, error) {
	client, camera := net.Pipe()
	if a.opened != nil {
		a.opened <- header.Clone()
	}
	go a.cam(header, camera)
	return client, nil
}

func wsServer(t *testing.T, cam func(header http.Header, c net.Conn)) (srv *httptest.Server, prefix string, agent *tunnelAgent) {
	t.Helper()
	e := newEnv(t, Config{})
	agent = &tunnelAgent{fakeAgent: e.agent, cam: cam, opened: make(chan http.Header, 4)}
	svc := NewService(Config{}, agent,
		func(context.Context, authz.Actor, Camera) error { return nil },
		func(context.Context, authz.Actor, uuid.UUID) error { return nil })
	o, err := svc.Open(context.Background(), e.actor, Camera{ID: uuid.New(), ServerID: uuid.New(), Host: "10.0.0.5"}, "http", 80)
	if err != nil {
		t.Fatal(err)
	}
	srv = httptest.NewServer(svc)
	t.Cleanup(srv.Close)
	return srv, strings.TrimSuffix(o.URL, "/"), agent
}

func TestWebSocketIsTunnelledToTheCameraPortWithItsOwnHandshake(t *testing.T) {
	reqCh := make(chan *http.Request, 1)
	srv, prefix, agent := wsServer(t, func(_ http.Header, c net.Conn) {
		defer c.Close()
		br := bufio.NewReader(c)
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		reqCh <- req
		_, _ = io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: accepted\r\n\r\n")
		line, _ := br.ReadString('\n')
		_, _ = io.WriteString(c, "echo:"+line)
	})
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprintf(conn, "GET %s/__ws/8000/live/ch1?x=1 HTTP/1.1\r\nHost: openvms.example\r\nOrigin: null\r\nCookie: openvms=secret\r\nAuthorization: Bearer api-token\r\n"+
		"Connection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: abc==\r\n\r\n", prefix)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols || resp.Header.Get("Sec-WebSocket-Accept") != "accepted" {
		t.Fatalf("the camera's own handshake answer must reach the browser: %v %v", err, resp)
	}
	defer resp.Body.Close()
	fmt.Fprint(conn, "ping\n")
	if got, _ := br.ReadString('\n'); got != "echo:ping\n" {
		t.Fatalf("frames must relay both ways, got %q", got)
	}

	ctl := <-agent.opened
	if ctl.Get(camproxy.HeaderTarget) != "http://10.0.0.5:8000" {
		t.Fatalf("tunnel target = %q, want the camera host on the requested port", ctl.Get(camproxy.HeaderTarget))
	}
	cam := <-reqCh
	if cam.URL.RequestURI() != "/live/ch1?x=1" || cam.Host != "10.0.0.5:8000" || cam.Header.Get("Sec-WebSocket-Key") != "abc==" {
		t.Fatalf("camera saw %s Host=%s key=%q", cam.URL.RequestURI(), cam.Host, cam.Header.Get("Sec-WebSocket-Key"))
	}
	for _, h := range []string{"Cookie", "Authorization"} {
		if cam.Header.Get(h) != "" {
			t.Fatalf("%s of the OpenVMS user must never reach the camera", h)
		}
	}
	if cam.Header.Get("Origin") != "http://10.0.0.5" {
		t.Fatalf("Origin = %q, want the camera's own", cam.Header.Get("Origin"))
	}
}

func TestWebSocketRouteRefusesWhatItCannotCarry(t *testing.T) {
	srv, prefix, _ := wsServer(t, func(http.Header, net.Conn) {})
	for name, tc := range map[string]struct {
		path, upgrade string
		want          int
	}{
		"plain GET":    {"/__ws/8000/x", "", http.StatusBadRequest},
		"bad port":     {"/__ws/abc/x", "websocket", http.StatusBadRequest},
		"port zero":    {"/__ws/0/x", "websocket", http.StatusBadRequest},
		"port too big": {"/__ws/70000/x", "websocket", http.StatusBadRequest},
	} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+prefix+tc.path, nil)
		if tc.upgrade != "" {
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", tc.upgrade)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s: status %d, want %d", name, resp.StatusCode, tc.want)
		}
	}
}

func TestWebSocketWithoutTunnelSupportIsNotImplementedAndNeverReachesTheCamera(t *testing.T) {
	e := newEnv(t, Config{}) // fakeAgent has no Tunnel: the shape of an API wired with an old forwarder
	prefix := e.open()
	srv := httptest.NewServer(e.svc)
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+prefix+"/__ws/80/x", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

// The shim is plain JavaScript inside a Go string, so a typo only shows up in a browser. When node is
// available, parse the injected script (and the worker prelude it embeds) to catch that here.
func TestShimScriptParsesAsJavaScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	shim := shimScriptFor("/camproxy/test1234", "http", "admin", "p'w\"d")
	body := strings.TrimSuffix(strings.TrimPrefix(shim, "<script>"), "</script>")
	if body == shim {
		t.Fatal("shim is not a single script element")
	}
	file := filepath.Join(t.TempDir(), "shim.js")
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- node comes from LookPath and file is a temp path
	if out, err := exec.Command(node, "--check", file).CombinedOutput(); err != nil {
		t.Fatalf("shim does not parse: %v\n%s", err, out)
	}
	for _, want := range []string{`window.WebSocket=function(u,p)`, `window.Worker=function(u,o)`, `/__ws/`, `importScripts`} {
		if !strings.Contains(shim, want) {
			t.Fatalf("shim missing %q", want)
		}
	}
}

func TestShimDefinesTopWindowHelpers(t *testing.T) {
	shim := shimScriptFor("/camproxy/test1234", "http", "", "")
	for _, want := range []string{
		`function __camTopW(){`,                   // nearest same-origin ancestor, else the frame itself
		`p.document;w=p`,                          // climbing stops at the first ancestor it cannot read
		`(__camIsRoot()?window:window.__camSelf)`, // opaque frames still get the on-demand library fallback
		`function __camFrame0Doc(w){`,
		`function __camParentW(){`,
		`window.__camTopW=__camTopW;window.__camParentW=__camParentW;window.__camFrame0Doc=__camFrame0Doc;`, // rewritten camera scripts call them as globals
	} {
		if !strings.Contains(shim, want) {
			t.Fatalf("shim missing %q", want)
		}
	}
}

// Responses keep a real origin only on the dedicated camera listener, which asserts it with a header the
// main listener strips; anywhere else the sandbox stays opaque.
func TestCameraOriginMarkerDecidesSameOriginSandbox(t *testing.T) {
	e := newEnv(t, Config{})
	prefix := e.open()
	opaque := e.get(prefix, "/x").Header().Get("Content-Security-Policy")
	marked := e.get(prefix, "/x", "X-OpenVMS-Camweb", "1").Header().Get("Content-Security-Policy")
	spoofed := e.get(prefix, "/x", "X-OpenVMS-Camweb", "yes").Header().Get("Content-Security-Policy")
	if opaque != wantCSP || spoofed != wantCSP || strings.Contains(opaque, "allow-same-origin") {
		t.Fatalf("without the marker the sandbox must stay opaque: %q / %q", opaque, spoofed)
	}
	if marked != wantCSP+" allow-same-origin" {
		t.Fatalf("camera origin CSP = %q", marked)
	}
}

// parent and top are different frames in a camera UI (a page inside main.htm calls parent.setMainMenu, which
// main.htm defines, not the camera root): each must resolve to its own, readable frame.
func TestRewriteJSKeepsParentAndTopApart(t *testing.T) {
	for in, want := range map[string]string{
		`window.parent.setMainMenu(id);`: `__camParentW().setMainMenu(id);`,
		// a bare parent is a local variable in libraries (emscripten's FS.mknod does parent.node_ops.mknod)
		`FS.mknod=function(p){var parent=lookup(p);return parent.node_ops.mknod(parent,p)};`: `FS.mknod=function(p){var parent=lookup(p);return parent.node_ops.mknod(parent,p)};`,
		`window.top.showLoading();`:       `__camTopW().showLoading();`,
		`top.showLoading();`:              `__camTopW().showLoading();`,
		`x=window.parent.location.href;`:  `x=__camTopLoc.href;`,
		`a.parent.foo(); myparent.bar();`: `a.parent.foo(); myparent.bar();`,
	} {
		if got := string(rewriteJS([]byte(in))); got != want {
			t.Errorf("%s\n got  %s\n want %s", in, got, want)
		}
	}
}

// Embedded WebSocket servers often parse header names case-sensitively: the camera must see
// Sec-WebSocket-Key (as every browser spells it), not Go's canonical Sec-Websocket-Key, and no extension.
func TestWebSocketHandshakeUsesBrowserHeaderSpelling(t *testing.T) {
	raw := make(chan string, 1)
	srv, prefix, _ := wsServer(t, func(_ http.Header, c net.Conn) {
		defer c.Close()
		var got strings.Builder
		buf := make([]byte, 1)
		for !strings.HasSuffix(got.String(), "\r\n\r\n") {
			if _, err := c.Read(buf); err != nil {
				return
			}
			got.Write(buf)
		}
		raw <- got.String()
	})
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET %s/__ws/7681/ HTTP/1.1\r\nHost: x\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\n"+
		"Sec-WebSocket-Key: abc==\r\nSec-WebSocket-Extensions: permessage-deflate; client_max_window_bits\r\nSec-WebSocket-Protocol: chat\r\n\r\n", prefix)
	select {
	case h := <-raw:
		for _, want := range []string{"GET / HTTP/1.1\r\n", "Host: 10.0.0.5:7681\r\n", "Sec-WebSocket-Key: abc==\r\n", "Sec-WebSocket-Version: 13\r\n", "Sec-WebSocket-Protocol: chat\r\n", "Upgrade: websocket\r\n", "Connection: Upgrade\r\n", "Origin: http://10.0.0.5\r\n"} {
			if !strings.Contains(h, want) {
				t.Errorf("handshake missing %q in\n%s", want, h)
			}
		}
		if strings.Contains(h, "Sec-Websocket") || strings.Contains(strings.ToLower(h), "extensions") {
			t.Errorf("canonicalized header names or extensions leaked:\n%s", h)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("camera never received a handshake")
	}
}

// When a camera refuses a change the operator needs to know why: the log carries the camera's own
// error code (and the camera-side path), and the browser still gets the camera's answer unchanged.
func TestRejectedCameraWriteIsLoggedWithItsErrorCode(t *testing.T) {
	var logs strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	e := newEnv(t, Config{})
	e.cam.HandleFunc("/SetHttpPostConfig", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<config status="failed" errorCode ="PermissionDenied"><secret>do-not-log</secret></config>`)
	})
	prefix := e.open()
	rec := e.do(http.MethodPost, prefix, "/SetHttpPostConfig", strings.NewReader("<config/>"))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "PermissionDenied") {
		t.Fatalf("the camera's answer must reach the browser unchanged: %d %s", rec.Code, rec.Body.String())
	}
	got := logs.String()
	for _, want := range []string{"camera rejected a request", "camera_path=/SetHttpPostConfig", "status=403", "error_code=PermissionDenied"} {
		if !strings.Contains(got, want) {
			t.Errorf("log missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "do-not-log") || strings.Contains(got, prefix) {
		t.Errorf("the log must not carry the camera's body or the session id:\n%s", got)
	}
	if rec2 := e.get(prefix, "/nothing-here"); rec2.Code == http.StatusOK || strings.Count(logs.String(), "camera rejected a request") != 1 {
		t.Errorf("only refused writes are logged")
	}
}
