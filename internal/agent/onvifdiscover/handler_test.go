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

func TestLoadConfigDefaultsAndOverrides(t *testing.T) {
	cfg, enabled, err := LoadConfig("", "")
	if err != nil || !enabled {
		t.Fatalf("expected LoadConfig empty to be enabled, got enabled=%v err=%v", enabled, err)
	}
	if len(cfg.Interfaces) != 1 || cfg.Interfaces[0] != "*" {
		t.Fatalf("expected wildcard interfaces, got %+v", cfg.Interfaces)
	}
	if len(cfg.AllowedCIDRs) != 1 || cfg.AllowedCIDRs[0] != "0.0.0.0/0" {
		t.Fatalf("expected 0.0.0.0/0, got %+v", cfg.AllowedCIDRs)
	}

	for _, val := range []string{"none", "disabled", "None", "DISABLED"} {
		_, enabled, err := LoadConfig(val, "")
		if err != nil || enabled {
			t.Fatalf("expected %q to be disabled, got enabled=%v err=%v", val, enabled, err)
		}
	}

	cfg, enabled, err = LoadConfig("eth0,ens18", "")
	if err != nil || !enabled || len(cfg.Interfaces) != 2 || cfg.AllowedCIDRs[0] != "0.0.0.0/0" {
		t.Fatalf("expected interface override with default CIDR, got %+v, enabled=%v, err=%v", cfg, enabled, err)
	}

	cfg, enabled, err = LoadConfig("eth0", "192.168.1.0/24")
	if err != nil || !enabled || cfg.Interfaces[0] != "eth0" || cfg.AllowedCIDRs[0] != "192.168.1.0/24" {
		t.Fatalf("expected explicit config, got %+v, enabled=%v, err=%v", cfg, enabled, err)
	}
}

func TestDiscoverHandlerWithWildcardInterfaces(t *testing.T) {
	f := &fakeDiscovery{devices: []onvif.DiscoveredDevice{{
		EndpointReference: "urn:uuid:device-1",
		XAddrs:            []onvif.Endpoint{endpoint(t, "http://10.1.1.50/onvif/device_service")},
	}}}
	h, err := NewHandler("token", f, Config{Interfaces: []string{"*"}, AllowedCIDRs: []string{"0.0.0.0/0"}})
	if err != nil {
		t.Fatal(err)
	}
	w := request(h, `{"interface_name":"ens18"}`, "token")
	if w.Code != 200 || !f.called || f.iface != "ens18" {
		t.Fatalf("expected ens18 to be allowed, code=%d called=%v iface=%s", w.Code, f.called, f.iface)
	}
	w = request(h, `{"interface_name":"../bad"}`, "token")
	if w.Code != 400 {
		t.Fatalf("expected 400 for malformed interface name, got %d", w.Code)
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
