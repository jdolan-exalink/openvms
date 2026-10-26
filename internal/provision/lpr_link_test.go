package provision

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

var linkNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

type fakeLinkAgent struct {
	mu sync.Mutex

	statuses  []map[string]any // returned in order, the last one repeats
	statusErr error
	calls     int

	registry    LPRRegistry
	registryErr error

	ingestion    LPRIngestion
	ingestionErr error
}

func (f *fakeLinkAgent) GetProvisionLPRStatus(context.Context, authz.Actor, uuid.UUID) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	i := f.calls - 1
	if i >= len(f.statuses) {
		i = len(f.statuses) - 1
	}
	return f.statuses[i], nil
}

func (f *fakeLinkAgent) GetProvisionLPRRegistry(context.Context, authz.Actor, uuid.UUID) (LPRRegistry, error) {
	return f.registry, f.registryErr
}

func (f *fakeLinkAgent) GetProvisionLPRIngestion(context.Context, authz.Actor, uuid.UUID) (LPRIngestion, error) {
	return f.ingestion, f.ingestionErr
}

func (f *fakeLinkAgent) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func camStatus(id uuid.UUID, lastSeen time.Time, keepalives, events int, plate string) map[string]any {
	entry := map[string]any{
		"camera_id": id.String(), "camera_ip": "10.0.15.107", "online": true,
		"last_seen": lastSeen.Format(time.RFC3339Nano), "keepalive_count": float64(keepalives), "event_count": float64(events),
	}
	if events > 0 {
		entry["last_plate"] = plate
		entry["last_event_time"] = lastSeen.Format(time.RFC3339Nano)
	}
	return map[string]any{"cameras": []any{entry}}
}

func healthyAgent(id uuid.UUID) *fakeLinkAgent {
	pull := linkNow.Add(-10 * time.Second)
	return &fakeLinkAgent{
		statuses:  []map[string]any{camStatus(id, linkNow.Add(-5*time.Second), 40, 3, "AB123CD")},
		registry:  LPRRegistry{Enforce: true, Cameras: []LPRCamera{{CameraID: id.String(), IP: "10.0.15.107"}}},
		ingestion: LPRIngestion{Known: true, IngestedTotal: 3, LastPullAt: &pull},
	}
}

func newTester(a LPRLinkAgent) *LPRLinkTester {
	return &LPRLinkTester{Agent: a, Now: func() time.Time { return linkNow }, PollEvery: time.Millisecond}
}

func checkByID(t *testing.T, res LPRLinkResult, id string) LPRCheck {
	t.Helper()
	for _, c := range res.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check %q in %+v", id, res.Checks)
	return LPRCheck{}
}

func want(t *testing.T, res LPRLinkResult, id, status string) LPRCheck {
	t.Helper()
	c := checkByID(t, res, id)
	if c.Status != status {
		t.Fatalf("check %s = %s (%s | %s), want %s", id, c.Status, c.Message, c.Detail, status)
	}
	return c
}

func target(id uuid.UUID) LPRProvisionTarget {
	return LPRProvisionTarget{CameraID: id, ServerID: uuid.New(), CameraIP: "10.0.15.107", RemoteName: "escuela", DisplayName: "Escuela", TargetHost: "192.168.1.10"}
}

func TestDetectLPREngine(t *testing.T) {
	provisionCaps := json.RawMessage(`{"manufacturer":"Provision-ISR","model":"I6-240LPRN"}`)
	for name, tc := range map[string]struct {
		tags []string
		lpr  bool
		caps json.RawMessage
		want string
	}{
		"provision tag":        {tags: []string{"lpr", "provision"}, want: LPREngineProvision},
		"provision_isr tag":    {tags: []string{"provision_isr"}, want: LPREngineProvision},
		"onvif manufacturer":   {caps: provisionCaps, want: LPREngineProvision},
		"provision beats lpr":  {tags: []string{"provision"}, lpr: true, want: LPREngineProvision},
		"lpr tag":              {tags: []string{"lpr"}, want: LPREngineFrigate},
		"lpr mirror flag":      {lpr: true, want: LPREngineFrigate},
		"neither":              {tags: []string{"parking"}, want: LPREngineNone},
		"invalid onvif is not": {caps: json.RawMessage(`nope`), want: LPREngineNone},
	} {
		if got := DetectLPREngine(tc.tags, tc.lpr, tc.caps); got != tc.want {
			t.Errorf("%s: engine = %s, want %s", name, got, tc.want)
		}
	}
}

