package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
)

// perCameraSource serves a different camera per id; ids absent from the map are not visible.
type perCameraSource map[uuid.UUID]inventory.CameraEngine

func (s perCameraSource) OpenCameraEngine(_ context.Context, _ authz.Actor, id uuid.UUID) (inventory.CameraEngine, error) {
	ce, ok := s[id]
	if !ok {
		return inventory.CameraEngine{}, store.ErrNotFound
	}
	return ce, nil
}

// perCameraWriter answers review and apply per camera and records every apply.
type perCameraWriter struct {
	review  map[uuid.UUID]camconfig.Review
	errs    map[uuid.UUID]error
	applied []inventory.ConfigApplyInput
	applyAt []uuid.UUID
	applyEr map[uuid.UUID]error
}

func (w *perCameraWriter) ReviewCameraConfig(_ context.Context, _ authz.Actor, id uuid.UUID, _ camconfig.Patch) (camconfig.Review, error) {
	if err := w.errs[id]; err != nil {
		return camconfig.Review{}, err
	}
	return w.review[id], nil
}

func (w *perCameraWriter) ApplyCameraConfig(_ context.Context, _ authz.Actor, id uuid.UUID, in inventory.ConfigApplyInput) (inventory.ConfigApplyResult, error) {
	w.applied, w.applyAt = append(w.applied, in), append(w.applyAt, id)
	if err := w.applyEr[id]; err != nil {
		return inventory.ConfigApplyResult{}, err
	}
	return inventory.ConfigApplyResult{Outcome: camconfig.Outcome{Result: camconfig.ResultOK}}, nil
}

type bulkFixture struct {
	h                        *Handlers
	w                        *perCameraWriter
	ok, noChange, forbidden  uuid.UUID
	prov, hidden, failsApply uuid.UUID
}

func newBulkFixture(t *testing.T) bulkFixture {
	t.Helper()
	f := bulkFixture{w: &perCameraWriter{review: map[uuid.UUID]camconfig.Review{}, errs: map[uuid.UUID]error{}, applyEr: map[uuid.UUID]error{}}}
	src := perCameraSource{}
	mk := func(name string, prov bool) uuid.UUID {
		id := uuid.New()
		var ce inventory.CameraEngine
		if prov {
			ce = provisionFixture(fakeFrigate{docErr: frigate.ErrNotFound})
		} else {
			ce = camConfigFixture(fakeFrigate{doc: map[string]any{}})
		}
		ce.Camera.ID, ce.Camera.DisplayName = id, name
		src[id] = ce
		return id
	}
	change := camconfig.Review{Changes: []camconfig.Change{{Path: "recording.enabled", Label: "grabación", From: false, To: true}},
		Impact: []camconfig.ImpactItem{{Code: camconfig.ImpactEngineRestart, Message: "x"}}}
	f.ok, f.noChange, f.forbidden = mk("ok", false), mk("same", false), mk("denied", false)
	f.prov, f.failsApply = mk("prov", true), mk("flaky", false)
	f.hidden = uuid.New()
	f.w.review[f.ok], f.w.review[f.forbidden], f.w.review[f.failsApply] = change, change, change
	f.w.errs[f.forbidden] = access.ErrForbidden
	f.w.applyEr[f.failsApply] = errors.New("boom")
	f.h = &Handlers{camConfig: src, camConfigWrite: f.w}
	return f
}

func bulkBody(ids []uuid.UUID, patch map[string]any) *gen.CameraConfigBulkRequest {
	us := make([]openapi_types.UUID, len(ids))
	copy(us, ids)
	return &gen.CameraConfigBulkRequest{CameraIds: &us, Patch: patch}
}

func rowStatus(rep gen.CameraConfigBulkReport) map[string]string {
	out := map[string]string{}
	for _, r := range rep.Rows {
		out[r.CameraId] = string(r.Status)
	}
	return out
}

func TestBulkReviewClassifiesEveryCamera(t *testing.T) {
	f := newBulkFixture(t)
	ids := []uuid.UUID{f.ok, f.noChange, f.forbidden, f.prov, f.hidden}
	res, err := f.h.ReviewCameraConfigBulk(writerCtx(), gen.ReviewCameraConfigBulkRequestObject{Body: bulkBody(ids, map[string]any{"recording": map[string]any{"enabled": true}})})
	if err != nil {
		t.Fatal(err)
	}
	rep := gen.CameraConfigBulkReport(res.(gen.ReviewCameraConfigBulk200JSONResponse))
	got := rowStatus(rep)
	want := map[uuid.UUID]string{f.ok: "will_apply", f.noChange: "skipped", f.forbidden: "skipped", f.prov: "incompatible", f.hidden: "skipped"}
	for id, st := range want {
		if got[id.String()] != st {
			t.Errorf("camera %s = %q, want %q", id, got[id.String()], st)
		}
	}
	if rep.Totals.WillApply != 1 || rep.Totals.Skipped != 3 || rep.Totals.Incompatible != 1 || rep.Restarts != 1 {
		t.Fatalf("totals = %+v restarts %d", rep.Totals, rep.Restarts)
	}
	for _, r := range rep.Rows {
		if r.CameraId == f.hidden.String() && r.Name != "" {
			t.Fatalf("a camera the caller cannot see leaked its name: %q", r.Name)
		}
	}
	if len(f.w.applied) != 0 {
		t.Fatal("review applied something")
	}
}

