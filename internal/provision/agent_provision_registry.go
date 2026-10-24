package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

// ErrAgentRegistryUnsupported means the edge agent answered 404 to a registry route, which is what an
// agent older than the camera registry does.
var ErrAgentRegistryUnsupported = errors.New("the agent does not support the Provision-ISR camera registry; update the agent")

// AgentStatusError is an unsuccessful answer from the edge agent, with what it said.
type AgentStatusError struct {
	Status  int
	Message string
}

func (e *AgentStatusError) Error() string {
	return fmt.Sprintf("agent returned %d: %s", e.Status, e.Message)
}

// LPRCamera is a camera registered on an agent.
type LPRCamera struct {
	CameraID string `json:"camera_id"`
	IP       string `json:"ip"`
}

// LPRSource is an address that sent requests to the agent without being registered.
type LPRSource struct {
	IP        string `json:"ip"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
	Requests  int64  `json:"requests"`
	LastPath  string `json:"last_path"`
	Rejected  bool   `json:"rejected"`
}

// LPRRegistry is the camera registry of one agent.
type LPRRegistry struct {
	Enforce      bool        `json:"enforce"`
	Cameras      []LPRCamera `json:"cameras"`
	Unregistered []LPRSource `json:"unregistered_seen"`
	LoadError    string      `json:"load_error,omitempty"`
}

const provisionRegistryPath = "/v1/provision-lpr/registry"

// RegisterProvisionLPRCamera tells the server's agent which address a camera connects from, so the
// agent can identify it and, once enforcement is on, accept it.
func (s *Service) RegisterProvisionLPRCamera(ctx context.Context, actor authz.Actor, serverID, cameraID uuid.UUID, ip string) (LPRRegistry, error) {
	body := LPRCamera{CameraID: cameraID.String(), IP: ip}
	return s.registryCall(ctx, actor, serverID, http.MethodPost, provisionRegistryPath+"/cameras", body)
}

// UnregisterProvisionLPRCamera removes a camera from the server's agent registry.
func (s *Service) UnregisterProvisionLPRCamera(ctx context.Context, actor authz.Actor, serverID, cameraID uuid.UUID) error {
	_, err := s.registryCall(ctx, actor, serverID, http.MethodDelete, provisionRegistryPath+"/cameras/"+cameraID.String(), nil)
	return err
}

// SetProvisionLPREnforce turns on or off the refusal of cameras that are not registered.
func (s *Service) SetProvisionLPREnforce(ctx context.Context, actor authz.Actor, serverID uuid.UUID, enforce bool) (LPRRegistry, error) {
	return s.registryCall(ctx, actor, serverID, http.MethodPut, provisionRegistryPath+"/enforce", map[string]bool{"enforce": enforce})
}

// GetProvisionLPRRegistry reads the registry and the unregistered sources the agent has seen.
func (s *Service) GetProvisionLPRRegistry(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (LPRRegistry, error) {
	return s.registryCall(ctx, actor, serverID, http.MethodGet, provisionRegistryPath, nil)
}

func (s *Service) registryCall(ctx context.Context, actor authz.Actor, serverID uuid.UUID, method, path string, reqBody any) (LPRRegistry, error) {
	if err := s.authorizeServerManage(ctx, actor, serverID); err != nil {
		return LPRRegistry{}, err
	}
	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return LPRRegistry{}, err
	}
	var payload io.Reader
	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil || len(data) > maxOnvifProbeRequestBytes {
			return LPRRegistry{}, ErrAgentProbeFailed
		}
		payload = bytes.NewReader(data)
		headers.Set("Content-Type", "application/json")
	}
	ctx, cancel := context.WithTimeout(ctx, onvifProbeTimeout)
	defer cancel()
	response, err := client.Do(ctx, method, path, headers, payload)
	if err != nil {
		return LPRRegistry{}, ErrAgentProbeUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
		data, err := io.ReadAll(io.LimitReader(response.Body, maxOnvifProbeResponseBytes+1))
		if err != nil || len(data) > maxOnvifProbeResponseBytes {
			return LPRRegistry{}, ErrAgentProbeFailed
		}
		var out LPRRegistry
		if err := json.Unmarshal(data, &out); err != nil {
			return LPRRegistry{}, ErrAgentProbeFailed
		}
		return out, nil
	case http.StatusNoContent:
		return LPRRegistry{}, nil
	case http.StatusNotFound:
		return LPRRegistry{}, ErrAgentRegistryUnsupported
	default:
		b, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return LPRRegistry{}, &AgentStatusError{Status: response.StatusCode, Message: strings.TrimSpace(string(b))}
	}
}
