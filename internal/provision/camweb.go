package provision

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent/camproxy"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

// ErrCameraTunnelRefused means the agent did not open the requested camera tunnel (an agent that
// predates tunnels answers the upgrade as a plain request).
var ErrCameraTunnelRefused = errors.New("agent refused the camera tunnel")

// cameraWebTimeout bounds one proxied camera request end to end (the agent caps itself at 60s).
const cameraWebTimeout = 70 * time.Second

// CameraWebAgentReady reports whether the server has an agent the camera web view can go through:
// nil, ErrAgentUnprovisioned when there is none, or ErrAgentProbeFailed when its stored connection
// settings cannot be used. It does not contact the agent and does not check permissions.
func (s *Service) CameraWebAgentReady(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error {
	_, _, err := s.getAgentClientAndTokenWithTimeout(ctx, actor, serverID, cameraWebTimeout)
	return err
}

// CameraWebForward sends one request to the agent route /v1/camera-web/ of the server. header already
// carries the camproxy control headers built by the caller; the agent bearer token is added here and
// never leaves this function. The caller authorizes the actor and closes the response body.
func (s *Service) CameraWebForward(ctx context.Context, actor authz.Actor, serverID uuid.UUID, method string, header http.Header, body io.Reader) (*http.Response, error) {
	client, token, err := s.getAgentClientAndTokenWithTimeout(ctx, actor, serverID, cameraWebTimeout)
	if err != nil {
		return nil, err
	}
	out := header.Clone()
	if out == nil {
		out = http.Header{}
	}
	out.Set("Authorization", "Bearer "+token)
	return client.Do(ctx, method, camproxy.Path, out, body)
}

// cameraWebTunnelHandshake is how long the agent's connection is given to answer the upgrade.
const cameraWebTunnelHandshake = 15 * time.Second

// clientFor returns c unchanged, or a copy without the overall request timeout when the request
// upgrades the connection: that timeout also covers reading the body, which for a 101 response is the
// tunnel itself, and would cut a live stream after one request's worth of time.
func clientFor(c *http.Client, h http.Header) *http.Client {
	if h.Get("Upgrade") == "" {
		return c
	}
	cp := *c
	cp.Timeout = 0
	return &cp
}

// CameraWebTunnel opens a raw byte tunnel through the server's agent to the camera named in header
// (the camproxy control headers, built by the caller) and returns it once the agent has connected
// to the camera. The caller authorizes the actor, owns the returned connection and closes it.
func (s *Service) CameraWebTunnel(ctx context.Context, actor authz.Actor, serverID uuid.UUID, header http.Header) (io.ReadWriteCloser, error) {
	client, token, err := s.getAgentClientAndTokenWithTimeout(ctx, actor, serverID, cameraWebTimeout)
	if err != nil {
		return nil, err
	}
	out := header.Clone()
	if out == nil {
		out = http.Header{}
	}
	out.Set("Authorization", "Bearer "+token)
	out.Set("Connection", "Upgrade")
	out.Set("Upgrade", camproxy.TunnelProtocol)
	hs, cancel := context.WithTimeout(ctx, cameraWebTunnelHandshake)
	defer cancel()
	resp, err := client.Do(hs, http.MethodGet, camproxy.Path, out, nil)
	if err != nil {
		return nil, err
	}
	rwc, ok := resp.Body.(io.ReadWriteCloser)
	if resp.StatusCode != http.StatusSwitchingProtocols || resp.Header.Get(camproxy.HeaderProxy) != "1" || !ok {
		_ = resp.Body.Close()
		return nil, ErrCameraTunnelRefused
	}
	return rwc, nil
}
