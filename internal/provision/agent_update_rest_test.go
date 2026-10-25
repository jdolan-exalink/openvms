package provision

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

func TestPushUpdateViaSendsTheBinaryWithItsChecksumAndExpectedVersion(t *testing.T) {
	var method, path string
	var headers http.Header
	var body []byte
	doer := agentDoerFunc(func(_ context.Context, m, p string, h http.Header, b io.Reader) (*http.Response, error) {
		method, path, headers = m, p, h.Clone()
		body, _ = io.ReadAll(b)
		return &http.Response{StatusCode: 202, Body: http.NoBody}, nil
	})
	binary := []byte("\x7fELF-binary")
	if err := pushUpdateVia(context.Background(), doer, "agent-token", binary, "0.4.1"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/v1/update" || !bytes.Equal(body, binary) {
		t.Fatalf("request = %s %s (%d bytes)", method, path, len(body))
	}
	if headers.Get("Authorization") != "Bearer agent-token" || headers.Get("Content-Type") != "application/octet-stream" ||
		headers.Get("X-OpenVMS-Agent-Version") != "0.4.1" || len(headers.Get("X-OpenVMS-SHA256")) != 64 {
		t.Fatalf("headers = %v", headers)
	}
}

func TestPushUpdateViaMapsTheAgentsRefusals(t *testing.T) {
	reply := func(status int, body string) AgentProbeDoer {
		return agentDoerFunc(func(context.Context, string, string, http.Header, io.Reader) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
	}
	if err := pushUpdateVia(context.Background(), reply(409, "an earlier agent update is still waiting"), "t", []byte("x"), ""); !errors.Is(err, ErrAgentUpdatePending) {
		t.Fatalf("409 = %v, want ErrAgentUpdatePending", err)
	}
	err := pushUpdateVia(context.Background(), reply(400, "the new binary does not run on this host"), "t", []byte("x"), "")
	var statusErr *AgentStatusError
	if !errors.As(err, &statusErr) || statusErr.Status != 400 || !strings.Contains(statusErr.Message, "does not run") {
		t.Fatalf("400 = %v, want the agent's reason", err)
	}
	if err := pushUpdateVia(context.Background(), reply(404, ""), "t", []byte("x"), ""); err == nil {
		t.Fatal("404 must be an error")
	}
	failing := agentDoerFunc(func(context.Context, string, string, http.Header, io.Reader) (*http.Response, error) {
		return nil, errors.New("connection reset")
	})
	if err := pushUpdateVia(context.Background(), failing, "t", []byte("x"), ""); !errors.Is(err, ErrAgentProbeUnavailable) {
		t.Fatalf("transport failure = %v", err)
	}
}

// A binary of tens of megabytes cannot go through the short timeout the probes use.
func TestUpdateClientHasALongTimeout(t *testing.T) {
	svc := endpointService("10.8.0.5", 7419, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "http://10.8.0.5:7419", nil)
	doer, _, err := svc.getAgentClientAndTokenWithTimeout(context.Background(), authz.Actor{}, uuid.New(), 3*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if plain, ok := doer.(*PlainAgentHTTPClient); !ok || plain.client.Timeout != 3*time.Minute {
		t.Fatalf("client = %#v", doer)
	}
}
