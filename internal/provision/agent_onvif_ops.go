package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

func (s *Service) ExecuteOnvifSyncTime(ctx context.Context, actor authz.Actor, serverID uuid.UUID, req onvifdiscover.SyncTimeRequest) (map[string]any, error) {
	if s.Inv != nil {
		if err := s.Inv.RequireServerManageAndConfigSecrets(ctx, actor, serverID); err != nil {
			return nil, err
		}
		if req.Username == "" {
			if srvUser, srvPass, err := s.Inv.GetServerCredentials(ctx, actor, serverID); err == nil && srvUser != "" {
				req.Username = srvUser
				req.Password = srvPass
			}
		}
	}
	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := postAgentJSON(ctx, client, token, "/v1/onvif/sync-time", req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) ExecuteOnvifPTZ(ctx context.Context, actor authz.Actor, serverID uuid.UUID, req onvifdiscover.PTZRequest) (map[string]any, error) {
	if s.Inv != nil {
		if err := s.Inv.RequireServerManageAndConfigSecrets(ctx, actor, serverID); err != nil {
			return nil, err
		}
		if req.Username == "" {
			if srvUser, srvPass, err := s.Inv.GetServerCredentials(ctx, actor, serverID); err == nil && srvUser != "" {
				req.Username = srvUser
				req.Password = srvPass
			}
		}
	}
	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := postAgentJSON(ctx, client, token, "/v1/onvif/ptz", req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) ExecuteOnvifVideoConfig(ctx context.Context, actor authz.Actor, serverID uuid.UUID, req onvifdiscover.VideoConfigRequest) (map[string]any, error) {
	if s.Inv != nil {
		if err := s.Inv.RequireServerManageAndConfigSecrets(ctx, actor, serverID); err != nil {
			return nil, err
		}
		if req.Username == "" {
			if srvUser, srvPass, err := s.Inv.GetServerCredentials(ctx, actor, serverID); err == nil && srvUser != "" {
				req.Username = srvUser
				req.Password = srvPass
			}
		}
	}
	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := postAgentJSON(ctx, client, token, "/v1/onvif/video-config", req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) ExecuteOnvifImaging(ctx context.Context, actor authz.Actor, serverID uuid.UUID, req onvifdiscover.ImagingRequest) (map[string]any, error) {
	if s.Inv != nil {
		if err := s.Inv.RequireServerManageAndConfigSecrets(ctx, actor, serverID); err != nil {
			return nil, err
		}
		if req.Username == "" {
			if srvUser, srvPass, err := s.Inv.GetServerCredentials(ctx, actor, serverID); err == nil && srvUser != "" {
				req.Username = srvUser
				req.Password = srvPass
			}
		}
	}
	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := postAgentJSON(ctx, client, token, "/v1/onvif/imaging", req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func postAgentJSON(ctx context.Context, client AgentProbeDoer, token, path string, reqBody any, out any) error {
	body, err := json.Marshal(reqBody)
	if err != nil || len(body) > maxOnvifProbeRequestBytes || token == "" {
		return ErrAgentProbeFailed
	}
	ctx, cancel := context.WithTimeout(ctx, onvifProbeTimeout)
	defer cancel()
	response, err := client.Do(ctx, http.MethodPost, path, http.Header{
		"Authorization": []string{"Bearer " + token},
		"Content-Type":  []string{"application/json"},
	}, bytes.NewReader(body))
	if err != nil {
		return ErrAgentProbeUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("agent returned %d: %s", response.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil {
		data, err := io.ReadAll(io.LimitReader(response.Body, maxOnvifProbeResponseBytes+1))
		if err != nil || len(data) > maxOnvifProbeResponseBytes {
			return ErrAgentProbeFailed
		}
		if err := json.Unmarshal(data, out); err != nil {
			return ErrAgentProbeFailed
		}
	}
	return nil
}

// maxProvisionLPRStatusBytes bounds one status response from the agent: camera statuses and
// up to 20 recent events holding base64 JPEG images (crops and panoramas).
const maxProvisionLPRStatusBytes = 16 << 20

func getAgentJSON(ctx context.Context, client AgentProbeDoer, token, path string, out any) error {
	return getAgentJSONWithLimit(ctx, client, token, path, maxOnvifProbeResponseBytes, out)
}

func getAgentJSONWithLimit(ctx context.Context, client AgentProbeDoer, token, path string, maxBytes int64, out any) error {
	if token == "" {
		return ErrAgentProbeFailed
	}
	ctx, cancel := context.WithTimeout(ctx, onvifProbeTimeout)
	defer cancel()
	response, err := client.Do(ctx, http.MethodGet, path, http.Header{
		"Authorization": []string{"Bearer " + token},
	}, nil)
	if err != nil {
		return ErrAgentProbeUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("agent returned %d: %s", response.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil {
		data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
		if err != nil || int64(len(data)) > maxBytes {
			return ErrAgentProbeFailed
		}
		if err := json.Unmarshal(data, out); err != nil {
			return ErrAgentProbeFailed
		}
	}
	return nil
}

// ConfigureProvisionLPR sends an LPR configuration request to the edge agent.
func (s *Service) ConfigureProvisionLPR(ctx context.Context, actor authz.Actor, serverID uuid.UUID, params any) (map[string]any, error) {
	if s.Inv != nil {
		if err := s.Inv.RequireServerManageAndConfigSecrets(ctx, actor, serverID); err != nil {
			return nil, err
		}
	}
	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := postAgentJSON(ctx, client, token, "/v1/provision-lpr/configure", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetProvisionLPRStatus gets the heartbeat and LPR receiver status from the edge agent.
func (s *Service) GetProvisionLPRStatus(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (map[string]any, error) {
	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := getAgentJSONWithLimit(ctx, client, token, "/v1/provision-lpr/status", maxProvisionLPRStatusBytes, &out); err != nil {
		return nil, err
	}
	return out, nil
}
