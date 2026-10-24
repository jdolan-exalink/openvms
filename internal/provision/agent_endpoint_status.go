package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

// fetchEndpointMetrics reads an agent's metrics through its stored base URL (LAN, VPN or a published
// name), with the client and trust that endpoint calls for.
func (s *Service) fetchEndpointMetrics(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (agent.Snapshot, error) {
	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return agent.Snapshot{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, onvifProbeTimeout)
	defer cancel()
	resp, err := client.Do(ctx, http.MethodGet, "/v1/metrics", http.Header{"Authorization": []string{"Bearer " + token}}, nil)
	if err != nil {
		return agent.Snapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return agent.Snapshot{}, fmt.Errorf("agent returned %d", resp.StatusCode)
	}
	var snap agent.Snapshot
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&snap); err != nil {
		return agent.Snapshot{}, err
	}
	return snap, nil
}

// fetchEndpointIdentity reads the binary identity an agent reports on /v1/health through its base URL.
func (s *Service) fetchEndpointIdentity(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (agent.BinaryIdentity, error) {
	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return agent.BinaryIdentity{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, onvifProbeTimeout)
	defer cancel()
	return readAgentIdentity(ctx, client, token)
}
