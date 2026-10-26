package camproxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func newTestHandler(t *testing.T, cfg Config) *Handler {
	t.Helper()
	if cfg.SelfAddrs == nil {
		cfg.SelfAddrs = func() []netip.Addr { return nil } // the fake camera lives on this host
	}
	h := New(cfg)
	// The fake camera is an httptest server on loopback, which production refuses.
	h.allowAddr = func(netip.Addr) bool { return true }
	return h
}

func do(h http.Handler, method, target, uri string, hdr map[string]string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, Path, body)
	req.Header.Set(HeaderTarget, target)
	if uri != "" {
		req.Header.Set(HeaderTargetURI, uri)
	}
	req.Header.Set("Authorization", "Bearer agent-secret")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTargetValidation(t *testing.T) {
	cfg := Config{BlockedPorts: []int{7419, 7443}, SelfAddrs: func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("192.168.1.10")} }}
	h := New(cfg)
	bad := map[string]string{ // #nosec G101 -- a test fixture
		"loopback":       "http://127.0.0.1:80",
		"metadata":       "http://169.254.169.254:80",
		"cgnat":          "http://100.64.1.1:80",
		"hostname":       "http://camera.local:80",
		"ipv6":           "http://[::1]:80",
		"mapped ipv6":    "http://[::ffff:10.0.0.1]:80",
		"own address":    "http://192.168.1.10:80",
		"own port":       "http://10.0.0.5:7419",
		"own tls port":   "http://10.0.0.5:7443",
		"ssh":            "http://10.0.0.5:22",
		"port zero":      "http://10.0.0.5:0",
		"port too big":   "http://10.0.0.5:70000",
		"ftp":            "ftp://10.0.0.5:21",
		"file":           "file:///etc/passwd",
		"userinfo":       "http://u:p@10.0.0.5:80",
		"path in target": "http://10.0.0.5:80/x",
		"public":         "http://8.8.8.8:80",
		"multicast":      "http://224.0.0.1:80",
		"unspecified":    "http://0.0.0.0:80",
		"octal":          "http://010.0.0.1:80",
		"empty":          "",
		"172.32 outside": "http://172.32.0.1:80",
	}
	for name, target := range bad {
		rec := do(h, http.MethodGet, target, "/", nil, nil)
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %q got %d, want 400/403", name, target, rec.Code)
		}
		if rec.Header().Get(HeaderProxyError) == "" {
			t.Errorf("%s: agent-originated error not marked", name)
		}
	}
	for _, ok := range []string{"http://10.1.2.3:80", "https://172.16.0.9:443", "http://192.168.0.5:8080"} {
		if _, err := ParseTarget(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
}

func TestMethodsAndURI(t *testing.T) {
	h := newTestHandler(t, Config{})
	for _, m := range []string{http.MethodTrace, http.MethodConnect, "PROPFIND", http.MethodPatch} {
		if rec := do(h, m, "http://10.0.0.5:80", "/", nil, nil); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", m, rec.Code)
		}
	}
	for _, uri := range []string{"http://evil/x", "//evil/x", "x", "/a\r\nX: y", "/" + strings.Repeat("a", 9000)} {
		if rec := do(h, http.MethodGet, "http://10.0.0.5:80", uri, nil, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("uri %.20q = %d, want 400", uri, rec.Code)
		}
	}
}

func TestForwardsAndMapsHeaders(t *testing.T) {
	var got http.Header
	var gotPath, gotQuery, gotMethod, gotBody string
	cam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, gotPath, gotQuery, gotMethod = r.Header.Clone(), r.URL.Path, r.URL.RawQuery, r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Add("Set-Cookie", "a=1; Path=/")
		w.Header().Add("Set-Cookie", "b=2")
		w.Header().Set("X-OpenVMS-Proxy-Error", "spoof")
		w.Header().Set("X-Camera", "yes")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hello"))
	}))
	defer cam.Close()
	h := newTestHandler(t, Config{})
	rec := do(h, http.MethodPost, "http://"+cam.Listener.Addr().String(), "/cgi/set%20x?a=b&c=d", map[string]string{
		HeaderForwardAuthorization: "Basic YTpi",
		HeaderForwardCookie:        "sid=9",
		"Connection":               "X-Hop",
		"X-Hop":                    "secret",
		"Cookie":                   "openvms_session=zzz",
		"Content-Type":             "text/plain",
		"X-Forwarded-For":          "1.2.3.4",
	}, strings.NewReader("payload"))
	if rec.Code != http.StatusTeapot || rec.Body.String() != "hello" {
		t.Fatalf("status/body = %d %q", rec.Code, rec.Body.String())
	}
	if gotMethod != "POST" || gotPath != "/cgi/set x" || gotQuery != "a=b&c=d" || gotBody != "payload" {
		t.Fatalf("upstream saw %s %q ?%q %q", gotMethod, gotPath, gotQuery, gotBody)
	}
	if got.Get("Authorization") != "Basic YTpi" {
		t.Errorf("forwarded Authorization = %q (the agent token must never reach the camera)", got.Get("Authorization"))
	}
	if got.Get("Cookie") != "sid=9" || got.Get("X-Hop") != "" || got.Get("X-Forwarded-For") != "" {
		t.Errorf("upstream headers leaked: %v", got)
	}
	for k := range got {
		if strings.HasPrefix(strings.ToLower(k), "x-openvms-") {
			t.Errorf("control header %s reached the camera", k)
		}
	}
	if sc := rec.Header().Values(HeaderSetCookie); len(sc) != 2 {
		t.Errorf("X-OpenVMS-Set-Cookie = %v", sc)
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Errorf("raw Set-Cookie present")
	}
	if rec.Header().Get(HeaderProxyError) != "" || rec.Header().Get(HeaderProxy) != "1" || rec.Header().Get("X-Camera") != "yes" {
		t.Errorf("response headers = %v", rec.Header())
	}
}

