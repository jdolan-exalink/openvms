package onvifdiscover

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/onvif"
)

func TestProbeHandlerAuthorizesBeforeCallingProbe(t *testing.T) {
	called := false
	handler, err := NewProbeHandler("agent-secret", Config{AllowedCIDRs: []string{"192.0.2.0/24"}}, func(context.Context, onvif.Endpoint, string, string) (ProbeResult, error) {
		called = true
		return ProbeResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, authorization := range []string{"", "Bearer wrong", "Basic agent-secret"} {
		r := httptest.NewRequest(http.MethodPost, "/v1/onvif/probe", strings.NewReader(`{"endpoint":"http://192.0.2.10/onvif"}`))
		r.Header.Set("Authorization", authorization)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("authorization %q status=%d", authorization, w.Code)
		}
	}
	if called {
		t.Fatal("probe ran before authorization")
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/onvif/probe", nil)
	r.Header.Set("Authorization", "Bearer agent-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed || called {
		t.Fatalf("GET status=%d, probe called=%v", w.Code, called)
	}
}

func TestProbeHandlerProjectsOnlyBoundedSanitizedResults(t *testing.T) {
	const user, pass = "operator", "camera-password"
	long := strings.Repeat("model-secret", 80)
	var gotEndpoint, gotUsername, gotPassword string
	calls := 0
	handler, err := NewProbeHandler("agent-secret", Config{AllowedCIDRs: []string{"192.0.2.0/24"}}, func(_ context.Context, endpoint onvif.Endpoint, username, password string) (ProbeResult, error) {
		calls++
		gotEndpoint, gotUsername, gotPassword = endpoint.String(), username, password
		return ProbeResult{
			Information: onvif.DeviceInformation{Manufacturer: long + pass, Model: "model-" + pass, SerialNumber: "serial-" + user, HardwareID: "hw-agent-secret"},
			Services:    []onvif.Service{{Namespace: "namespace-" + pass, Endpoint: "http://192.0.2.11:8080/sensitive-path"}, {Endpoint: "http://198.51.100.4:80/private"}, {Endpoint: "http://camera.example/device"}, {Endpoint: "http://user:pass@192.0.2.12/device"}},
			Clock:       onvif.DeviceClock{DateTimeType: "NTP", UTC: onvif.DateTimeValue{Time: onvif.CalendarTime{Hour: 12}}},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"endpoint": "https://192.0.2.10:8443/onvif/device_service", "username": user, "password": pass})
	r := httptest.NewRequest(http.MethodPost, "/v1/onvif/probe", strings.NewReader(string(body)))
	r.Header.Set("Authorization", "Bearer agent-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if calls != 1 || gotEndpoint != "https://192.0.2.10:8443/onvif/device_service" || gotUsername != user || gotPassword != pass {
		t.Fatalf("unexpected probe call: calls=%d endpoint=%q user=%q", calls, gotEndpoint, gotUsername)
	}
	response := w.Body.String()
	for _, secret := range []string{user, pass, "agent-secret", "/sensitive-path", "/private", "camera.example", "user:pass"} {
		if strings.Contains(response, secret) {
			t.Errorf("response leaked %q: %s", secret, response)
		}
	}
	if len([]rune(response)) > 16_384 {
		t.Fatalf("response exceeded bound: %d bytes", len(response))
	}
	if strings.Contains(response, long) {
		t.Fatal("unbounded vendor field was returned")
	}
	if !strings.Contains(response, "192.0.2.11:8080") || strings.Contains(response, "198.51.100.4") {
		t.Fatalf("policy-invalid XAddr projection: %s", response)
	}
}

func TestProbeHandlerRejectsInvalidRequestsBeforeProbe(t *testing.T) {
	called := false
	handler, err := NewProbeHandler("agent-secret", Config{AllowedCIDRs: []string{"192.0.2.0/24"}}, func(context.Context, onvif.Endpoint, string, string) (ProbeResult, error) {
		called = true
		return ProbeResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, body string
		status     int
	}{
		{name: "invalid JSON", body: `{`, status: http.StatusBadRequest},
		{name: "unknown fields", body: `{"endpoint":"http://192.0.2.10/x","credential":"x"}`, status: http.StatusBadRequest},
		{name: "trailing JSON", body: `{"endpoint":"http://192.0.2.10/x"} {}`, status: http.StatusBadRequest},
		{name: "oversized body", body: `{"endpoint":"http://192.0.2.10/` + strings.Repeat("x", maxProbeRequestBytes) + `"}`, status: http.StatusBadRequest},
		{name: "DNS hostname", body: `{"endpoint":"http://camera.example/device","username":"u","password":"p"}`, status: http.StatusBadRequest},
		{name: "IPv6 literal", body: `{"endpoint":"http://[2001:db8::1]/device","username":"u","password":"p"}`, status: http.StatusBadRequest},
		{name: "outside allowed CIDR", body: `{"endpoint":"http://198.51.100.4/device","username":"u","password":"p"}`, status: http.StatusForbidden},
		{name: "endpoint userinfo", body: `{"endpoint":"http://user:pass@192.0.2.10/device","username":"u","password":"p"}`, status: http.StatusBadRequest},
		{name: "endpoint query", body: `{"endpoint":"http://192.0.2.10/device?token=secret","username":"u","password":"p"}`, status: http.StatusBadRequest},
		{name: "endpoint fragment", body: `{"endpoint":"http://192.0.2.10/device#secret","username":"u","password":"p"}`, status: http.StatusBadRequest},
		{name: "non-HTTP scheme", body: `{"endpoint":"ftp://192.0.2.10/device","username":"u","password":"p"}`, status: http.StatusBadRequest},
		{name: "partial credentials", body: `{"endpoint":"http://192.0.2.10/device","username":"u"}`, status: http.StatusBadRequest},
		{name: "oversized username", body: `{"endpoint":"http://192.0.2.10/device","username":"` + strings.Repeat("u", maxProbeUsernameBytes+1) + `","password":"p"}`, status: http.StatusBadRequest},
		{name: "oversized password", body: `{"endpoint":"http://192.0.2.10/device","username":"u","password":"` + strings.Repeat("p", maxProbePasswordBytes+1) + `"}`, status: http.StatusBadRequest},
		{name: "oversized endpoint", body: `{"endpoint":"http://192.0.2.10/` + strings.Repeat("x", maxProbeEndpointBytes) + `"}`, status: http.StatusBadRequest},
		{name: "empty endpoint", body: `{"endpoint":"","username":"u","password":"p"}`, status: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/onvif/probe", strings.NewReader(tt.body))
			r.Header.Set("Authorization", "Bearer agent-secret")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("status=%d body=%s, want %d", w.Code, w.Body.String(), tt.status)
			}
		})
	}
	if called {
		t.Fatal("probe ran for an invalid endpoint or request")
	}
}

func TestProbeHandlerRedactsFailuresAndCredentials(t *testing.T) {
	const user, pass = "u-sensitive", "p-sensitive"
	handler, err := NewProbeHandler("agent-secret", Config{AllowedCIDRs: []string{"192.0.2.0/24"}}, func(context.Context, onvif.Endpoint, string, string) (ProbeResult, error) {
		return ProbeResult{}, errors.New("fault " + user + " " + pass + " agent-secret")
	})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/onvif/probe", strings.NewReader(`{"endpoint":"http://192.0.2.10/device","username":"`+user+`","password":"`+pass+`"}`))
	r.Header.Set("Authorization", "Bearer agent-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status=%d", w.Code)
	}
	for _, secret := range []string{user, pass, "agent-secret", "deadline"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Errorf("failure leaked %q", secret)
		}
	}
}

