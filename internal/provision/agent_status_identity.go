package provision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type AgentBinaryStatus string

const (
	AgentBinaryNotInstalled    AgentBinaryStatus = "not_installed"
	AgentBinaryCurrent         AgentBinaryStatus = "current"
	AgentBinaryUpdateAvailable AgentBinaryStatus = "update_available"
	AgentBinaryUnknown         AgentBinaryStatus = "unknown"
	AgentBinaryUnreachable     AgentBinaryStatus = "unreachable"
)

var errAgentHealthUnreachable = errors.New("verified agent health endpoint is unreachable")

type agentBinaryStatusResult struct {
	Status           AgentBinaryStatus
	Outdated         *bool
	UpgradeAvailable bool
}

func classifyAgentBinaryStatus(installed bool, available, observed *agent.BinaryIdentity, unreachable bool) agentBinaryStatusResult {
	if !installed {
		return agentBinaryStatusResult{Status: AgentBinaryNotInstalled}
	}
	if unreachable {
		return agentBinaryStatusResult{Status: AgentBinaryUnreachable, UpgradeAvailable: available != nil}
	}
	if available == nil || !validAgentBinaryIdentity(*available) || observed == nil || !validAgentBinaryIdentity(*observed) {
		return agentBinaryStatusResult{Status: AgentBinaryUnknown, UpgradeAvailable: available != nil}
	}
	if available.Architecture != observed.Architecture {
		return agentBinaryStatusResult{Status: AgentBinaryUnknown, UpgradeAvailable: true}
	}
	if strings.EqualFold(available.SHA256, observed.SHA256) {
		return agentBinaryStatusResult{Status: AgentBinaryCurrent, Outdated: boolPointer(false)}
	}
	return agentBinaryStatusResult{Status: AgentBinaryUpdateAvailable, Outdated: boolPointer(true), UpgradeAvailable: true}
}

func boolPointer(value bool) *bool { return &value }

func validAgentBinaryIdentity(identity agent.BinaryIdentity) bool {
	if len(identity.SHA256) != sha256.Size*2 || (identity.Architecture != "amd64" && identity.Architecture != "arm64") || len(identity.Version) > 256 || len(identity.Commit) > 256 {
		return false
	}
	digest, err := hex.DecodeString(identity.SHA256)
	return err == nil && len(digest) == sha256.Size && hex.EncodeToString(digest) == identity.SHA256
}

func (s *Service) desiredAgentArtifact() (AgentArtifact, error) {
	s.agentStatusArtifactOnce.Do(func() {
		if s.agentStatusArtifactLoader != nil {
			s.cachedAgentArtifact, s.cachedAgentArtifactErr = s.agentStatusArtifactLoader()
			return
		}
		s.cachedAgentArtifact, s.cachedAgentArtifactErr = LoadTrustedAgentArtifact("/opt/openvms/edge-agent")
	})
	return s.cachedAgentArtifact, s.cachedAgentArtifactErr
}

func (s *Service) loadLegacyAgentUpdateBinary() ([]byte, error) {
	return s.loadAgentInstallBinary()
}

func (s *Service) verifiedAgentBinaryIdentity(ctx context.Context, actor authz.Actor, row db.ServerAgent, token string) (agent.BinaryIdentity, error) {
	tlsRow, err := s.agentTLSRow(ctx, actor, row.ServerID)
	if err != nil {
		return agent.BinaryIdentity{}, err
	}
	return fetchVerifiedAgentIdentity(ctx, row.Host, token, configFromRow(tlsRow), s.agentStatusRoundTripper)
}

func fetchVerifiedAgentIdentity(ctx context.Context, registeredHost, token string, cfg AgentTLSConfig, testTransport http.RoundTripper) (agent.BinaryIdentity, error) {
	if cfg.SecurePort < 1 || cfg.SecurePort > 65535 {
		return agent.BinaryIdentity{}, errors.New("invalid registered agent TLS port")
	}
	useSystemRoots := false
	switch cfg.TrustMode {
	case AgentTLSTrustSystem:
		useSystemRoots = true
	case AgentTLSTrustCustom:
	default:
		return agent.BinaryIdentity{}, errors.New("invalid registered agent TLS trust mode")
	}
	client, err := newVerifiedAgentHTTPClient(registeredHost, uint16(cfg.SecurePort), cfg.CAPEM, useSystemRoots, 4*time.Second, nil, testTransport)
	if err != nil {
		return agent.BinaryIdentity{}, err
	}
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(ctx, http.MethodGet, "/v1/health", header, nil)
	if err != nil {
		return agent.BinaryIdentity{}, errors.Join(errAgentHealthUnreachable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return agent.BinaryIdentity{}, errors.New("agent health response was not successful")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, agent.MaxHealthResponseBytes+1))
	if err != nil || len(body) > agent.MaxHealthResponseBytes {
		return agent.BinaryIdentity{}, errors.New("agent health response exceeded its size bound")
	}
	var health struct {
		Status         string               `json:"status"`
		BinaryIdentity agent.BinaryIdentity `json:"binary_identity"`
	}
	if err := json.Unmarshal(body, &health); err != nil || health.Status != "ok" || !validAgentBinaryIdentity(health.BinaryIdentity) {
		return agent.BinaryIdentity{}, errors.New("agent health identity is unavailable")
	}
	return health.BinaryIdentity, nil
}