func TestNoAgentTokenWhenNoForwardAuthorization(t *testing.T) {
	var auth string
	cam := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { auth = r.Header.Get("Authorization") }))
	defer cam.Close()
	h := newTestHandler(t, Config{})
	do(h, http.MethodGet, "http://"+cam.Listener.Addr().String(), "/", nil, nil)
	if auth != "" {
		t.Fatalf("camera received Authorization %q", auth)
	}
}

func TestRedirectsAreReturnedNotFollowed(t *testing.T) {
	hits := 0
	cam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer cam.Close()
	rec := do(newTestHandler(t, Config{}), http.MethodGet, "http://"+cam.Listener.Addr().String(), "/", nil, nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/elsewhere" || hits != 1 {
		t.Fatalf("%d %q hits=%d", rec.Code, rec.Header().Get("Location"), hits)
	}
}

func TestRequestBodyLimit(t *testing.T) {
	cam := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body) }))
	defer cam.Close()
	h := newTestHandler(t, Config{MaxRequestBody: 10})
	rec := do(h, http.MethodPost, "http://"+cam.Listener.Addr().String(), "/", nil, strings.NewReader(strings.Repeat("x", 11)))
	if rec.Code != http.StatusRequestEntityTooLarge || rec.Header().Get(HeaderProxyError) == "" {
		t.Fatalf("declared oversized body = %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, Path, io.MultiReader(strings.NewReader(strings.Repeat("x", 11))))
	req.ContentLength = -1
	req.Header.Set(HeaderTarget, "http://"+cam.Listener.Addr().String())
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("undeclared oversized body accepted")
	}
}

func TestResponseBodyLimit(t *testing.T) {
	cam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/declared" {
			w.Header().Set("Content-Length", "100")
		}
		_, _ = w.Write([]byte(strings.Repeat("y", 100)))
		if f, ok := w.(http.Flusher); ok {
			f.Flush() // forces chunked framing: no Content-Length to check up front
		}
	}))
	defer cam.Close()
	h := newTestHandler(t, Config{MaxResponseBody: 10})
	rec := do(h, http.MethodGet, "http://"+cam.Listener.Addr().String(), "/declared", nil, nil)
	if rec.Code != http.StatusBadGateway || rec.Header().Get(HeaderProxyError) == "" {
		t.Fatalf("declared oversized response = %d", rec.Code)
	}
	func() {
		defer func() {
			if v := recover(); !errors.Is(asError(v), http.ErrAbortHandler) {
				t.Fatalf("undeclared oversized response must abort the stream, recovered %v", v)
			}
		}()
		do(h, http.MethodGet, "http://"+cam.Listener.Addr().String(), "/chunked", nil, nil)
	}()
}

func TestDialControlRechecksResolvedAddress(t *testing.T) {
	h := New(Config{BlockedPorts: []int{7419}, SelfAddrs: func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("192.168.1.10")} }})
	for addr, wantErr := range map[string]bool{
		"10.0.0.5:80":        false,
		"127.0.0.1:80":       true,
		"169.254.169.254:80": true,
		"100.64.0.1:80":      true,
		"192.168.1.10:80":    true,
		"10.0.0.5:7419":      true,
		"10.0.0.5:22":        true,
		"[::1]:80":           true,
		"garbage":            true,
	} {
		err := h.dialControl("tcp4", addr, nil)
		if (err != nil) != wantErr {
			t.Errorf("dialControl(%s) err=%v wantErr=%v", addr, err, wantErr)
		}
	}
}

func TestUnreachableTargetTimesOut(t *testing.T) {
	h := newTestHandler(t, Config{ConnectTimeout: 100 * time.Millisecond})
	rec := do(h, http.MethodGet, "http://127.0.0.1:1", "/", nil, nil)
	if rec.Code != http.StatusBadGateway || rec.Header().Get(HeaderProxyError) == "" {
		t.Fatalf("closed port = %d", rec.Code)
	}
}

func TestHostInterfaceAddressesAreRefusedByDefault(t *testing.T) {
	h := New(Config{}) // reads the real interfaces
	if err := h.dialControl("tcp4", "127.0.0.1:80", nil); err == nil {
		t.Fatal("loopback accepted")
	}
	h.allowAddr = func(netip.Addr) bool { return true } // as in the other tests: only the self check remains
	if err := h.dialControl("tcp4", "127.0.0.1:80", nil); err == nil {
		t.Fatal("the host's own loopback address was accepted")
	}
}

func TestEmptyBodyIsNotSentChunked(t *testing.T) {
	var te []string
	var cl int64
	cam := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { te, cl = r.TransferEncoding, r.ContentLength }))
	defer cam.Close()
	do(newTestHandler(t, Config{}), http.MethodPost, "http://"+cam.Listener.Addr().String(), "/", nil, strings.NewReader(""))
	if len(te) != 0 || cl != 0 {
		t.Fatalf("empty POST arrived with TE %v, length %d", te, cl)
	}
}

func asError(v any) error {
	err, _ := v.(error)
	return err
}
