package provision

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

func TestProbeOnvifDeniesEitherPermissionBeforeAgentSecretsOrTransport(t *testing.T) {
	for _, denied := range []string{"manage", "config.secrets"} {
		t.Run(denied, func(t *testing.T) {
			var order []string
			svc := &Service{
				requireServerManage: func(context.Context, authz.Actor, uuid.UUID) error {
					order = append(order, "manage")
					if denied == "manage" {
						return access.ErrForbidden
					}
					return nil
				},
				requireServerConfigSecrets: func(context.Context, authz.Actor, uuid.UUID) error {
					order = append(order, "config.secrets")
					return access.ErrForbidden
				},
				loadProbeAgent: func(context.Context, authz.Actor, uuid.UUID) (string, int32, string, AgentTLSConfig, error) {
					order = append(order, "load-agent-secrets")
					return "", 0, "", AgentTLSConfig{}, errors.New("must not load secrets")
				},
				probeRoundTripper: roundTripFunc(func(*http.Request) (*http.Response, error) {
					order = append(order, "outbound")
					return nil, errors.New("must not call transport")
				}),
			}
			_, err := svc.ProbeOnvif(context.Background(), authz.Actor{}, uuid.New(), OnvifProbeRequest{
				Endpoint: "http://192.0.2.8/onvif/device_service", Username: "camera", Password: "secret",
			})
			if !errors.Is(err, access.ErrForbidden) {
				t.Fatalf("ProbeOnvif error = %v, want forbidden", err)
			}
			want := []string{"manage"}
			if denied == "config.secrets" {
				want = append(want, "config.secrets")
			}
			if strings.Join(order, ",") != strings.Join(want, ",") {
				t.Fatalf("operations before denial = %v, want %v", order, want)
			}
		})
	}
}

func TestProbeOnvifAgentUsesFixedVerifiedHTTPSAuthorityAndTransientCredentials(t *testing.T) {
	var calls int
	var captured *http.Request
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		captured = r.Clone(r.Context())
		body, _ := io.ReadAll(r.Body)
		captured.Body = io.NopCloser(strings.NewReader(string(body)))
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(validAgentProbeJSON))}, nil
	})
	got, err := probeAgent(context.Background(), transport, "192.0.2.10", 9443, nil, true, "agent-token",
		OnvifProbeRequest{Endpoint: "http://192.0.2.8/onvif/device_service", Username: "camera-user", Password: "camera-password"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || captured == nil {
		t.Fatalf("round trip calls = %d, request = %#v", calls, captured)
	}
	if captured.URL.Scheme != "https" || captured.URL.Host != "192.0.2.10:9443" || captured.URL.Path != "/v1/onvif/probe" || captured.URL.RawQuery != "" {
		t.Fatalf("unexpected destination: %s", captured.URL)
	}
	if captured.Header.Get("Authorization") != "Bearer agent-token" {
		t.Fatalf("agent bearer not sent: %v", captured.Header)
	}
	body, _ := io.ReadAll(captured.Body)
	if !strings.Contains(string(body), "camera-password") || strings.Contains(captured.URL.String(), "camera-password") {
		t.Fatalf("camera credentials were not confined to request body")
	}
	if got.Information.Manufacturer != "Acme" || len(got.Services) != 1 {
		t.Fatalf("unexpected result: %#v", got)
	}
}

func TestProbeOnvifAgentRejectsInvalidOrUnsafeAgentResponse(t *testing.T) {
	for _, body := range []string{
		`{"error":"private data"}`,
		`{"device_information":{"manufacturer":"Acme"},"services":[],"system_time":{},"unexpected":"secret"}`,
		strings.Repeat("x", 64<<10+1),
	} {
		t.Run("invalid agent response", func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			if _, err := probeAgent(context.Background(), transport, "192.0.2.10", 9443, nil, true, "token",
				OnvifProbeRequest{Endpoint: "http://192.0.2.8/onvif/device_service", Username: "camera-user", Password: "camera-password"}); err == nil {
				t.Fatal("expected invalid response rejection")
			}
		})
	}
	t.Run("credential-like response text is redacted", func(t *testing.T) {
		body := strings.Replace(validAgentProbeJSON, `"manufacturer":"Acme"`, `"manufacturer":"Acme camera-password"`, 1)
		transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		got, err := probeAgent(context.Background(), transport, "192.0.2.10", 9443, nil, true, "token",
			OnvifProbeRequest{Endpoint: "http://192.0.2.8/onvif/device_service", Username: "camera-user", Password: "camera-password"})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got.Information.Manufacturer, "camera-password") {
			t.Fatalf("credential leaked in result: %#v", got.Information)
		}
	})
}

func TestProbeOnvifAgentRejectsMissingRequiredResponseFields(t *testing.T) {
	responses := []struct {
		name string
		body string
	}{
		{name: "missing top-level information and time", body: `{"services":[]}`},
		{name: "missing device information member", body: strings.Replace(validAgentProbeJSON, `"manufacturer":"Acme",`, "", 1)},
		{name: "missing service version", body: strings.Replace(validAgentProbeJSON, `,"version":{"major":1,"minor":0}`, "", 1)},
		{name: "missing nested time member", body: strings.Replace(validAgentProbeJSON, `,"day":2`, "", 1)},
	}
	for _, tt := range responses {
		t.Run(tt.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})
			if _, err := probeAgent(context.Background(), transport, "192.0.2.10", 9443, nil, true, "token", OnvifProbeRequest{Endpoint: "http://192.0.2.8/device"}); err == nil {
				t.Fatal("accepted agent response missing a required field")
			}
		})
	}
}

func TestValidateOnvifProbeRequestRequiresBoundedIPv4EndpointAndCredentialPair(t *testing.T) {
	for _, input := range []OnvifProbeRequest{
		{Endpoint: "http://camera.local/device"},
		{Endpoint: "http://192.0.2.8/device?token=secret"},
		{Endpoint: "http://user:pass@192.0.2.8/device"},
		{Endpoint: "http://[2001:db8::1]/device"},
		{Endpoint: "http://192.0.2.8/device#fragment"},
		{Endpoint: "http://192.0.2.8:65536/device"},
		{Endpoint: "http://192.0.2.8/device", Username: "only-user"},
		{Endpoint: "http://192.0.2.8/device", Username: "u", Password: strings.Repeat("p", maxOnvifProbePasswordBytes+1)},
	} {
		if err := validateOnvifProbeRequest(input); err == nil {
			t.Fatalf("accepted unsafe request: %#v", input)
		}
	}
	if err := validateOnvifProbeRequest(OnvifProbeRequest{Endpoint: "https://192.0.2.8/onvif/device_service", Username: "u", Password: "p"}); err != nil {
		t.Fatalf("valid IPv4 HTTPS endpoint rejected: %v", err)
	}
}

const validAgentProbeJSON = `{"device_information":{"manufacturer":"Acme","model":"M1","firmware_version":"1.2","serial_number":"S1","hardware_id":"H1"},"services":[{"namespace":"urn:test","xaddrs":["http://192.0.2.8"],"version":{"major":1,"minor":0}}],"system_time":{"date_time_type":"NTP","utc":{"time":{"hour":1,"minute":2,"second":3},"date":{"year":2025,"month":1,"day":2}},"local":{"time":{"hour":1,"minute":2,"second":3},"date":{"year":2025,"month":1,"day":2}}}}`
