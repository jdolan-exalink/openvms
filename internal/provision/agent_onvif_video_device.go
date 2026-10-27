package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
)

// ErrAgentOpUnsupported means the server's agent answered 404 or 405 for an operation: it is older
// than the OpenVMS that asked and has to be updated.
var ErrAgentOpUnsupported = errors.New("the agent does not support this operation; update the agent")

// onvifDeviceOpTimeout covers one video-device round trip; the agent gives up on its own at 30 s.
const onvifDeviceOpTimeout = 40 * time.Second

// authorizeOnvifOp requires what every ONVIF operation through an agent requires: managing the
// server and reading its config secrets, because the camera credentials travel with the request.
func (s *Service) authorizeOnvifOp(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error {
	if s.requireServerManage != nil {
		if err := s.requireServerManage(ctx, actor, serverID); err != nil {
			return err
		}
		return s.requireProbeConfigSecrets(ctx, actor, serverID)
	}
	if s.Inv == nil {
		return ErrAgentProbeFailed
	}
	return s.Inv.RequireServerManageAndConfigSecrets(ctx, actor, serverID)
}

// HasRegisteredAgent says whether the server has an agent OpenVMS can talk to (one that was
// installed or connected). It does not contact the agent.
func (s *Service) HasRegisteredAgent(ctx context.Context, actor authz.Actor, serverID uuid.UUID) bool {
	if s.Store == nil {
		return false
	}
	row, err := s.agentRow(ctx, actor, serverID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.logger().Warn("agent lookup failed", "server_id", serverID, "error", err)
		}
		return false
	}
	return row.ServerID != uuid.Nil
}

// postAgentOp sends one JSON operation to the server's agent and decodes the answer for the
// statuses the operation uses to report a domain outcome (accepted). A missing route reads as
// ErrAgentOpUnsupported; any other status is ErrAgentProbeFailed.
func (s *Service) postAgentOp(ctx context.Context, actor authz.Actor, serverID uuid.UUID, path string, req, out any, accepted ...int) (int, error) {
	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return 0, err
	}
	// #nosec G117 -- the camera password is sent on purpose, only to the server's own agent over the
	// verified channel, for this one call; it is never stored or logged.
	body, err := json.Marshal(req)
	if err != nil || len(body) > maxOnvifProbeRequestBytes || token == "" {
		return 0, ErrAgentProbeFailed
	}
	ctx, cancel := context.WithTimeout(ctx, onvifDeviceOpTimeout)
	defer cancel()
	response, err := client.Do(ctx, http.MethodPost, path, http.Header{
		"Authorization": []string{"Bearer " + token},
		"Content-Type":  []string{"application/json"},
	}, bytes.NewReader(body))
	if err != nil {
		return 0, ErrAgentProbeUnavailable
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxOnvifProbeResponseBytes+1))
	if err != nil || len(data) > maxOnvifProbeResponseBytes {
		return 0, ErrAgentProbeFailed
	}
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
		return response.StatusCode, ErrAgentOpUnsupported
	}
	ok := false
	for _, st := range accepted {
		ok = ok || st == response.StatusCode
	}
	if !ok {
		return response.StatusCode, ErrAgentProbeFailed
	}
	if err := json.Unmarshal(data, out); err != nil {
		return response.StatusCode, ErrAgentProbeFailed
	}
	return response.StatusCode, nil
}

// ExecuteOnvifVideoDevice runs a video operation (profile_streams, video_options,
// set_video_encoder) on a camera through the server's agent. The agent answers a refused or
// unapplied change with a JSON body and a status; both are returned as they are. An error means
// the agent could not be reached or trusted, or is too old (ErrAgentOpUnsupported).
func (s *Service) ExecuteOnvifVideoDevice(ctx context.Context, actor authz.Actor, serverID uuid.UUID, req onvifdiscover.VideoDeviceRequest) (onvifdiscover.VideoDeviceResult, int, error) {
	if err := s.authorizeOnvifOp(ctx, actor, serverID); err != nil {
		return onvifdiscover.VideoDeviceResult{}, 0, err
	}
	var res onvifdiscover.VideoDeviceResult
	status, err := s.postAgentOp(ctx, actor, serverID, "/v1/onvif/video-device", req, &res,
		http.StatusOK, http.StatusBadRequest, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusBadGateway)
	if err != nil {
		return onvifdiscover.VideoDeviceResult{}, 0, err
	}
	return res, status, nil
}

// ExecuteOnvifReboot asks the server's agent to restart a camera (ONVIF SystemReboot). It reports
// whether the camera accepted the command; the agent answers 502 when it did not.
func (s *Service) ExecuteOnvifReboot(ctx context.Context, actor authz.Actor, serverID uuid.UUID, req onvifdiscover.RebootRequest) error {
	if err := s.authorizeOnvifOp(ctx, actor, serverID); err != nil {
		return err
	}
	var out struct {
		Success bool `json:"success"`
	}
	status, err := s.postAgentOp(ctx, actor, serverID, "/v1/onvif/reboot", req, &out, http.StatusOK)
	if err != nil {
		return err
	}
	if status != http.StatusOK || !out.Success {
		return ErrAgentProbeFailed
	}
	return nil
}
