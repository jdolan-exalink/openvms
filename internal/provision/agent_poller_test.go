package provision

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/agentstatus"
)

type recordingHeartbeats struct {
	ids []agentca.Identity
	hbs []agentstatus.Heartbeat
	err error
}

func (r *recordingHeartbeats) RecordHeartbeat(_ context.Context, id agentca.Identity, hb agentstatus.Heartbeat) error {
	r.ids, r.hbs = append(r.ids, id), append(r.hbs, hb)
	return r.err
}

// An agent that answers its REST metrics is online and reports its version, exactly like a heartbeat
// from an enrolled agent would: that is what lets a host that cannot call OpenVMS show as online.
func TestPollOneRecordsAnOnlineHeartbeatFromTheAgentMetrics(t *testing.T) {
	rec := &recordingHeartbeats{}
	p := &AgentPoller{Recorder: rec, fetch: func(context.Context, uuid.UUID) (agent.Snapshot, error) {
		return agent.Snapshot{Version: "0.4.0"}, nil
	}}
	tenant, server := uuid.New(), uuid.New()
	if err := p.pollOne(context.Background(), PollTarget{ServerID: server, TenantID: tenant}); err != nil {
		t.Fatal(err)
	}
	if len(rec.hbs) != 1 || rec.hbs[0].Status != agentstatus.Online || rec.hbs[0].Version != "0.4.0" ||
		rec.ids[0].ServerID != server || rec.ids[0].TenantID != tenant {
		t.Fatalf("recorded %+v for %+v", rec.hbs, rec.ids)
	}
}

// An unreachable agent records nothing: a heartbeat row means "seen now", and a failed poll saw nothing.
func TestPollOneRecordsNothingWhenTheAgentDoesNotAnswer(t *testing.T) {
	rec := &recordingHeartbeats{}
	p := &AgentPoller{Recorder: rec, fetch: func(context.Context, uuid.UUID) (agent.Snapshot, error) {
		return agent.Snapshot{}, errors.New("connection refused")
	}}
	if err := p.pollOne(context.Background(), PollTarget{ServerID: uuid.New(), TenantID: uuid.New()}); err == nil {
		t.Fatal("the failure must be returned for the log")
	}
	if len(rec.hbs) != 0 {
		t.Fatalf("recorded %+v for an agent that did not answer", rec.hbs)
	}
}

func TestPollOneDoesNotLetTheAgentChooseWhatIsStored(t *testing.T) {
	rec := &recordingHeartbeats{}
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'x'
	}
	p := &AgentPoller{Recorder: rec, fetch: func(context.Context, uuid.UUID) (agent.Snapshot, error) {
		return agent.Snapshot{Version: string(long)}, nil
	}}
	if err := p.pollOne(context.Background(), PollTarget{ServerID: uuid.New(), TenantID: uuid.New()}); err != nil {
		t.Fatal(err)
	}
	if len(rec.hbs) != 1 || len(rec.hbs[0].Version) > agentstatus.MaxVersionLen {
		t.Fatalf("version stored with %d bytes", len(rec.hbs[0].Version))
	}
}