func TestProbeHandlerPropagatesRequestCancellation(t *testing.T) {
	started := make(chan struct{})
	handler, err := NewProbeHandler("agent-secret", Config{AllowedCIDRs: []string{"192.0.2.0/24"}}, func(ctx context.Context, _ onvif.Endpoint, _, _ string) (ProbeResult, error) {
		close(started)
		<-ctx.Done()
		return ProbeResult{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest(http.MethodPost, "/v1/onvif/probe", strings.NewReader(`{"endpoint":"http://192.0.2.10/device"}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer agent-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	<-started
	if w.Code != http.StatusRequestTimeout {
		t.Fatalf("status=%d, want request timeout", w.Code)
	}
}

func TestDeviceProbeDoesNotFollowCameraRedirects(t *testing.T) {
	calls := 0
	prober := NewDeviceProbe(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"http://198.51.100.9/steal"}}, Body: io.NopCloser(strings.NewReader("redirect")), Request: r}, nil
	}))
	handler, err := NewProbeHandler("agent-secret", Config{AllowedCIDRs: []string{"192.0.2.0/24"}}, prober)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/onvif/probe", strings.NewReader(`{"endpoint":"http://192.0.2.10/device","username":"u","password":"p"}`))
	r.Header.Set("Authorization", "Bearer agent-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadGateway || calls != 1 {
		t.Fatalf("status=%d round trips=%d, want 502 and one request", w.Code, calls)
	}
	if strings.Contains(w.Body.String(), "steal") || strings.Contains(w.Body.String(), "redirect") {
		t.Fatalf("redirect details leaked: %s", w.Body.String())
	}
}

func TestProbeHandlerBoundsOverallProbeAndReadOnlyCalls(t *testing.T) {
	started := time.Now()
	var actions []string
	var targetScheme, targetHost string
	var overallDeadline time.Time
	prober := NewDeviceProbe(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if deadline, ok := r.Context().Deadline(); ok && time.Until(deadline) <= probeOperationTimeout {
			// The ONVIF client adds the per-operation bound to the handler's outer budget.
		} else {
			t.Errorf("missing per-operation timeout deadline")
		}
		targetScheme, targetHost = r.URL.Scheme, r.URL.Host
		actions = append(actions, r.Header.Get("Content-Type"))
		soap, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(soap), "camera-password") {
			t.Error("SOAP payload contained the plaintext password")
		}
		return soapResponse(t, r), nil
	}))
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			actions = nil
			handler, err := NewProbeHandler("agent-secret", Config{AllowedCIDRs: []string{"192.0.2.0/24"}}, func(ctx context.Context, endpoint onvif.Endpoint, username, password string) (ProbeResult, error) {
				var ok bool
				overallDeadline, ok = ctx.Deadline()
				if !ok || time.Until(overallDeadline) > probeOverallTimeout {
					t.Error("overall probe deadline was not bounded")
				}
				return prober(ctx, endpoint, username, password)
			})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPost, "/v1/onvif/probe", strings.NewReader(`{"endpoint":"`+scheme+`://192.0.2.10/device","username":"u","password":"camera-password"}`))
			r.Header.Set("Authorization", "Bearer agent-secret")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if len(actions) != 3 || targetScheme != scheme || targetHost != "192.0.2.10" {
				t.Fatalf("SOAP requests=%d target=%s://%s", len(actions), targetScheme, targetHost)
			}
			for _, action := range []string{"GetDeviceInformation", "GetServices", "GetSystemDateAndTime"} {
				found := false
				for _, got := range actions {
					if strings.Contains(got, action) {
						found = true
					}
				}
				if !found {
					t.Errorf("missing read-only ONVIF action %s in %v", action, actions)
				}
			}
		})
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("fake probe exceeded bound: %s", time.Since(started))
	}
}

func soapResponse(t *testing.T, request *http.Request) *http.Response {
	t.Helper()
	action := request.Header.Get("Content-Type")
	var operation string
	switch {
	case strings.Contains(action, "GetDeviceInformation"):
		operation = `<tds:GetDeviceInformationResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Manufacturer>Acme</tds:Manufacturer><tds:Model>MockCam</tds:Model></tds:GetDeviceInformationResponse>`
	case strings.Contains(action, "GetServices"):
		operation = `<tds:GetServicesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Service><tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace><tds:XAddr>http://192.0.2.99:8080/onvif/media</tds:XAddr></tds:Service><tds:Service><tds:Namespace>http://www.onvif.org/ver10/events/wsdl</tds:Namespace><tds:XAddr>http://198.51.100.5:80/onvif/events</tds:XAddr></tds:Service></tds:GetServicesResponse>`
	case strings.Contains(action, "GetSystemDateAndTime"):
		operation = `<tds:GetSystemDateAndTimeResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:SystemDateAndTime><tt:DateTimeType xmlns:tt="http://www.onvif.org/ver10/schema">NTP</tt:DateTimeType></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`
	default:
		t.Fatalf("unexpected SOAP action %q", action)
	}
	body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>` + operation + `</s:Body></s:Envelope>`
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
