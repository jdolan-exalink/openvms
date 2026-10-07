package provision

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// AgentView is the live reading shown on the servers screen.
type AgentView struct {
	Installed              bool
	Version                string
	Current                string
	Outdated               bool
	BinaryStatus           AgentBinaryStatus
	BinaryAvailable        *agent.BinaryIdentity
	BinaryObserved         *agent.BinaryIdentity
	BinaryOutdated         *bool
	BinaryUpgradeAvailable bool
	Variant                string
	NTP                    string
	CPUPercent             float64
	MemoryTotal            int64
	MemoryAvailable        int64
	Coral                  bool
	GPUPresent             bool
	GPUVendor              string
	GPUName                string
	CCTVTotal              int64
	CCTVFree               int64
	DatabaseTotal          int64
	DatabaseFree           int64
	Error                  string
}

// AgentStatus polls the edge agent. Servers without one are outdated.
func (s *Service) AgentStatus(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (AgentView, error) {
	out := AgentView{Current: agent.Version, Outdated: true, BinaryStatus: AgentBinaryNotInstalled}
	if _, err := s.Inv.GetServer(ctx, actor, serverID); err != nil {
		return AgentView{}, err
	}
	if artifact, err := s.desiredAgentArtifact(); err == nil && validAgentBinaryIdentity(artifact.Identity) {
		identity := artifact.Identity
		out.BinaryAvailable = &identity
		out.BinaryUpgradeAvailable = true
	}
	row, err := s.agentRow(ctx, actor, serverID)
	if errors.Is(err, store.ErrNotFound) {
		out.BinaryUpgradeAvailable = false
		return out, nil
	}
	if err != nil {
		return AgentView{}, err
	}
	out.Installed = true
	out.BinaryStatus = AgentBinaryUnknown
	out.Variant = row.Variant
	out.Version = row.Version
	token, err := s.Sealer.Open(row.TokenSealed, row.ServerID[:])
	if err != nil {
		out.Error = "agent credential cannot be opened"
		return out, nil
	}
	snap, err := fetchMetrics(ctx, row.Host, row.Port, string(token))
	if err != nil {
		out.Error = err.Error()
		out.Outdated = true
		status := classifyAgentBinaryStatus(true, out.BinaryAvailable, nil, true)
		out.BinaryStatus, out.BinaryOutdated, out.BinaryUpgradeAvailable = status.Status, status.Outdated, status.UpgradeAvailable
		return out, nil
	}
	applySnapshot(&out, snap)
	if identity, healthErr := s.verifiedAgentBinaryIdentity(ctx, actor, row, string(token)); healthErr == nil {
		out.BinaryObserved = &identity
		status := classifyAgentBinaryStatus(true, out.BinaryAvailable, out.BinaryObserved, false)
		out.BinaryStatus, out.BinaryOutdated, out.BinaryUpgradeAvailable = status.Status, status.Outdated, status.UpgradeAvailable
	} else {
		status := classifyAgentBinaryStatus(true, out.BinaryAvailable, nil, errors.Is(healthErr, errAgentHealthUnreachable))
		out.BinaryStatus, out.BinaryOutdated, out.BinaryUpgradeAvailable = status.Status, status.Outdated, status.UpgradeAvailable
	}
	if snap.Version != "" && snap.Version != row.Version {
		_ = s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
			return q.UpdateServerAgentVersion(ctx, db.UpdateServerAgentVersionParams{ServerID: serverID, Version: snap.Version})
		})
	}
	return out, nil
}

// UpdateAgent sends the current binary to the host and waits until it reports this version.
func (s *Service) UpdateAgent(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (AgentView, error) {
	if _, err := s.Inv.RequireServerManage(ctx, actor, serverID); err != nil {
		return AgentView{}, err
	}
	row, err := s.agentRow(ctx, actor, serverID)
	if err != nil {
		return AgentView{}, err
	}
	token, err := s.Sealer.Open(row.TokenSealed, row.ServerID[:])
	if err != nil {
		return AgentView{}, err
	}
	binary, err := s.loadLegacyAgentUpdateBinary()
	if err != nil {
		return AgentView{}, err
	}
	if err := pushUpdate(ctx, row.Host, row.Port, string(token), binary); err != nil {
		return AgentView{}, err
	}
	deadline := time.Now().Add(20 * time.Second)
	var last AgentView
	for {
		last, err = s.AgentStatus(ctx, actor, serverID)
		if err == nil && last.Installed && last.Version == agent.Version && last.Error == "" {
			return last, nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return AgentView{}, err
			}
			return last, nil
		}
		time.Sleep(time.Second)
	}
}

func (s *Service) agentRow(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (db.ServerAgent, error) {
	var row db.ServerAgent
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		var err error
		row, err = q.GetServerAgent(ctx, serverID)
		return store.Classify(err)
	})
	return row, err
}

func applySnapshot(out *AgentView, snap agent.Snapshot) {
	out.Version = snap.Version
	out.Current = agent.Version
	out.Outdated = snap.Version != agent.Version
	if snap.Variant != "" {
		out.Variant = snap.Variant
	}
	out.NTP = snap.NTP
	out.CPUPercent = snap.CPUPercent
	out.MemoryTotal = int64(snap.MemoryTotal)
	out.MemoryAvailable = int64(snap.MemoryAvailable)
	out.Coral = snap.Coral
	out.GPUPresent = snap.GPUPresent
	out.GPUVendor = snap.GPUVendor
	out.GPUName = snap.GPUName
	out.CCTVTotal = int64(snap.CCTVTotal)
	out.CCTVFree = int64(snap.CCTVFree)
	out.DatabaseTotal = int64(snap.DatabaseTotal)
	out.DatabaseFree = int64(snap.DatabaseFree)
	out.Error = ""
}

// Discover runs credential-free ONVIF discovery on a managed server's registered agent.
func (s *Service) Discover(ctx context.Context, actor authz.Actor, serverID uuid.UUID, interfaceName string) (DiscoveryResult, error) {
	if s.requireServerManage != nil {
		if err := s.requireServerManage(ctx, actor, serverID); err != nil {
			return DiscoveryResult{}, err
		}
	} else if _, err := s.Inv.RequireServerManage(ctx, actor, serverID); err != nil {
		return DiscoveryResult{}, err
	}
	row, err := s.agentRow(ctx, actor, serverID)
	if errors.Is(err, store.ErrNotFound) {
		return DiscoveryResult{}, ErrAgentUnprovisioned
	}
	if err != nil {
		return DiscoveryResult{}, err
	}
	token, err := s.Sealer.Open(row.TokenSealed, row.ServerID[:])
	if err != nil {
		return DiscoveryResult{}, ErrAgentDiscoveryUnknown
	}
	return discoverAgent(ctx, nil, row.Host, row.Port, string(token), interfaceName)
}
