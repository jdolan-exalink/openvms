package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type fakeLPRAgent struct {
	status    map[string]any
	statusErr error
	registry  provision.LPRRegistry
	ingestion provision.LPRIngestion
	calls     int
}

func (f *fakeLPRAgent) GetProvisionLPRStatus(context.Context, authz.Actor, uuid.UUID) (map[string]any, error) {
	f.calls++
	return f.status, f.statusErr
}

func (f *fakeLPRAgent) GetProvisionLPRRegistry(context.Context, authz.Actor, uuid.UUID) (provision.LPRRegistry, error) {
	return f.registry, nil
}

func (f *fakeLPRAgent) GetProvisionLPRIngestion(context.Context, authz.Actor, uuid.UUID) (provision.LPRIngestion, error) {
	return f.ingestion, nil
}

type fakeLPRSource struct {
	cam        db.GetCameraRow
	camErr     error
	manageErr  error
	server     inventory.ServerView
	address    string
	enabled    bool
	enabledErr error
	reads      provision.LPRReadsSummary
	readsErr   error
	agent      *fakeLPRAgent
}

func (f *fakeLPRSource) GetCamera(context.Context, authz.Actor, uuid.UUID) (db.GetCameraRow, error) {
	return f.cam, f.camErr
}

func (f *fakeLPRSource) RequireServerManage(context.Context, authz.Actor, uuid.UUID) (inventory.ServerView, error) {
	return f.server, f.manageErr
}

func (f *fakeLPRSource) CameraAddress(context.Context, authz.Actor, db.GetCameraRow) string {
	return f.address
}

func (f *fakeLPRSource) FrigateLPREnabled(context.Context, authz.Actor, uuid.UUID) (bool, error) {
	return f.enabled, f.enabledErr
}

func (f *fakeLPRSource) Reads24h(context.Context, authz.Actor, uuid.UUID, time.Time) (provision.LPRReadsSummary, error) {
	return f.reads, f.readsErr
}

func (f *fakeLPRSource) Tester() *provision.LPRLinkTester {
	return &provision.LPRLinkTester{Agent: f.agent, PollEvery: time.Millisecond}
}

func newLPRSource(tags ...string) *fakeLPRSource {
	camID := uuid.New()
	pull := time.Now().Add(-5 * time.Second)
	seen := time.Now().Add(-3 * time.Second)
	caps, _ := json.Marshal(map[string]bool{"lpr": true})
	return &fakeLPRSource{
		cam:     db.GetCameraRow{ID: camID, ServerID: uuid.New(), RemoteName: "escuela", DisplayName: "Escuela", Tags: tags},
		server:  inventory.ServerView{GetServerRow: db.GetServerRow{BaseUrl: "http://192.168.1.10:5000", Capabilities: caps}},
		address: "10.0.15.107",
		enabled: true,
		reads:   provision.LPRReadsSummary{Count: 3, LastAt: &seen},
		agent: &fakeLPRAgent{
			status: map[string]any{"cameras": []any{map[string]any{
				"camera_id": camID.String(), "camera_ip": "10.0.15.107", "online": true,
				"last_seen": seen.Format(time.RFC3339Nano), "keepalive_count": float64(12), "event_count": float64(2), "last_plate": "AB123CD",
				"last_event_time": seen.Format(time.RFC3339Nano),
			}}},
			registry:  provision.LPRRegistry{Enforce: true, Cameras: []provision.LPRCamera{{CameraID: camID.String(), IP: "10.0.15.107"}}},
			ingestion: provision.LPRIngestion{Known: true, LastPullAt: &pull},
		},
	}
}

func runLPRTest(t *testing.T, src *fakeLPRSource, wait *int) (gen.TestCameraLprResponseObject, error) {
	t.Helper()
	h := &Handlers{lprTest: src}
	return h.TestCameraLpr(testActorCtx(), gen.TestCameraLprRequestObject{CameraId: src.cam.ID, Body: &gen.TestCameraLprJSONRequestBody{WaitSeconds: wait}})
}

func testActorCtx() context.Context {
	return WithActor(context.Background(), authz.Actor{UserID: uuid.New()})
}

func lprOK(t *testing.T, resp gen.TestCameraLprResponseObject, err error) gen.CameraLprTest {
	t.Helper()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	ok, isOK := resp.(gen.TestCameraLpr200JSONResponse)
	if !isOK {
		t.Fatalf("response = %T", resp)
	}
	return gen.CameraLprTest(ok)
}

func lprCheck(t *testing.T, res gen.CameraLprTest, id string) gen.CameraLprTestCheck {
	t.Helper()
	for _, c := range res.Checks {
		if c.Id == id {
			return c
		}
	}
	t.Fatalf("no check %q in %+v", id, res.Checks)
	return gen.CameraLprTestCheck{}
}

func TestTestCameraLprProvisionPassesAndReportsCounters(t *testing.T) {
	src := newLPRSource("lpr", "provision")
	resp, err := runLPRTest(t, src, nil)
	res := lprOK(t, resp, err)
	if res.Engine != gen.CameraLprTestEngineProvision || !res.Passed {
		t.Fatalf("result = %+v", res)
	}
	if res.Observed.KeepaliveCount == nil || *res.Observed.KeepaliveCount != 12 || res.Observed.LastPlate == nil || *res.Observed.LastPlate != "AB123CD" || res.Observed.LastSeen == nil {
		t.Fatalf("observed = %+v", res.Observed)
	}
	if len(res.Hints) == 0 || !strings.Contains(res.Hints[0], "192.168.1.10:7419/SendKeepalive") {
		t.Fatalf("hints = %v", res.Hints)
	}
	if res.Checks == nil || res.Hints == nil {
		t.Fatal("arrays must not be null")
	}
}

