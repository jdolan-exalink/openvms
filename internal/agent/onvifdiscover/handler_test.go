package onvifdiscover

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/onvif"
)

type fakeDiscovery struct {
	called  bool
	iface   string
	devices []onvif.DiscoveredDevice
	err     error
}

func (f *fakeDiscovery) Probe(_ context.Context, iface string) ([]onvif.DiscoveredDevice, error) {
	f.called = true
	f.iface = iface
	return f.devices, f.err
}

func request(h http.Handler, body string, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/v1/onvif/discover", strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestDiscoverHandlerSecurityAndPolicy(t *testing.T) {
	f := &fakeDiscovery{devices: []onvif.DiscoveredDevice{{
		EndpointReference: "urn:uuid:device-1",
		XAddrs:            []onvif.Endpoint{endpoint(t, "http://192.168.1.20/onvif/device_service"), endpoint(t, "http://192.168.2.20/device"), endpoint(t, "http://camera.local/device")},
		Types:             []string{"tds:Device"}, ScopeHints: []string{"onvif://www.onvif.org/name/camera"},
	}}}
	h, err := NewHandler("secret-token", f, Config{Interfaces: []string{"eth0"}, AllowedCIDRs: []string{"192.168.1.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, method, body, token string
		code                      int
		called                    bool
	}{
		{"unauthorized", "POST", `{"interface_name":"eth0"}`, "", 401, false},
		{"wrong method", "GET", ``, "secret-token", 405, false},
		{"unknown field credentials", "POST", `{"interface_name":"eth0","username":"u","password":"p"}`, "secret-token", 400, false},
		{"malformed json", "POST", `{`, "secret-token", 400, false},
		{"not allowed interface", "POST", `{"interface_name":"wlan0"}`, "secret-token", 403, false},
		{"bounded body", "POST", `{"interface_name":"` + strings.Repeat("a", 5000) + `"}`, "secret-token", 400, false},
		{"success", "POST", `{"interface_name":"eth0"}`, "secret-token", 200, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f.called = false
			r := httptest.NewRequest(tt.method, "/v1/onvif/discover", strings.NewReader(tt.body))
			if tt.token != "" {
				r.Header.Set("Authorization", "Bearer "+tt.token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.code {
				t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
			}
			if f.called != tt.called {
				t.Fatalf("discovery called=%v want %v", f.called, tt.called)
			}
			if tt.name == "success" {
				if f.iface != "eth0" {
					t.Fatalf("interface = %q", f.iface)
				}
				body := w.Body.String()
				if strings.Contains(body, "192.168.2.20") || strings.Contains(body, "camera.local") || !strings.Contains(body, "192.168.1.20") {
					t.Fatalf("address policy response: %s", body)
				}
			}
		})
	}
}

func TestDiscoverHandlerRejectsInvalidConfig(t *testing.T) {
	for _, cfg := range []Config{
		{Interfaces: []string{"eth0"}},
		{Interfaces: []string{"eth0"}, AllowedCIDRs: []string{"not-a-cidr"}},
		{Interfaces: []string{"eth0", "eth0"}, AllowedCIDRs: []string{"192.168.1.0/24"}},
	} {
		if _, err := NewHandler("token", &fakeDiscovery{}, cfg); err == nil {
			t.Fatalf("accepted invalid config: %+v", cfg)
		}
	}
}

func TestDiscoverHandlerRedactsDiscoveryErrors(t *testing.T) {
	f := &fakeDiscovery{err: errors.New("failed with password=secret-token")}
	h, err := NewHandler("secret-token", f, Config{Interfaces: []string{"eth0"}, AllowedCIDRs: []string{"192.168.1.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	w := request(h, `{"interface_name":"eth0"}`, "secret-token")
	if w.Code != 502 || strings.Contains(w.Body.String(), "secret-token") || !strings.Contains(w.Body.String(), "discovery_failed") {
		t.Fatalf("unsafe error response: %d %s", w.Code, w.Body.String())
	}
}

func endpoint(t *testing.T, s string) onvif.Endpoint {
	t.Helper()
	e, err := onvif.ParseEndpoint(s)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
