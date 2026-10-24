package provision

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/agentstatus"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
)

// HeartbeatRecorder stores what is known about an agent; agentstatus.Recorder implements it.
type HeartbeatRecorder interface {
	RecordHeartbeat(ctx context.Context, id agentca.Identity, hb agentstatus.Heartbeat) error
}

// PollTarget is a server whose agent OpenVMS has to ask for its state.
type PollTarget struct {
	ServerID, TenantID uuid.UUID
}

const (
	defaultPollInterval   = 15 * time.Second
	defaultPollStaleAfter = 45 * time.Second
	pollTimeout           = 20 * time.Second
)

// AgentPoller keeps the status of agents that do not send a heartbeat: those whose host cannot
// reach OpenVMS (behind a NAT, on another network). OpenVMS asks their REST API and records the
// answer as the heartbeat it would have received, so the server list and the offline rules treat
// every agent the same. An agent whose heartbeat is fresh is left alone.
type AgentPoller struct {
	Service     *Service
	Store       *store.Store
	Recorder    HeartbeatRecorder
	Log         *slog.Logger
	Interval    time.Duration
	StaleAfter  time.Duration
	Concurrency int

	// fetch reads an agent's metrics; tests replace it.
	fetch func(ctx context.Context, serverID uuid.UUID) (agent.Snapshot, error)
}

// Run polls until ctx ends.
func (p *AgentPoller) Run(ctx context.Context) {
	interval := p.Interval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		p.PollAll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// PollAll asks every agent without a fresh heartbeat once.
func (p *AgentPoller) PollAll(ctx context.Context) {
	targets, err := p.targets(ctx)
	if err != nil {
		p.logger().ErrorContext(ctx, "agent poll: list agents", "error", err)
		return
	}
	sem := make(chan struct{}, max(p.Concurrency, 8))
	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			cctx, cancel := context.WithTimeout(ctx, pollTimeout)
			defer cancel()
			if err := p.pollOne(cctx, target); err != nil && ctx.Err() == nil {
				p.logger().DebugContext(ctx, "agent poll: no answer", "server_id", target.ServerID, "error", err)
			}
		}()
	}
	wg.Wait()
}

// targets lists the servers with an agent and no heartbeat newer than StaleAfter.
func (p *AgentPoller) targets(ctx context.Context) ([]PollTarget, error) {
	stale := p.StaleAfter
	if stale <= 0 {
		stale = defaultPollStaleAfter
	}
	var out []PollTarget
	err := p.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
SELECT sa.server_id, sa.tenant_id
FROM server_agents sa
JOIN frigate_servers fs ON fs.id = sa.server_id AND fs.deleted_at IS NULL
LEFT JOIN agent_heartbeats ah ON ah.server_id = sa.server_id
WHERE ah.last_seen_at IS NULL OR ah.last_seen_at < now() - make_interval(secs => $1)`, stale.Seconds())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t PollTarget
			if err := rows.Scan(&t.ServerID, &t.TenantID); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// pollOne asks one agent and records an online heartbeat when it answers. A failure records nothing:
// a heartbeat row means "seen now", and the staleness of the last one is what marks the agent offline.
func (p *AgentPoller) pollOne(ctx context.Context, target PollTarget) error {
	fetch := p.fetch
	if fetch == nil {
		fetch = p.Service.fetchAgentSnapshot
	}
	snap, err := fetch(ctx, target.ServerID)
	if err != nil {
		return err
	}
	version := snap.Version
	if len(version) > agentstatus.MaxVersionLen {
		version = version[:agentstatus.MaxVersionLen] // what the agent says about itself does not choose what is stored
	}
	return p.Recorder.RecordHeartbeat(ctx, agentca.Identity{TenantID: target.TenantID, ServerID: target.ServerID},
		agentstatus.Heartbeat{Version: version, Status: agentstatus.Online})
}

func (p *AgentPoller) logger() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.Default()
}

// fetchAgentSnapshot reads an agent's metrics as the system actor, the way the agent panel does: a
// registered base URL goes through its endpoint client, a legacy host:port over plain HTTP.
func (s *Service) fetchAgentSnapshot(ctx context.Context, serverID uuid.UUID) (agent.Snapshot, error) {
	system := authz.Actor{System: true}
	row, err := s.agentRow(ctx, system, serverID)
	if err != nil {
		return agent.Snapshot{}, err
	}
	if row.BaseUrl != "" {
		return s.fetchEndpointMetrics(ctx, system, serverID)
	}
	token, err := s.Sealer.Open(row.TokenSealed, row.ServerID[:])
	if err != nil {
		return agent.Snapshot{}, errors.New("agent credential cannot be opened")
	}
	return fetchMetrics(ctx, row.Host, row.Port, string(token))
}
