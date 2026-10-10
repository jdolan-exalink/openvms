package control

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/agentauth/agentauthtest"
	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/agentstatus"
)

type recordedHeartbeat struct {
	id agentca.Identity
	hb agentstatus.Heartbeat
}

type fakeRecorder struct {
	mu  sync.Mutex
	got []recordedHeartbeat
	err error
}

func (f *fakeRecorder) RecordHeartbeat(_ context.Context, id agentca.Identity, hb agentstatus.Heartbeat) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.got = append(f.got, recordedHeartbeat{id, hb})
	return nil
}

func (f *fakeRecorder) recorded() []recordedHeartbeat {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedHeartbeat(nil), f.got...)
}

func startHeartbeatServer(t *testing.T) (*agentHarness, *fakeRecorder) {
	t.Helper()
	rec := &fakeRecorder{}
	h := startAgentServer(t, func(cfg *AgentConfig, _ *agentauthtest.PKI) { cfg.Heartbeats = rec })
	return h, rec
}

func send(t *testing.T, h *agentHarness, c agentauthtest.Client, req *openvmsv1.NodeHeartbeatRequest) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := h.nodes(t, &c.TLS).Heartbeat(ctx, req)
	return err
}

func TestAgentListener_HeartbeatIsPersistedForTheCertificateIdentity(t *testing.T) {
	h, rec := startHeartbeatServer(t)
	c := h.pki.Issue(t, agentID, time.Now(), time.Hour)
	err := send(t, h, c, &openvmsv1.NodeHeartbeatRequest{
		NodeId: agentID.ServerID.String(), Version: " 0.1.1 ", Status: openvmsv1.OperationalStatus_OPERATIONAL_STATUS_DEGRADED,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := rec.recorded()
	if len(got) != 1 || got[0].id != agentID || got[0].hb != (agentstatus.Heartbeat{Version: "0.1.1", Status: "degraded"}) {
		t.Fatalf("recorded = %+v, want one degraded 0.1.1 heartbeat for %+v", got, agentID)
	}
}

func TestAgentListener_HeartbeatStatusMapping(t *testing.T) {
	h, rec := startHeartbeatServer(t)
	c := h.pki.Issue(t, agentID, time.Now(), time.Hour)
	for in, want := range map[openvmsv1.OperationalStatus]string{
		openvmsv1.OperationalStatus_OPERATIONAL_STATUS_UNSPECIFIED: "unknown",
		openvmsv1.OperationalStatus_OPERATIONAL_STATUS_ONLINE:      "online",
		openvmsv1.OperationalStatus_OPERATIONAL_STATUS_OFFLINE:     "offline",
		openvmsv1.OperationalStatus_OPERATIONAL_STATUS_DEGRADED:    "degraded",
		openvmsv1.OperationalStatus_OPERATIONAL_STATUS_MAINTENANCE: "maintenance",
		openvmsv1.OperationalStatus(99):                            "unknown",
	} {
		before := len(rec.recorded())
		if err := send(t, h, c, &openvmsv1.NodeHeartbeatRequest{NodeId: agentID.ServerID.String(), Status: in}); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		got := rec.recorded()
		if len(got) != before+1 || got[before].hb.Status != want {
			t.Errorf("status %s stored as %+v, want %q", in, got, want)
		}
	}
}

func TestAgentListener_RejectedHeartbeatsAreNotPersisted(t *testing.T) {
	h, rec := startHeartbeatServer(t)
	c := h.pki.Issue(t, agentID, time.Now(), time.Hour)
	cases := map[string]*openvmsv1.NodeHeartbeatRequest{
		"another server's node_id": {NodeId: "00000000-0000-0000-0000-000000000001"},
		"empty node_id":            {},
		"malformed node_id":        {NodeId: "x"},
		"oversized version":        {NodeId: agentID.ServerID.String(), Version: strings.Repeat("v", agentstatus.MaxVersionLen+1)},
	}
	for name, req := range cases {
		if err := send(t, h, c, req); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	h.pki.Store.Update(c.Serial, func(r *agentca.Certificate) { n := time.Now(); r.RevokedAt = &n })
	if err := send(t, h, c, &openvmsv1.NodeHeartbeatRequest{NodeId: agentID.ServerID.String()}); status.Code(err) != codes.Unauthenticated {
		t.Errorf("revoked: code = %s, want Unauthenticated", status.Code(err))
	}
	if got := rec.recorded(); len(got) != 0 {
		t.Fatalf("recorded %+v for rejected heartbeats, want none", got)
	}
}

func TestAgentListener_HeartbeatFailsClosedWhenItCannotBePersisted(t *testing.T) {
	h, rec := startHeartbeatServer(t)
	c := h.pki.Issue(t, agentID, time.Now(), time.Hour)
	rec.err = errors.New("pq: connection to 10.0.0.5 refused")
	err := send(t, h, c, &openvmsv1.NodeHeartbeatRequest{NodeId: agentID.ServerID.String()})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("code = %s (%v), want Unavailable so the agent retries", status.Code(err), err)
	}
	if strings.Contains(status.Convert(err).Message(), "10.0.0.5") {
		t.Fatalf("the error leaks the cause: %v", err)
	}
}

func TestAgentListener_HeartbeatWithoutARecorderStillAcknowledges(t *testing.T) {
	h := startAgentServer(t)
	c := h.pki.Issue(t, agentID, time.Now(), time.Hour)
	if err := send(t, h, c, &openvmsv1.NodeHeartbeatRequest{NodeId: agentID.ServerID.String()}); err != nil {
		t.Fatal(err)
	}
}
