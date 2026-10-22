package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

// onvifVideoEncodingTimeout covers a whole options or set round trip on the agent, which talks to
// the camera several times and gives up on its own after 25 s.
const onvifVideoEncodingTimeout = 40 * time.Second

// ExecuteOnvifVideoEncoding asks the server's agent to read, or change, the video encoding of a
// camera over ONVIF Media2. It needs the same rights as the other ONVIF operations: managing the
// server and reading its config secrets, because the camera credentials travel with the request.
//
// The agent answers a refused or impossible change (unsupported encoding, already that encoding,
// unknown configuration, change not applied) with a JSON body and a status; both are returned as
// they are so the caller can tell the user why. An error means the agent itself could not be
// reached or trusted.
func (s *Service) ExecuteOnvifVideoEncoding(ctx context.Context, actor authz.Actor, serverID uuid.UUID, req onvifdiscover.VideoEncodingRequest) (onvifdiscover.VideoEncodingResult, int, error) {
	if s.requireServerManage != nil {
		if err := s.requireServerManage(ctx, actor, serverID); err != nil {
			return onvifdiscover.VideoEncodingResult{}, 0, err
		}
		if err := s.requireProbeConfigSecrets(ctx, actor, serverID); err != nil {
			return onvifdiscover.VideoEncodingResult{}, 0, err
		}
	} else {
		if s.Inv == nil {
			return onvifdiscover.VideoEncodingResult{}, 0, ErrAgentProbeFailed
		}
		if err := s.Inv.RequireServerManageAndConfigSecrets(ctx, actor, serverID); err != nil {
			return onvifdiscover.VideoEncodingResult{}, 0, err
		}
		if req.Username == "" {
			if srvUser, srvPass, err := s.Inv.GetServerCredentials(ctx, actor, serverID); err == nil && srvUser != "" {
				req.Username, req.Password = srvUser, srvPass
			}
		}
	}

	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return onvifdiscover.VideoEncodingResult{}, 0, err
	}
	// #nosec G117 -- the camera password is sent on purpose, only to the server's own agent over the
	// verified channel, for this one call; it is never stored or logged.
	body, err := json.Marshal(req)
	if err != nil || len(body) > maxOnvifProbeRequestBytes || token == "" {
		return onvifdiscover.VideoEncodingResult{}, 0, ErrAgentProbeFailed
	}
	ctx, cancel := context.WithTimeout(ctx, onvifVideoEncodingTimeout)
	defer cancel()
	response, err := client.Do(ctx, http.MethodPost, "/v1/onvif/video-encoding", http.Header{
		"Authorization": []string{"Bearer " + token},
		"Content-Type":  []string{"application/json"},
	}, bytes.NewReader(body))
	if err != nil {
		return onvifdiscover.VideoEncodingResult{}, 0, ErrAgentProbeUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK, http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusBadGateway:
	default:
		return onvifdiscover.VideoEncodingResult{}, 0, ErrAgentProbeFailed
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxOnvifProbeResponseBytes+1))
	if err != nil || len(data) > maxOnvifProbeResponseBytes {
		return onvifdiscover.VideoEncodingResult{}, 0, ErrAgentProbeFailed
	}
	var res onvifdiscover.VideoEncodingResult
	if err := json.Unmarshal(data, &res); err != nil {
		return onvifdiscover.VideoEncodingResult{}, 0, ErrAgentProbeFailed
	}
	return res, response.StatusCode, nil
}
