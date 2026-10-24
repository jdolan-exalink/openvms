package provision

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

// agentAnswering fakes an agent: each path answers with the status given for the token it sees.
func agentAnswering(token string, byPath map[string]int) roundTripFunc {
	return func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			return agentReply(401, "unauthorized")(r)
		}
		status, ok := byPath[r.URL.Path]
		if !ok {
			return agentReply(404, "")(r)
		}
		body := ""
		if r.URL.Path == "/v1/metrics" && status == 200 {
			body = `{"version":"0.4.0","variant":"cpu"}`
		}
		return agentReply(status, body)(r)
	}
}

func connectService(rt roundTripFunc) *Service {
	return &Service{
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		probeRoundTripper:              rt,
	}
}

func TestVerifyAgentConnectionReportsVersionAndWhatTheTokenMayDo(t *testing.T) {
	for name, tc := range map[string]struct {
		paths  map[string]int
		scopes AgentScopes
	}{
		"full token":                    {map[string]int{"/v1/metrics": 200, "/v1/provision-lpr/events/after/0": 200, "/v1/provision-lpr/registry": 200}, AgentScopes{Read: true, Events: true, Control: true}},
		"read only":                     {map[string]int{"/v1/metrics": 200, "/v1/provision-lpr/events/after/0": 403, "/v1/provision-lpr/registry": 403}, AgentScopes{Read: true}},
		"events only plus read":         {map[string]int{"/v1/metrics": 200, "/v1/provision-lpr/events/after/0": 200, "/v1/provision-lpr/registry": 403}, AgentScopes{Read: true, Events: true}},
		"an agent older than the queue": {map[string]int{"/v1/metrics": 200}, AgentScopes{Read: true}},
	} {
		t.Run(name, func(t *testing.T) {
			svc := connectService(agentAnswering("tok", tc.paths))
			ep, _ := ParseAgentEndpoint("http://10.8.0.5:7419")
			client, err := svc.endpointClient(ep, AgentTLSConfig{}, onvifProbeTimeout)
			if err != nil {
				t.Fatal(err)
			}
			version, scopes, err := verifyAgentConnection(context.Background(), client, "tok")
			if err != nil || version != "0.4.0" || scopes != tc.scopes {
				t.Fatalf("version=%q scopes=%+v err=%v; want scopes %+v", version, scopes, err, tc.scopes)
			}
		})
	}
}

func TestVerifyAgentConnectionTellsApartTheWaysItCanFail(t *testing.T) {
	ep, _ := ParseAgentEndpoint("http://10.8.0.5:7419")
	check := func(rt roundTripFunc) error {
		client, err := connectService(rt).endpointClient(ep, AgentTLSConfig{}, onvifProbeTimeout)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = verifyAgentConnection(context.Background(), client, "tok")
		return err
	}
	if err := check(agentAnswering("other", map[string]int{"/v1/metrics": 200})); !errors.Is(err, ErrAgentConnectUnauthorized) {
		t.Errorf("wrong token = %v, want ErrAgentConnectUnauthorized", err)
	}
	if err := check(agentAnswering("tok", map[string]int{"/v1/metrics": 403})); !errors.Is(err, ErrAgentConnectForbidden) {
		t.Errorf("token without the read scope = %v, want ErrAgentConnectForbidden", err)
	}
	if err := check(agentAnswering("tok", map[string]int{})); !errors.Is(err, ErrAgentConnectNotAnAgent) {
		t.Errorf("no /v1/metrics = %v, want ErrAgentConnectNotAnAgent", err)
	}
	if err := check(roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("connection refused") })); !errors.Is(err, ErrAgentConnectUnreachable) {
		t.Errorf("unreachable = %v, want ErrAgentConnectUnreachable", err)
	}
	if err := check(roundTripFunc(func(r *http.Request) (*http.Response, error) { return agentReply(200, "<html>")(r) })); !errors.Is(err, ErrAgentConnectNotAnAgent) {
		t.Errorf("a page that is not the agent's JSON = %v, want ErrAgentConnectNotAnAgent", err)
	}
}

func TestParseConnectInputRefusesWhatCannotBeStoredSafely(t *testing.T) {
	good := ConnectAgentInput{URL: "https://agent.example.com", Token: "ovmsa_" + "0123456789ab" + "_" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	if _, err := parseConnectInput(good); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ConnectAgentInput){
		"no token":           func(i *ConnectAgentInput) { i.Token = "" },
		"token with space":   func(i *ConnectAgentInput) { i.Token = "a b" },
		"token with newline": func(i *ConnectAgentInput) { i.Token = "abc\ndef" },
		"huge token":         func(i *ConnectAgentInput) { i.Token = string(make([]byte, 400)) },
		"bad url":            func(i *ConnectAgentInput) { i.URL = "agent.example.com" },
		"url with a path":    func(i *ConnectAgentInput) { i.URL = "https://agent.example.com/x" },
		"reserved header":    func(i *ConnectAgentInput) { i.Headers = map[string]string{"Authorization": "x"} },
		"loopback":           func(i *ConnectAgentInput) { i.URL = "http://127.0.0.1:7419" },
		"metadata address":   func(i *ConnectAgentInput) { i.URL = "http://169.254.169.254" },
		"plain public ip":    func(i *ConnectAgentInput) { i.URL = "http://203.0.113.9:7419" },
		"ca without https":   func(i *ConnectAgentInput) { i.URL = "http://10.0.0.5:7419"; i.CAPEM = "-----BEGIN CERTIFICATE-----" },
	} {
		in := good
		mutate(&in)
		if _, err := parseConnectInput(in); err == nil {
			t.Errorf("%s must be refused", name)
		}
		var ve *ValidationError
		if _, err := parseConnectInput(in); !errors.As(err, &ve) {
			t.Errorf("%s: err = %v, want a ValidationError (a 400, not a 500)", name, err)
		}
	}
}