func TestTestCameraLprProvisionFailureIsAnswered200WithTheFailingCheck(t *testing.T) {
	src := newLPRSource("provision_isr", "lpr")
	src.agent.registry = provision.LPRRegistry{Enforce: true}
	res := runOK(t, src)
	if res.Passed || lprCheck(t, res, "registered").Status != gen.Fail {
		t.Fatalf("result = %+v", res)
	}
}

func runOK(t *testing.T, src *fakeLPRSource) gen.CameraLprTest {
	t.Helper()
	resp, err := runLPRTest(t, src, nil)
	return lprOK(t, resp, err)
}

func TestTestCameraLprFrigateEngine(t *testing.T) {
	src := newLPRSource("lpr")
	res := runOK(t, src)
	if res.Engine != gen.CameraLprTestEngineFrigate || !res.Passed {
		t.Fatalf("result = %+v", res)
	}
	if res.Observed.Reads24h == nil || *res.Observed.Reads24h != 3 || res.Observed.LastReadAt == nil {
		t.Fatalf("observed = %+v", res.Observed)
	}
	if src.agent.calls != 0 {
		t.Fatal("a Frigate camera must not call the agent")
	}

	src.enabled = false
	res = runOK(t, src)
	if res.Passed || lprCheck(t, res, "frigate_lpr_enabled").Status != gen.Fail {
		t.Fatalf("result = %+v", res)
	}

	src = newLPRSource("lpr")
	src.server.Capabilities = json.RawMessage(`{"lpr":false}`)
	res = runOK(t, src)
	if res.Passed || lprCheck(t, res, "capability").Status != gen.Fail {
		t.Fatalf("result = %+v", res)
	}

	src = newLPRSource("lpr")
	src.reads = provision.LPRReadsSummary{}
	res = runOK(t, src)
	if !res.Passed || lprCheck(t, res, "reads_recent").Status != gen.Warn {
		t.Fatalf("result = %+v", res)
	}
}

func TestTestCameraLprFrigateLprFlagWithoutTag(t *testing.T) {
	src := newLPRSource()
	src.cam.Lpr = true
	res := runOK(t, src)
	if res.Engine != gen.CameraLprTestEngineFrigate {
		t.Fatalf("engine = %s", res.Engine)
	}
}

func TestTestCameraLprEngineNone(t *testing.T) {
	src := newLPRSource("parking")
	res := runOK(t, src)
	if res.Engine != gen.CameraLprTestEngineNone || !res.Passed || len(res.Checks) != 1 || res.Checks[0].Status != gen.Skip || res.Checks[0].Message != "La cámara no es LPR" {
		t.Fatalf("result = %+v", res)
	}
}

func TestTestCameraLprRejectsWaitOutOfRange(t *testing.T) {
	for _, bad := range []int{-1, 31} {
		src := newLPRSource("provision")
		n := bad
		_, err := runLPRTest(t, src, &n)
		status, code, _ := statusFor(err)
		if status != http.StatusBadRequest || code != "invalid" {
			t.Errorf("wait %d: status %d code %s err %v", bad, status, code, err)
		}
		if src.agent.calls != 0 {
			t.Errorf("wait %d: the agent was called", bad)
		}
	}
	src := newLPRSource("provision")
	ok := 0
	if _, err := runLPRTest(t, src, &ok); err != nil {
		t.Fatalf("wait 0 rejected: %v", err)
	}
}

func TestTestCameraLprErrors(t *testing.T) {
	src := newLPRSource("provision")
	src.camErr = store.ErrNotFound
	_, err := runLPRTest(t, src, nil)
	if status, _, _ := statusFor(err); status != http.StatusNotFound {
		t.Errorf("camera not found: status %d (%v)", status, err)
	}

	src = newLPRSource("provision")
	src.camErr = access.ErrForbidden
	_, err = runLPRTest(t, src, nil)
	if status, _, _ := statusFor(err); status != http.StatusForbidden {
		t.Errorf("camera forbidden: status %d (%v)", status, err)
	}

	src = newLPRSource("provision")
	src.manageErr = access.ErrForbidden
	_, err = runLPRTest(t, src, nil)
	if status, _, _ := statusFor(err); status != http.StatusForbidden {
		t.Errorf("servers.manage missing: status %d (%v)", status, err)
	}
	if src.agent.calls != 0 {
		t.Error("the agent was called without servers.manage")
	}

	h := &Handlers{lprTest: newLPRSource("provision")}
	_, err = h.TestCameraLpr(context.Background(), gen.TestCameraLprRequestObject{CameraId: uuid.New()})
	if !errors.Is(err, errUnauthenticated) {
		t.Errorf("no actor: err = %v", err)
	}
}

func TestTestCameraLprNilBodyMeansNoWait(t *testing.T) {
	src := newLPRSource("provision")
	h := &Handlers{lprTest: src}
	resp, err := h.TestCameraLpr(testActorCtx(), gen.TestCameraLprRequestObject{CameraId: src.cam.ID})
	if res := lprOK(t, resp, err); !res.Passed {
		t.Fatalf("result = %+v", res)
	}
	if src.agent.calls != 1 {
		t.Fatalf("status calls = %d, want 1 (no polling)", src.agent.calls)
	}
}
