package api

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type fakeRestart struct {
	cam      db.GetCameraRow
	denied   error
	agent    bool
	viaErr   error
	direct   int
	viaAgent int
	audits   []string
	sent     onvifdiscover.RebootRequest
	auditErr error
}

func newFakeRestart() *fakeRestart {
	return &fakeRestart{cam: db.GetCameraRow{ID: uuid.New(), ServerID: uuid.New()}, agent: true}
}

func (f *fakeRestart) GetCamera(context.Context, authz.Actor, uuid.UUID) (db.GetCameraRow, error) {
	return f.cam, nil
}
func (f *fakeRestart) RequireCameraRestart(context.Context, authz.Actor, uuid.UUID) error {
	return f.denied
}
func (f *fakeRestart) AuditCameraRestart(_ context.Context, _ authz.Actor, _, _ uuid.UUID, status, via string) error {
	if f.auditErr != nil && status == "started" {
		return f.auditErr
	}
	f.audits = append(f.audits, status+"/"+via)
	return nil
}
func (f *fakeRestart) Credentials(context.Context, db.GetCameraRow) (string, string, string) {
	return "http://192.168.1.50/onvif/device_service", "admin", "pw"
}
func (f *fakeRestart) HasRegisteredAgent(context.Context, uuid.UUID) bool { return f.agent }
func (f *fakeRestart) RebootViaAgent(_ context.Context, _ uuid.UUID, req onvifdiscover.RebootRequest) error {
	f.viaAgent++
	f.sent = req
	return f.viaErr
}
func (f *fakeRestart) RebootDirect(_ context.Context, req onvifdiscover.RebootRequest) error {
	f.direct++
	f.sent = req
	return f.viaErr
}

func restartReq(f *fakeRestart, confirm bool) gen.RestartCameraRequestObject {
	return gen.RestartCameraRequestObject{CameraId: f.cam.ID, Body: &gen.CameraRestartRequest{Confirm: confirm}}
}

func TestRestartRefusesWithoutConfirmAndWithoutPermission(t *testing.T) {
	f := newFakeRestart()
	h := &Handlers{camRestart: f}
	res, err := h.RestartCamera(writerCtx(), restartReq(f, false))
	if r, ok := res.(gen.RestartCamera400JSONResponse); err != nil || !ok || r.Code != "confirmation_required" {
		t.Fatalf("no confirm: %v %+v", err, res)
	}
	if f.viaAgent+f.direct != 0 || len(f.audits) != 0 {
		t.Fatal("an unconfirmed restart touched the camera or the audit")
	}
	res, _ = h.RestartCamera(writerCtx(), gen.RestartCameraRequestObject{CameraId: f.cam.ID})
	if _, ok := res.(gen.RestartCamera400JSONResponse); !ok {
		t.Fatalf("no body: %T", res)
	}
	// The permission is checked before the confirmation: a user without it learns nothing else.
	f.denied = access.ErrForbidden
	if _, err := h.RestartCamera(writerCtx(), restartReq(f, true)); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("denied: %v", err)
	}
	if _, err := h.RestartCamera(writerCtx(), restartReq(f, false)); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("denied without confirm: %v", err)
	}
	if f.viaAgent+f.direct != 0 || len(f.audits) != 0 {
		t.Fatal("a denied restart touched the camera or the audit")
	}
}

func TestRestartUsesTheAgentAuditsAndFallsBackToTheDirectDial(t *testing.T) {
	f := newFakeRestart()
	h := &Handlers{camRestart: f}
	res, err := h.RestartCamera(writerCtx(), restartReq(f, true))
	if r, ok := res.(gen.RestartCamera200JSONResponse); err != nil || !ok || !r.Restarted || r.Via != "agent" {
		t.Fatalf("agent: %v %+v", err, res)
	}
	if f.viaAgent != 1 || f.direct != 0 || f.sent.Username != "admin" || len(f.audits) != 2 || f.audits[0] != "started/agent" || f.audits[1] != "succeeded/agent" {
		t.Fatalf("calls agent=%d direct=%d audits=%v", f.viaAgent, f.direct, f.audits)
	}
	f.agent, f.audits = false, nil
	res, _ = h.RestartCamera(writerCtx(), restartReq(f, true))
	if r, ok := res.(gen.RestartCamera200JSONResponse); !ok || r.Via != "direct" || f.direct != 1 || f.audits[1] != "succeeded/direct" {
		t.Fatalf("direct: %+v audits %v", res, f.audits)
	}
}

func TestRestartFailuresAreAuditedAndNeverCarryCredentials(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		audit  string
		status string
	}{
		"camera refused": {provision.ErrAgentProbeFailed, "failed/agent", "502"},
		"unreachable":    {provision.ErrAgentProbeUnavailable, "outcome_unknown/agent", "502"},
		"old agent":      {provision.ErrAgentOpUnsupported, "failed/agent", "409"},
	} {
		f := newFakeRestart()
		f.viaErr = tc.err
		h := &Handlers{camRestart: f}
		res, err := h.RestartCamera(writerCtx(), restartReq(f, true))
		if err != nil {
			t.Fatal(err)
		}
		switch r := res.(type) {
		case gen.RestartCamera502JSONResponse:
			if tc.status != "502" || r.Code != "restart_failed" || r.Message == "" {
				t.Errorf("%s: %+v", name, r)
			}
		case gen.RestartCamera409JSONResponse:
			if tc.status != "409" || r.Code != "agent_outdated" {
				t.Errorf("%s: %+v", name, r)
			}
		default:
			t.Errorf("%s: %T", name, res)
		}
		if len(f.audits) != 2 || f.audits[1] != tc.audit {
			t.Errorf("%s: audits %v", name, f.audits)
		}
	}
}

func TestRestartDoesNotActWhenTheAttemptCannotBeAudited(t *testing.T) {
	f := newFakeRestart()
	f.auditErr = errors.New("audit down")
	h := &Handlers{camRestart: f}
	if _, err := h.RestartCamera(writerCtx(), restartReq(f, true)); err == nil {
		t.Fatal("want the audit error")
	}
	if f.viaAgent+f.direct != 0 {
		t.Fatal("the camera was restarted without an audit record")
	}
}

func TestRestartContractDeclaresItsResponses(t *testing.T) {
	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	p := spec.Paths.Find("/api/v1/cameras/{cameraId}/restart")
	if p == nil || p.Post == nil {
		t.Fatal("restart operation missing")
	}
	for _, s := range []int{200, 400, 401, 403, 404, 409, 502} {
		if p.Post.Responses.Status(s) == nil {
			t.Errorf("missing %d", s)
		}
	}
}