func TestValidateLPRTestWait(t *testing.T) {
	for _, ok := range []int{0, 1, 20, 30} {
		if err := ValidateLPRTestWait(ok); err != nil {
			t.Errorf("%d rejected: %v", ok, err)
		}
	}
	for _, bad := range []int{-1, 31, 1000} {
		var ve *ValidationError
		if err := ValidateLPRTestWait(bad); !errors.As(err, &ve) {
			t.Errorf("%d accepted or wrong error: %v", bad, err)
		}
	}
}

func TestProvisionLinkAllPass(t *testing.T) {
	id := uuid.New()
	res, err := newTester(healthyAgent(id)).CheckProvision(context.Background(), authz.Actor{}, target(id))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || res.Engine != LPREngineProvision {
		t.Fatalf("result = %+v", res)
	}
	for _, c := range []string{"agent_configured", "agent_reachable", "receiver_listening", "registered", "enforcement", "keepalive_recent", "event_seen", "central_ingestion"} {
		want(t, res, c, LPRCheckPass)
	}
	if res.Observed.LastSeen == nil || *res.Observed.KeepaliveCount != 40 || *res.Observed.EventCount != 3 || res.Observed.LastPlate != "AB123CD" || res.Observed.LastEventTime == nil {
		t.Fatalf("observed = %+v", res.Observed)
	}
	joined := strings.Join(res.Hints, " ")
	for _, s := range []string{"192.168.1.10:7419", "/SendKeepalive", "/v1/provision-lpr/event"} {
		if !strings.Contains(joined, s) {
			t.Errorf("hints %q miss %q", joined, s)
		}
	}
}

func TestProvisionLinkAgentNotConfigured(t *testing.T) {
	id := uuid.New()
	a := healthyAgent(id)
	a.statusErr = ErrAgentUnprovisioned
	res, err := newTester(a).CheckProvision(context.Background(), authz.Actor{}, target(id))
	if err != nil {
		t.Fatal(err)
	}
	want(t, res, "agent_configured", LPRCheckFail)
	want(t, res, "agent_reachable", LPRCheckSkip)
	want(t, res, "registered", LPRCheckSkip)
	if res.Passed {
		t.Fatal("a server without agent passed")
	}
}

func TestProvisionLinkAgentUnreachable(t *testing.T) {
	id := uuid.New()
	for _, e := range []error{ErrAgentProbeUnavailable, ErrAgentProbeFailed} {
		a := healthyAgent(id)
		a.statusErr = e
		res, err := newTester(a).CheckProvision(context.Background(), authz.Actor{}, target(id))
		if err != nil {
			t.Fatal(err)
		}
		want(t, res, "agent_configured", LPRCheckPass)
		want(t, res, "agent_reachable", LPRCheckFail)
		want(t, res, "receiver_listening", LPRCheckSkip)
		want(t, res, "keepalive_recent", LPRCheckSkip)
		if res.Passed {
			t.Fatal("an unreachable agent passed")
		}
	}
}

func TestProvisionLinkReceiverNotAnswering(t *testing.T) {
	id := uuid.New()
	a := healthyAgent(id)
	a.statusErr = errors.New("agent returned 404: not found")
	res, err := newTester(a).CheckProvision(context.Background(), authz.Actor{}, target(id))
	if err != nil {
		t.Fatal(err)
	}
	want(t, res, "agent_reachable", LPRCheckPass)
	c := want(t, res, "receiver_listening", LPRCheckFail)
	if !strings.Contains(c.Detail, "404") {
		t.Fatalf("detail = %q", c.Detail)
	}
	want(t, res, "registered", LPRCheckSkip)
}

func TestProvisionLinkNotRegisteredFailsWithHintAndNeverRegisters(t *testing.T) {
	id := uuid.New()
	a := healthyAgent(id)
	a.registry = LPRRegistry{Enforce: true}
	res, err := newTester(a).CheckProvision(context.Background(), authz.Actor{}, target(id))
	if err != nil {
		t.Fatal(err)
	}
	want(t, res, "registered", LPRCheckFail)
	if res.Passed {
		t.Fatal("an unregistered camera passed")
	}
	if !strings.Contains(strings.Join(res.Hints, "|"), "Registrá la cámara con Configurar y cargá los valores en la cámara") {
		t.Fatalf("hints = %v", res.Hints)
	}
	// The fake exposes only reads: the tester cannot register, and this compiles only while LPRLinkAgent stays read-only.
	var _ LPRLinkAgent = a
}

