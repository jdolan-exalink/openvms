package provision

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent/camproxy"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

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
