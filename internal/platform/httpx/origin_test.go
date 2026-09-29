package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
)

func TestOriginAllowed(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		host   string
		extra  []string
		want   bool
	}{
		{"no origin header is not a browser", "", "vms.example", nil, true},
		{"same origin", "https://vms.example", "vms.example", nil, true},
		{"same origin ignores case", "https://VMS.example", "vms.example", nil, true},
		{"cross origin refused", "https://evil.example", "vms.example", nil, false},
		{"explicitly allowed origin", "https://app.example", "vms.example", []string{"https://app.example"}, true},
		{"malformed origin refused", "://bad", "vms.example", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/ws", nil)
			r.Host = tc.host
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := httpx.OriginAllowed(r, tc.extra); got != tc.want {
				t.Fatalf("OriginAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}