func TestProvisionLinkRegistryVariants(t *testing.T) {
	id := uuid.New()
	a := healthyAgent(id)
	a.registry = LPRRegistry{Enforce: false, Cameras: []LPRCamera{{CameraID: "other", IP: "10.0.15.107"}}}
	res, _ := newTester(a).CheckProvision(context.Background(), authz.Actor{}, target(id))
	want(t, res, "registered", LPRCheckPass) // matched by address
	c := want(t, res, "enforcement", LPRCheckWarn)
	if !strings.Contains(c.Detail, "cualquier IP") {
		t.Fatalf("detail = %q", c.Detail)
	}
	if !res.Passed {
		t.Fatal("a warning must not fail the test")
	}

	a.registryErr = ErrAgentRegistryUnsupported
	res, _ = newTester(a).CheckProvision(context.Background(), authz.Actor{}, target(id))
	want(t, res, "registered", LPRCheckFail)
	want(t, res, "enforcement", LPRCheckSkip)

	a.registryErr = errors.New("boom")
	res, _ = newTester(a).CheckProvision(context.Background(), authz.Actor{}, target(id))
	want(t, res, "registered", LPRCheckFail)
}

func TestProvisionLinkKeepalive(t *testing.T) {
	id := uuid.New()
	a := healthyAgent(id)

	a.statuses = []map[string]any{{"cameras": []any{}}}
	res, _ := newTester(a).CheckProvision(context.Background(), authz.Actor{}, target(id))
	want(t, res, "keepalive_recent", LPRCheckFail)
	want(t, res, "event_seen", LPRCheckSkip)

	a.statuses = []map[string]any{camStatus(id, linkNow.Add(-5*time.Minute), 9, 0, "")}
	res, _ = newTester(a).CheckProvision(context.Background(), authz.Actor{}, target(id))
	want(t, res, "keepalive_recent", LPRCheckFail)
	want(t, res, "event_seen", LPRCheckWarn)
	if res.Passed {
		t.Fatal("a stale keepalive passed")
	}

	a.statuses = []map[string]any{camStatus(id, linkNow.Add(-59*time.Second), 9, 0, "")}
	res, _ = newTester(a).CheckProvision(context.Background(), authz.Actor{}, target(id))
	want(t, res, "keepalive_recent", LPRCheckPass)
}

func TestProvisionLinkKeepalivesWithoutEventsWarns(t *testing.T) {
	id := uuid.New()
	a := healthyAgent(id)
	a.statuses = []map[string]any{camStatus(id, linkNow.Add(-5*time.Second), 9, 0, "")}
	res, _ := newTester(a).CheckProvision(context.Background(), authz.Actor{}, target(id))
	c := want(t, res, "event_seen", LPRCheckWarn)
	if c.Message != "La cámara está conectada; falta una lectura de patente" {
		t.Fatalf("message = %q", c.Message)
	}
	if !res.Passed {
		t.Fatal("a warning must not fail the test")
	}
}

func TestProvisionLinkIngestionVariants(t *testing.T) {
	id := uuid.New()
	stale := linkNow.Add(-10 * time.Minute)
	fresh := linkNow.Add(-5 * time.Second)
	for name, tc := range map[string]struct {
		ing  LPRIngestion
		err  error
		want string
	}{
		"unknown":  {ing: LPRIngestion{}, want: LPRCheckWarn},
		"error":    {err: errors.New("db down"), want: LPRCheckFail},
		"last err": {ing: LPRIngestion{Known: true, LastPullAt: &fresh, LastError: "agent 500"}, want: LPRCheckFail},
		"stale":    {ing: LPRIngestion{Known: true, LastPullAt: &stale}, want: LPRCheckFail},
		"no pull":  {ing: LPRIngestion{Known: true}, want: LPRCheckFail},
		"lost":     {ing: LPRIngestion{Known: true, LastPullAt: &fresh, AgentLost: 4}, want: LPRCheckWarn},
		"backlog":  {ing: LPRIngestion{Known: true, LastPullAt: &fresh, AgentDepth: 500}, want: LPRCheckWarn},
		"healthy":  {ing: LPRIngestion{Known: true, LastPullAt: &fresh, AgentDepth: 2}, want: LPRCheckPass},
	} {
		a := healthyAgent(id)
		a.ingestion, a.ingestionErr = tc.ing, tc.err
		res, err := newTester(a).CheckProvision(context.Background(), authz.Actor{}, target(id))
		if err != nil {
			t.Fatal(err)
		}
		if got := checkByID(t, res, "central_ingestion").Status; got != tc.want {
			t.Errorf("%s: central_ingestion = %s, want %s", name, got, tc.want)
		}
	}
}

