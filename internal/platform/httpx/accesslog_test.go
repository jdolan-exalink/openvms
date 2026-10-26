package httpx

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A camera web session id travels in the URL path and is a working credential for its lifetime, so
// the access log must never record it.
func TestAccessLogRedactsTheCameraWebSessionIDFromThePath(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"/camproxy/abc123secret/js/app.js", "/camproxy/[redacted]/js/app.js"},
		{"/camproxy/abc123secret", "/camproxy/[redacted]"},
		{"/camproxy/abc123secret/", "/camproxy/[redacted]/"},
		{"/api/v1/servers", "/api/v1/servers"},
		{"/camproxyx/abc", "/camproxyx/abc"},
	} {
		var buf bytes.Buffer
		log := slog.New(slog.NewJSONHandler(&buf, nil))
		h := AccessLog(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tc.path, nil))
		if got := buf.String(); strings.Contains(got, "abc123secret") || !strings.Contains(got, `"path":"`+tc.want+`"`) {
			t.Errorf("path %q logged as %s, want path %q and no session id", tc.path, got, tc.want)
		}
	}
}
