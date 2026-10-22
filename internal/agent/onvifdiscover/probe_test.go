package onvifdiscover

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
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
			// #nosec G101 -- literal URL credentials are rejection fixtures, not secrets.
			Services: []onvif.Service{{Namespace: "namespace-" + pass, Endpoint: "http://192.0.2.11:8080/sensitive-path"}, {Endpoint: "http://198.51.100.4:80/private"}, {Endpoint: "http://camera.example/device"}, {Endpoint: "http://user:pass@192.0.2.12/device"}},
			Clock:    onvif.DeviceClock{DateTimeType: "NTP", UTC: onvif.DateTimeValue{Time: onvif.CalendarTime{Hour: 12}}},
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
		// #nosec G101 -- literal URL credentials are a rejection fixture.
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
		if deadline, ok := r.Context().Deadline(); !ok || time.Until(deadline) > probeOperationTimeout {
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
			if len(actions) != 6 || targetScheme == "" || targetHost == "" {
				t.Fatalf("SOAP requests=%d target=%s://%s", len(actions), targetScheme, targetHost)
			}
			for _, action := range []string{"GetDeviceInformation", "GetServices", "GetSystemDateAndTime", "GetProfiles", "GetStreamUri", "GetEventProperties"} {
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
	case strings.Contains(action, "GetProfiles"):
		operation = `<trt:GetProfilesResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl"><trt:Profiles token="p1"><tt:Name xmlns:tt="http://www.onvif.org/ver10/schema">Main</tt:Name></trt:Profiles></trt:GetProfilesResponse>`
	case strings.Contains(action, "GetStreamUri"):
		operation = `<trt:GetStreamUriResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl"><trt:MediaUri><tt:Uri xmlns:tt="http://www.onvif.org/ver10/schema">rtsp://192.0.2.10/main</tt:Uri></trt:MediaUri></trt:GetStreamUriResponse>`
	case strings.Contains(action, "GetEventProperties"):
		operation = `<tev:GetEventPropertiesResponse xmlns:tev="http://www.onvif.org/ver10/events/wsdl"/>`
	default:
		t.Fatalf("unexpected SOAP action %q", action)
	}
	body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>` + operation + `</s:Body></s:Envelope>`
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func TestDeviceProbePrefersMedia2AndFallsBackWhenUnsupported(t *testing.T) {
	tests := []struct {
		name            string
		media2Supported bool
	}{
		{name: "Media2 profile", media2Supported: true},
		{name: "Media1 fallback after unsupported Media2", media2Supported: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actions []string
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				request := string(body)
				endpoint := r.URL.Path
				op := "other"
				for _, candidate := range []string{"GetDeviceInformation", "GetServices", "GetSystemDateAndTime", "GetProfiles", "GetStreamUri"} {
					if strings.Contains(request, candidate) {
						op = candidate
						break
					}
				}
				actions = append(actions, endpoint+":"+op)
				var operation string
				switch {
				case strings.Contains(request, "GetDeviceInformation"):
					operation = `<tds:GetDeviceInformationResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Manufacturer>Acme</tds:Manufacturer></tds:GetDeviceInformationResponse>`
				case strings.Contains(request, "GetServices"):
					operation = `<tds:GetServicesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Service><tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace><tds:XAddr>http://192.0.2.10:8080/onvif/media1</tds:XAddr></tds:Service><tds:Service><tds:Namespace>http://www.onvif.org/ver20/media/wsdl</tds:Namespace><tds:XAddr>http://192.0.2.10:8082/onvif/media2</tds:XAddr></tds:Service></tds:GetServicesResponse>`
				case strings.Contains(request, "GetSystemDateAndTime"):
					operation = `<tds:GetSystemDateAndTimeResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:SystemDateAndTime><tt:DateTimeType xmlns:tt="http://www.onvif.org/ver10/schema">NTP</tt:DateTimeType></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`
				case strings.Contains(request, "GetProfiles") && endpoint == "/onvif/media2":
					if !tt.media2Supported {
						fault := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault><s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>ter:ActionNotSupported</s:Value></s:Subcode></s:Code></s:Fault></s:Body></s:Envelope>`
						return &http.Response{StatusCode: http.StatusInternalServerError, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fault)), Request: r}, nil
					}
					operation = `<tr2:GetProfilesResponse xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><tr2:Profiles token="main-profile" fixed="true"><tt:Name xmlns:tt="http://www.onvif.org/ver10/schema">Main</tt:Name><tt:VideoSourceConfiguration xmlns:tt="http://www.onvif.org/ver10/schema" token="source-main"/><tt:VideoEncoderConfiguration xmlns:tt="http://www.onvif.org/ver10/schema" token="encoder-main"><tt:Encoding>H265</tt:Encoding><tt:Resolution><tt:Width>2560</tt:Width><tt:Height>1440</tt:Height></tt:Resolution><tt:RateControl><tt:FrameRateLimit>25</tt:FrameRateLimit><tt:BitrateLimit>6144</tt:BitrateLimit></tt:RateControl></tt:VideoEncoderConfiguration><tt:PTZConfiguration xmlns:tt="http://www.onvif.org/ver10/schema" token="ptz-main"/></tr2:Profiles></tr2:GetProfilesResponse>`
				case strings.Contains(request, "GetProfiles") && endpoint == "/onvif/media1":
					operation = `<trt:GetProfilesResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl"><trt:Profiles token="legacy-profile" fixed="true"><tt:Name xmlns:tt="http://www.onvif.org/ver10/schema">Legacy</tt:Name><tt:VideoSourceConfiguration xmlns:tt="http://www.onvif.org/ver10/schema" token="legacy-source"/><tt:VideoEncoderConfiguration xmlns:tt="http://www.onvif.org/ver10/schema" token="legacy-encoder"><tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution><tt:RateControl><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl></tt:VideoEncoderConfiguration><tt:PTZConfiguration xmlns:tt="http://www.onvif.org/ver10/schema" token="legacy-ptz"/></trt:Profiles></trt:GetProfilesResponse>`
				case strings.Contains(request, "GetStreamUri"):
					if strings.Contains(request, "tr2:GetStreamUri") {
						streamURI := "rtsp://" + url.UserPassword("admin", "camera-secret").String() + "@192.0.2.10/main?x=camera-secret"
						operation = `<tr2:GetStreamUriResponse xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><tr2:Uri>` + streamURI + `</tr2:Uri></tr2:GetStreamUriResponse>`
					} else {
						operation = `<trt:GetStreamUriResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl"><trt:MediaUri><tt:Uri xmlns:tt="http://www.onvif.org/ver10/schema">rtsp://192.0.2.10/legacy</tt:Uri></trt:MediaUri></trt:GetStreamUriResponse>`
					}
				default:
					t.Fatalf("unexpected SOAP request %s at %s", request, endpoint)
				}
				response := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>` + operation + `</s:Body></s:Envelope>`
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response)), Request: r}, nil
			})

			prober := NewDeviceProbe(transport)
			endpoint, err := onvif.ParseEndpoint("http://192.0.2.10/onvif/device_service")
			if err != nil {
				t.Fatal(err)
			}
			result, err := prober(context.Background(), endpoint, "admin", "camera-secret")
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Profiles) != 1 {
				t.Fatalf("profiles = %+v", result.Profiles)
			}
			profile := result.Profiles[0]
			wantToken, wantVersion, wantEncoding := "main-profile", "2.0", "H265"
			if !tt.media2Supported {
				wantToken, wantVersion, wantEncoding = "legacy-profile", "1.0", "H264"
			}
			if profile.Token != wantToken || profile.MediaVersion != wantVersion || profile.Encoding != wantEncoding || !profile.Fixed {
				t.Fatalf("profile = %+v", profile)
			}
			wantWidth, wantEncoder, wantSource, wantPTZ := 2560, "encoder-main", "source-main", "ptz-main"
			if !tt.media2Supported {
				wantWidth, wantEncoder, wantSource, wantPTZ = 1920, "legacy-encoder", "legacy-source", "legacy-ptz"
			}
			if profile.Width != wantWidth || profile.EncoderConfigurationToken != wantEncoder || profile.SourceConfigurationToken != wantSource || profile.PTZConfigurationToken != wantPTZ {
				t.Fatalf("profile capability metadata = %+v", profile)
			}
			if !strings.Contains(actions[0], "GetDeviceInformation") {
				t.Fatalf("first action = %q", actions[0])
			}
			media2Index, media1Index := -1, -1
			for i, action := range actions {
				if action == "/onvif/media2:GetProfiles" {
					media2Index = i
				}
				if action == "/onvif/media1:GetProfiles" {
					media1Index = i
				}
			}
			if media2Index < 0 || (!tt.media2Supported && media1Index <= media2Index) || (tt.media2Supported && media1Index >= 0) {
				t.Fatalf("profile request order = %+v", actions)
			}
			projected := projectProbeResult(result, []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, "agent-secret", "admin", "camera-secret")
			jsonResult, err := json.Marshal(projected)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"admin", "camera-secret", "?x="} {
				if strings.Contains(string(jsonResult), secret) {
					t.Errorf("projected profile leaked %q: %s", secret, jsonResult)
				}
			}
		})
	}
}

func TestSanitizeStreamURIRedactsPercentEncodedCredentials(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		{name: "encoded username", uri: "rtsp://192.0.2.10/%61dmin/main"},
		{name: "encoded password", uri: "rtsp://192.0.2.10/camera%2Dsecret/main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeStreamURI(tt.uri, "admin", "camera-secret"); got != "" {
				t.Fatalf("sanitizeStreamURI(%q) = %q, want URI omitted", tt.uri, got)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestDeviceProbeFallsBackToGetCapabilitiesWhenGetServicesFails(t *testing.T) {
	capabilitiesXML := `<tds:GetCapabilitiesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">` +
		`<tds:Capabilities>` +
		`<tt:Media><tt:XAddr>http://192.0.2.10:8080/onvif/media</tt:XAddr></tt:Media>` +
		`</tds:Capabilities>` +
		`</tds:GetCapabilitiesResponse>`

	var calledActions []string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		action := r.Header.Get("Content-Type")
		calledActions = append(calledActions, action)
		switch {
		case strings.Contains(action, "GetSystemDateAndTime"):
			body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tds:GetSystemDateAndTimeResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:SystemDateAndTime><tt:DateTimeType xmlns:tt="http://www.onvif.org/ver10/schema">NTP</tt:DateTimeType></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse></s:Body></s:Envelope>`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		case strings.Contains(action, "GetDeviceInformation"):
			body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tds:GetDeviceInformationResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Manufacturer>LegacyCam</tds:Manufacturer><tds:Model>Model1</tds:Model></tds:GetDeviceInformationResponse></s:Body></s:Envelope>`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		case strings.Contains(action, "GetServices"):
			// Return SOAP Fault ActionNotSupported as older cameras do
			fault := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault><s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>ter:ActionNotSupported</s:Value></s:Subcode></s:Code></s:Fault></s:Body></s:Envelope>`
			return &http.Response{StatusCode: http.StatusInternalServerError, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fault)), Request: r}, nil
		case strings.Contains(action, "GetCapabilities"):
			body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>` + capabilitiesXML + `</s:Body></s:Envelope>`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		case strings.Contains(action, "GetProfiles"):
			body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><trt:GetProfilesResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/></s:Body></s:Envelope>`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		default:
			t.Fatalf("unexpected action: %s", action)
			return nil, nil
		}
	})

	prober := NewDeviceProbe(transport)
	ep, err := onvif.ParseEndpoint("http://192.0.2.10/onvif/device_service")
	if err != nil {
		t.Fatal(err)
	}

	result, err := prober(context.Background(), ep, "admin", "secret")
	if err != nil {
		t.Fatalf("prober failed on legacy camera: %v", err)
	}

	if result.Information.Manufacturer != "LegacyCam" {
		t.Errorf("manufacturer = %q, want LegacyCam", result.Information.Manufacturer)
	}
	if len(result.Services) != 1 || result.Services[0].Namespace != "http://www.onvif.org/ver10/media/wsdl" {
		t.Errorf("services = %+v, want 1 media service from capabilities fallback", result.Services)
	}
}

func TestDeviceProbeCompensatesClockSkewOnAuthFailure(t *testing.T) {
	skewedYear := 2030
	var attempts int
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		action := r.Header.Get("Content-Type")
		attempts++
		switch {
		case strings.Contains(action, "GetDeviceInformation"):
			body, _ := io.ReadAll(r.Body)
			// Check if Created header contains 2030 (compensated timestamp)
			if strings.Contains(string(body), "2030-") {
				respBody := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tds:GetDeviceInformationResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Manufacturer>SyncedCam</tds:Manufacturer><tds:Model>ModelSkew</tds:Model></tds:GetDeviceInformationResponse></s:Body></s:Envelope>`
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(respBody)), Request: r}, nil
			}
			// First attempt fails with 401 Unauthorized (clock skew)
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("Unauthorized")), Request: r}, nil
		case strings.Contains(action, "GetSystemDateAndTime"):
			respBody := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tds:GetSystemDateAndTimeResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:SystemDateAndTime><tt:DateTimeType xmlns:tt="http://www.onvif.org/ver10/schema">Manual</tt:DateTimeType><tt:UTCDateTime xmlns:tt="http://www.onvif.org/ver10/schema"><tt:Time><tt:Hour>10</tt:Hour><tt:Minute>0</tt:Minute><tt:Second>0</tt:Second></tt:Time><tt:Date><tt:Year>2030</tt:Year><tt:Month>5</tt:Month><tt:Day>1</tt:Day></tt:Date></tt:UTCDateTime></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse></s:Body></s:Envelope>`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(respBody)), Request: r}, nil
		case strings.Contains(action, "GetServices"):
			respBody := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tds:GetServicesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Service><tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace><tds:XAddr>http://192.0.2.10:8080/onvif/media</tds:XAddr></tds:Service></tds:GetServicesResponse></s:Body></s:Envelope>`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(respBody)), Request: r}, nil
		case strings.Contains(action, "GetProfiles"):
			body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><trt:GetProfilesResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/></s:Body></s:Envelope>`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		default:
			t.Fatalf("unexpected action: %s", action)
			return nil, nil
		}
	})

	prober := NewDeviceProbe(transport)
	ep, err := onvif.ParseEndpoint("http://192.0.2.10/onvif/device_service")
	if err != nil {
		t.Fatal(err)
	}

	result, err := prober(context.Background(), ep, "admin", "secret")
	if err != nil {
		t.Fatalf("prober failed on clock-skewed camera: %v", err)
	}

	if result.Information.Manufacturer != "SyncedCam" {
		t.Errorf("manufacturer = %q, want SyncedCam", result.Information.Manufacturer)
	}
	if result.Clock.UTC.Date.Year != skewedYear {
		t.Errorf("camera year = %d, want %d", result.Clock.UTC.Date.Year, skewedYear)
	}
}