func TestProvisionLinkWaitReturnsAsSoonAsAKeepaliveArrives(t *testing.T) {
	id := uuid.New()
	a := healthyAgent(id)
	// The camera's last keepalive is older than the call; the third poll sees a newer one.
	old := camStatus(id, linkNow.Add(-30*time.Second), 9, 0, "")
	newer := camStatus(id, linkNow.Add(2*time.Second), 10, 0, "")
	a.statuses = []map[string]any{old, old, old, newer}
	tgt := target(id)
	tgt.WaitSeconds = 20
	start := time.Now()
	res, err := newTester(a).CheckProvision(context.Background(), authz.Actor{}, tgt)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("did not return when the keepalive arrived")
	}
	want(t, res, "keepalive_recent", LPRCheckPass)
	if *res.Observed.KeepaliveCount != 10 {
		t.Fatalf("observed = %+v", res.Observed)
	}
	if a.callCount() != 4 {
		t.Fatalf("status calls = %d, want 4", a.callCount())
	}
}

func TestProvisionLinkWaitStopsAtTheDeadline(t *testing.T) {
	id := uuid.New()
	a := healthyAgent(id)
	a.statuses = []map[string]any{{"cameras": []any{}}}
	tgt := target(id)
	tgt.WaitSeconds = 1
	start := time.Now()
	res, err := newTester(a).CheckProvision(context.Background(), authz.Actor{}, tgt)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 900*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("waited %s, want about 1 s", elapsed)
	}
	c := want(t, res, "keepalive_recent", LPRCheckFail)
	if !strings.Contains(c.Message, "durante la espera") {
		t.Fatalf("message = %q", c.Message)
	}
	calls := a.callCount()
	time.Sleep(50 * time.Millisecond)
	if a.callCount() != calls {
		t.Fatal("kept polling after the deadline")
	}
}

func TestProvisionLinkWaitStopsWhenTheContextIsCancelled(t *testing.T) {
	id := uuid.New()
	a := healthyAgent(id)
	a.statuses = []map[string]any{{"cameras": []any{}}}
	tgt := target(id)
	tgt.WaitSeconds = 30
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := newTester(a).CheckProvision(ctx, authz.Actor{}, tgt)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("did not stop on cancellation")
	}
	calls := a.callCount()
	time.Sleep(50 * time.Millisecond)
	if a.callCount() != calls {
		t.Fatal("kept polling after cancellation")
	}
}

func TestFrigateLinkChecks(t *testing.T) {
	last := linkNow.Add(-time.Hour)
	probe := func(enabled bool, enabledErr error, server bool, reads LPRReadsSummary, readsErr error) LPRFrigateProbe {
		return LPRFrigateProbe{
			LPREnabled: func(context.Context) (bool, error) { return enabled, enabledErr },
			ServerLPR:  server,
			Reads24h:   func(context.Context) (LPRReadsSummary, error) { return reads, readsErr },
		}
	}
	res := CheckLPRFrigate(context.Background(), probe(true, nil, true, LPRReadsSummary{Count: 7, LastAt: &last}, nil))
	if !res.Passed || res.Engine != LPREngineFrigate {
		t.Fatalf("result = %+v", res)
	}
	want(t, res, "frigate_lpr_enabled", LPRCheckPass)
	want(t, res, "capability", LPRCheckPass)
	want(t, res, "reads_recent", LPRCheckPass)
	if *res.Observed.Reads24h != 7 || res.Observed.LastReadAt == nil {
		t.Fatalf("observed = %+v", res.Observed)
	}

	res = CheckLPRFrigate(context.Background(), probe(false, nil, true, LPRReadsSummary{}, nil))
	want(t, res, "frigate_lpr_enabled", LPRCheckFail)
	want(t, res, "reads_recent", LPRCheckWarn)
	if res.Passed || len(res.Hints) == 0 {
		t.Fatalf("result = %+v", res)
	}

	res = CheckLPRFrigate(context.Background(), probe(true, nil, false, LPRReadsSummary{Count: 1}, nil))
	want(t, res, "capability", LPRCheckFail)
	if res.Passed {
		t.Fatal("a server without lpr capability passed")
	}

	res = CheckLPRFrigate(context.Background(), probe(false, errors.New("frigate down"), true, LPRReadsSummary{}, errors.New("db")))
	want(t, res, "frigate_lpr_enabled", LPRCheckFail)
	want(t, res, "reads_recent", LPRCheckWarn)
	if res.Observed.Reads24h != nil {
		t.Fatal("reported reads that could not be read")
	}

	res = CheckLPRFrigate(context.Background(), probe(true, nil, true, LPRReadsSummary{}, nil))
	want(t, res, "reads_recent", LPRCheckWarn)
	if !res.Passed {
		t.Fatal("zero reads must warn, not fail")
	}
}

func TestNotLPRResult(t *testing.T) {
	res := NotLPRResult()
	if res.Engine != LPREngineNone || !res.Passed || len(res.Checks) != 1 || res.Checks[0].Status != LPRCheckSkip || res.Checks[0].Message != "La cámara no es LPR" {
		t.Fatalf("result = %+v", res)
	}
}