func TestBulkApplyIsPerCameraAndStopsNothing(t *testing.T) {
	f := newBulkFixture(t)
	ids := []uuid.UUID{f.failsApply, f.ok, f.forbidden}
	patch := map[string]any{"recording": map[string]any{"enabled": true}}
	b := bulkBody(ids, patch)
	res, err := f.h.ApplyCameraConfigBulk(writerCtx(), gen.ApplyCameraConfigBulkRequestObject{Body: b})
	if err != nil {
		t.Fatal(err)
	}
	rep := gen.CameraConfigBulkReport(res.(gen.ApplyCameraConfigBulk200JSONResponse))
	got := rowStatus(rep)
	if got[f.failsApply.String()] != "failed" || got[f.ok.String()] != "applied" || got[f.forbidden.String()] != "skipped" {
		t.Fatalf("statuses = %v", got)
	}
	if len(f.w.applied) != 2 {
		t.Fatalf("applies = %d, want 2 (the failure must not stop the rest)", len(f.w.applied))
	}
	for _, in := range f.w.applied {
		if in.Origin != camconfig.OriginAPI || in.Patch.Recording == nil || in.Patch.Detection != nil || in.Patch.Analytics != nil || in.Patch.Streams != nil {
			t.Fatalf("apply input = %+v: only the selected section may be sent", in)
		}
	}
}

func TestBulkRejectsMoreThanOneSection(t *testing.T) {
	f := newBulkFixture(t)
	patch := map[string]any{"recording": map[string]any{"enabled": true}, "detection": map[string]any{"enabled": true}}
	for name, call := range map[string]func() error{
		"review": func() error {
			_, err := f.h.ReviewCameraConfigBulk(writerCtx(), gen.ReviewCameraConfigBulkRequestObject{Body: bulkBody([]uuid.UUID{f.ok}, patch)})
			return err
		},
		"apply": func() error {
			_, err := f.h.ApplyCameraConfigBulk(writerCtx(), gen.ApplyCameraConfigBulkRequestObject{Body: bulkBody([]uuid.UUID{f.ok}, patch)})
			return err
		},
	} {
		code, _, _ := statusFor(call())
		if code != http.StatusBadRequest {
			t.Errorf("%s with two sections = %d, want 400", name, code)
		}
	}
	if len(f.w.applied) != 0 {
		t.Fatal("a rejected request applied something")
	}
}

func TestBulkCapsCamerasAndRequiresOneSelector(t *testing.T) {
	f := newBulkFixture(t)
	patch := map[string]any{"recording": map[string]any{"enabled": true}}
	many := make([]uuid.UUID, camconfig.MaxBulkCameras+1)
	for i := range many {
		many[i] = uuid.New()
	}
	if code, _, _ := statusFor(errOf(f.h.ReviewCameraConfigBulk(writerCtx(), gen.ReviewCameraConfigBulkRequestObject{Body: bulkBody(many, patch)}))); code != http.StatusBadRequest {
		t.Fatalf("201 cameras = %d, want 400", code)
	}
	gid := uuid.New()
	both := bulkBody([]uuid.UUID{f.ok}, patch)
	both.GroupId = &gid
	if code, _, _ := statusFor(errOf(f.h.ReviewCameraConfigBulk(writerCtx(), gen.ReviewCameraConfigBulkRequestObject{Body: both}))); code != http.StatusBadRequest {
		t.Fatalf("camera_ids with group_id = %d, want 400", code)
	}
	if code, _, _ := statusFor(errOf(f.h.ReviewCameraConfigBulk(writerCtx(), gen.ReviewCameraConfigBulkRequestObject{Body: &gen.CameraConfigBulkRequest{Patch: patch}}))); code != http.StatusBadRequest {
		t.Fatalf("no selector = %d, want 400", code)
	}
}

func errOf(_ any, err error) error { return err }

type groupOnly struct {
	camProfileService
	ids []uuid.UUID
}

func (g groupOnly) GroupCameraIDs(context.Context, authz.Actor, uuid.UUID) ([]uuid.UUID, error) {
	return g.ids, nil
}

func TestBulkByGroupUsesGroupOrigin(t *testing.T) {
	f := newBulkFixture(t)
	f.h.camProfile = groupOnly{ids: []uuid.UUID{f.ok}}
	gid := uuid.New()
	b := &gen.CameraConfigBulkRequest{GroupId: &gid, Patch: map[string]any{"recording": map[string]any{"enabled": true}}}
	if _, err := f.h.ApplyCameraConfigBulk(writerCtx(), gen.ApplyCameraConfigBulkRequestObject{Body: b}); err != nil {
		t.Fatal(err)
	}
	if len(f.w.applied) != 1 || f.w.applied[0].Origin != camconfig.OriginGroup {
		t.Fatalf("applied = %+v", f.w.applied)
	}
}
