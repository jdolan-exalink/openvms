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
	Installed       bool
	Version         string
	Current         string
	Outdated        bool
	Variant         string
	NTP             string
	CPUPercent      float64
	MemoryTotal     int64
	MemoryAvailable int64
	Coral           bool
	GPUPresent      bool
	GPUVendor       string
	GPUName         string
	CCTVTotal       int64
	CCTVFree        int64
	DatabaseTotal   int64
	DatabaseFree    int64
	Error           string
}

// AgentStatus polls the edge agent. Servers without one are outdated.
func (s *Service) AgentStatus(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (AgentView, error) {
	out := AgentView{Current: agent.Version, Outdated: true}
	if _, err := s.Inv.GetServer(ctx, actor, serverID); err != nil {
		return AgentView{}, err
	}
	row, err := s.agentRow(ctx, actor, serverID)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return AgentView{}, err
	}
	out.Installed = true
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
		return out, nil
	}
	applySnapshot(&out, snap)
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
	binary, err := s.Binary()
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
