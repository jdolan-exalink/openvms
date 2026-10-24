package provisionlpr

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPIsTheConnectionPeerAndNeverAHeaderTheClientChooses(t *testing.T) {
	for name, tc := range map[string]struct {
		remote, xff, want string
	}{
		"ipv4 with port":              {"10.0.15.107:54321", "", "10.0.15.107"},
		"forwarded-for is ignored":    {"10.0.15.200:54321", "10.0.15.107", "10.0.15.200"},
		"forwarded-for list ignored":  {"10.0.15.200:54321", "10.0.15.107, 1.2.3.4", "10.0.15.200"},
		"ipv6 with brackets":          {"[2001:db8::1]:54321", "", "2001:db8::1"},
		"ipv6 ignores forwarded-for":  {"[2001:db8::2]:54321", "2001:db8::1", "2001:db8::2"},
		"address without a port":      {"10.0.0.5", "", "10.0.0.5"},
		"ipv4-mapped ipv6 is unmaped": {"[::ffff:10.0.15.107]:5000", "", "10.0.15.107"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/SendKeepalive", nil)
		req.RemoteAddr = tc.remote
		if tc.xff != "" {
			req.Header.Set("X-Forwarded-For", tc.xff)
		}
		if got := extractClientIP(req); got != tc.want {
			t.Fatalf("%s: extractClientIP = %q, want %q", name, got, tc.want)
		}
	}
}
