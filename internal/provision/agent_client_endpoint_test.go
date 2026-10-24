package provision

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

func endpointService(host string, port int32, trust AgentTLSConfig, base string, baseErr error) *Service {
	return &Service{
		loadProbeAgent: func(context.Context, authz.Actor, uuid.UUID) (string, int32, string, AgentTLSConfig, error) {
			return host, port, "agent-token", trust, nil
		},
		loadAgentConnection: func(context.Context, authz.Actor, uuid.UUID) (string, map[string]string, error) {
			return base, nil, baseErr
		},
	}
}

func TestAgentClientUsesTheStoredBaseURLWhenThereIsOne(t *testing.T) {
	// A published name over HTTPS: verified by that name with the system roots, on the URL's port.
	svc := endpointService("agent.example.com", 443, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "https://agent.example.com", nil)
	doer, token, err := svc.getAgentClientAndToken(context.Background(), authz.Actor{}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	verified, ok := doer.(*VerifiedAgentHTTPClient)
	if !ok || verified.baseURL != "https://agent.example.com:443" || token != "agent-token" {
		t.Fatalf("client = %#v token %q", doer, token)
	}
	cfg := verified.client.Transport.(*http.Transport).TLSClientConfig
	if cfg.ServerName != "agent.example.com" || cfg.MinVersion != tls.VersionTLS12 || cfg.InsecureSkipVerify {
		t.Fatalf("TLS config = %+v", cfg)
	}

	// A VPN address over plain HTTP.
	svc = endpointService("ignored", 7419, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "http://10.8.0.5:7419", nil)
	doer, _, err = svc.getAgentClientAndToken(context.Background(), authz.Actor{}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if plain, ok := doer.(*PlainAgentHTTPClient); !ok || plain.baseURL != "http://10.8.0.5:7419" {
		t.Fatalf("client = %#v", doer)
	}
}

func TestAgentClientWithoutABaseURLKeepsTheLegacyHostAndPort(t *testing.T) {
	svc := endpointService("10.0.15.2", 7419, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "", nil)
	doer, _, err := svc.getAgentClientAndToken(context.Background(), authz.Actor{}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if plain, ok := doer.(*PlainAgentHTTPClient); !ok || plain.baseURL != "http://10.0.15.2:7419" {
		t.Fatalf("client = %#v", doer)
	}
}

func TestAgentClientRefusesABadStoredBaseURLInsteadOfFallingBack(t *testing.T) {
	for name, svc := range map[string]*Service{
		"not an authority": endpointService("10.0.15.2", 7419, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "ftp://agent.example.com", nil),
		"has a path":       endpointService("10.0.15.2", 7419, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "https://agent.example.com/v1", nil),
		"cannot be read":   endpointService("10.0.15.2", 7419, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "", errors.New("db down")),
	} {
		if _, _, err := svc.getAgentClientAndToken(context.Background(), authz.Actor{}, uuid.New()); !errors.Is(err, ErrAgentProbeFailed) {
			t.Errorf("%s: err = %v, want ErrAgentProbeFailed (never a silent fall back to host:port)", name, err)
		}
	}
}

const metricsBody = `{"version":"0.4.0","variant":"cpu"}`
const healthBody = `{"status":"ok","binary_identity":{"version":"0.4.0","sha256":"` + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" + `","architecture":"amd64"}}`

// The status of an agent reached through a stored base URL goes through that URL, not through
// host:port, and the agent token travels as a bearer.
func TestAgentStatusOverTheEndpointReadsMetricsAndIdentityFromTheBaseURL(t *testing.T) {
	var seen []string
	svc := endpointService("agent.example.com", 443, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "http://10.8.0.5:7419", nil)
	svc.probeRoundTripper = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.Method+" "+r.URL.String()+" "+r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/v1/metrics":
			return agentReply(200, metricsBody)(r)
		case "/v1/health":
			return agentReply(200, healthBody)(r)
		}
		return agentReply(404, "")(r)
	})
	snap, err := svc.fetchEndpointMetrics(context.Background(), authz.Actor{}, uuid.New())
	if err != nil || snap.Version != "0.4.0" || snap.Variant != "cpu" {
		t.Fatalf("snapshot = %+v, %v", snap, err)
	}
	identity, err := svc.fetchEndpointIdentity(context.Background(), authz.Actor{}, uuid.New())
	if err != nil || identity.Version != "0.4.0" {
		t.Fatalf("identity = %+v, %v", identity, err)
	}
	want := []string{"GET http://10.8.0.5:7419/v1/metrics Bearer agent-token", "GET http://10.8.0.5:7419/v1/health Bearer agent-token"}
	if len(seen) != 2 || seen[0] != want[0] || seen[1] != want[1] {
		t.Fatalf("requests = %q, want %q", seen, want)
	}
}

func TestAgentStatusOverTheEndpointReportsAnUnhealthyAgent(t *testing.T) {
	svc := endpointService("agent.example.com", 443, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "http://10.8.0.5:7419", nil)
	svc.probeRoundTripper = roundTripFunc(func(r *http.Request) (*http.Response, error) { return agentReply(401, "unauthorized")(r) })
	if _, err := svc.fetchEndpointMetrics(context.Background(), authz.Actor{}, uuid.New()); err == nil {
		t.Fatal("a 401 from the agent must be an error")
	}
	if _, err := svc.fetchEndpointIdentity(context.Background(), authz.Actor{}, uuid.New()); err == nil {
		t.Fatal("a 401 from the agent must be an error")
	}
}

// The guard is wired into the production transport: an agent address that points at this host never
// receives a connection, whatever the operator typed.
func TestAgentClientNeverConnectsToThisHostThroughAStoredBaseURL(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer server.Close()
	svc := endpointService("ignored", 1, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, server.URL, nil) // http://127.0.0.1:port
	doer, _, err := svc.getAgentClientAndToken(context.Background(), authz.Actor{}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	resp, err := doer.Do(context.Background(), http.MethodGet, "/v1/metrics", http.Header{}, nil)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("a request to a loopback agent address must fail")
	}
	if hits != 0 {
		t.Fatalf("the loopback server received %d requests", hits)
	}
}
