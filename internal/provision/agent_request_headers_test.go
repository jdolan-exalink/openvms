package provision

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/secrets"
)

func TestValidateAgentRequestHeaders(t *testing.T) {
	good := map[string]string{"CF-Access-Client-Id": "abc.access", "CF-Access-Client-Secret": "s3cr3t"}
	if err := validateAgentRequestHeaders(good); err != nil {
		t.Fatal(err)
	}
	if err := validateAgentRequestHeaders(nil); err != nil {
		t.Fatalf("no headers is valid: %v", err)
	}
	tooMany := map[string]string{}
	for i := 0; i < 9; i++ {
		tooMany["X-H"+string(rune('A'+i))] = "v"
	}
	for name, h := range map[string]map[string]string{
		"authorization":   {"Authorization": "Bearer x"},
		"host":            {"host": "evil.example"},
		"content length":  {"Content-Length": "1"},
		"transfer":        {"Transfer-Encoding": "chunked"},
		"connection":      {"Connection": "close"},
		"bad name":        {"X Bad": "v"},
		"empty name":      {"": "v"},
		"newline value":   {"X-A": "a\r\nX-B: b"},
		"huge value":      {"X-A": strings.Repeat("a", 2000)},
		"too many":        tooMany,
		"control in name": {"X-\x00": "v"},
	} {
		if err := validateAgentRequestHeaders(h); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}
}

func TestSealedAgentHeadersRoundTripBoundToTheServer(t *testing.T) {
	sealer, err := secrets.NewSealer([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	serverID, other := uuid.New(), uuid.New()
	headers := map[string]string{"CF-Access-Client-Id": "id", "CF-Access-Client-Secret": "secret"}
	sealed, err := sealAgentRequestHeaders(sealer, serverID, headers)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "secret") {
		t.Fatal("headers are stored in the clear")
	}
	got, err := openAgentRequestHeaders(sealer, serverID, sealed)
	if err != nil || got["CF-Access-Client-Secret"] != "secret" || len(got) != 2 {
		t.Fatalf("round trip = %v, %v", got, err)
	}
	if _, err := openAgentRequestHeaders(sealer, other, sealed); err == nil {
		t.Fatal("headers sealed for one server opened for another")
	}
	if got, err := openAgentRequestHeaders(sealer, serverID, nil); err != nil || len(got) != 0 {
		t.Fatalf("an agent without headers = %v, %v", got, err)
	}
}

func TestHeaderDoerAddsTheGatewayHeadersButNeverReplacesTheCallers(t *testing.T) {
	var seen http.Header
	inner := agentDoerFunc(func(_ context.Context, _, _ string, h http.Header, _ io.Reader) (*http.Response, error) {
		seen = h.Clone()
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})
	d := withAgentRequestHeaders(inner, map[string]string{"CF-Access-Client-Id": "id", "X-Keep": "gateway"})
	resp, err := d.Do(context.Background(), http.MethodGet, "/v1/metrics", http.Header{"Authorization": {"Bearer agent"}, "X-Keep": {"caller"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if seen.Get("CF-Access-Client-Id") != "id" || seen.Get("Authorization") != "Bearer agent" || seen.Get("X-Keep") != "caller" {
		t.Fatalf("headers = %v", seen)
	}
	// Without headers there is no wrapper at all.
	if withAgentRequestHeaders(inner, nil) == nil {
		t.Fatal("nil wrapper")
	}
}

func TestAgentClientAppliesTheStoredGatewayHeaders(t *testing.T) {
	svc := endpointService("agent.example.com", 443, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "http://10.8.0.5:7419", nil)
	svc.loadAgentConnection = func(context.Context, authz.Actor, uuid.UUID) (string, map[string]string, error) {
		return "http://10.8.0.5:7419", map[string]string{"CF-Access-Client-Id": "id"}, nil
	}
	var seen http.Header
	svc.probeRoundTripper = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = r.Header.Clone()
		return agentReply(200, "{}")(r)
	})
	doer, token, err := svc.getAgentClientAndToken(context.Background(), authz.Actor{}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	resp, err := doer.Do(context.Background(), http.MethodGet, "/v1/metrics", http.Header{"Authorization": {"Bearer " + token}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if seen.Get("CF-Access-Client-Id") != "id" || seen.Get("Authorization") != "Bearer agent-token" {
		t.Fatalf("request headers = %v", seen)
	}
}

type agentDoerFunc func(ctx context.Context, method, path string, headers http.Header, body io.Reader) (*http.Response, error)

func (f agentDoerFunc) Do(ctx context.Context, method, path string, headers http.Header, body io.Reader) (*http.Response, error) {
	return f(ctx, method, path, headers, body)
}
