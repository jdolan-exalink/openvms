package provision

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent/camproxy"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

func TestCameraWebForwardGoesToTheAgentRouteWithTheTokenOnlyOnTheWire(t *testing.T) {
	svc := endpointService("10.0.15.2", 7419, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "", nil)
	var got *http.Request
	var body string
	svc.probeRoundTripper = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		return agentReply(200, "ok")(r)
	})
	in := http.Header{}
	in.Set(camproxy.HeaderTarget, "http://10.0.0.5:80")
	in.Set(camproxy.HeaderTargetURI, "/a?b=c")
	resp, err := svc.CameraWebForward(context.Background(), authz.Actor{}, uuid.New(), http.MethodPost, in, strings.NewReader("data"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got.Method != "POST" || got.URL.String() != "http://10.0.15.2:7419/v1/camera-web/" || body != "data" {
		t.Fatalf("request = %s %s %q", got.Method, got.URL, body)
	}
	if got.Header.Get("Authorization") != "Bearer agent-token" || got.Header.Get(camproxy.HeaderTargetURI) != "/a?b=c" {
		t.Fatalf("headers = %v", got.Header)
	}
	if in.Get("Authorization") != "" {
		t.Fatal("the caller's header map was modified")
	}
}

func TestCameraWebAgentReadyReportsAMissingAgent(t *testing.T) {
	svc := endpointService("", 0, AgentTLSConfig{}, "", nil)
	svc.loadProbeAgent = func(context.Context, authz.Actor, uuid.UUID) (string, int32, string, AgentTLSConfig, error) {
		return "", 0, "", AgentTLSConfig{}, ErrAgentUnprovisioned
	}
	if err := svc.CameraWebAgentReady(context.Background(), authz.Actor{}, uuid.New()); !errors.Is(err, ErrAgentUnprovisioned) {
		t.Fatalf("err = %v", err)
	}
	ok := endpointService("10.0.15.2", 7419, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "", nil)
	if err := ok.CameraWebAgentReady(context.Background(), authz.Actor{}, uuid.New()); err != nil {
		t.Fatalf("err = %v", err)
	}
}

type pipeBody struct {
	io.Reader
	io.Writer
	closed bool
}

func (p *pipeBody) Close() error { p.closed = true; return nil }

func TestCameraWebTunnelReturnsAWritableConnectionAndSendsUpgradeHeaders(t *testing.T) {
	svc := endpointService("10.0.15.2", 7419, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "", nil)
	var got *http.Request
	var written strings.Builder
	svc.probeRoundTripper = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r
		h := http.Header{}
		h.Set(camproxy.HeaderProxy, "1")
		return &http.Response{StatusCode: http.StatusSwitchingProtocols, Header: h, Request: r, Body: &pipeBody{Reader: strings.NewReader("from-camera"), Writer: &written}}, nil
	})
	in := http.Header{}
	in.Set(camproxy.HeaderTarget, "http://10.0.0.5:81")
	conn, err := svc.CameraWebTunnel(context.Background(), authz.Actor{}, uuid.New(), in)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("to-camera")); err != nil || written.String() != "to-camera" {
		t.Fatalf("tunnel not writable: %v %q", err, written.String())
	}
	if b, _ := io.ReadAll(conn); string(b) != "from-camera" {
		t.Fatalf("read %q", b)
	}
	if got.URL.Path != camproxy.Path || got.Header.Get("Upgrade") != camproxy.TunnelProtocol || got.Header.Get("Connection") != "Upgrade" || got.Header.Get("Authorization") != "Bearer agent-token" {
		t.Fatalf("request = %s %v", got.URL, got.Header)
	}
	if in.Get("Authorization") != "" || in.Get("Upgrade") != "" {
		t.Fatal("the caller's header map was modified")
	}
}

func TestCameraWebTunnelRefusedWhenTheAgentDoesNotSwitchProtocols(t *testing.T) {
	svc := endpointService("10.0.15.2", 7419, AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, "", nil)
	svc.probeRoundTripper = roundTripFunc(agentReply(405, "method not allowed")) // an agent that predates tunnels
	in := http.Header{}
	in.Set(camproxy.HeaderTarget, "http://10.0.0.5:81")
	if _, err := svc.CameraWebTunnel(context.Background(), authz.Actor{}, uuid.New(), in); !errors.Is(err, ErrCameraTunnelRefused) {
		t.Fatalf("err = %v", err)
	}
}
