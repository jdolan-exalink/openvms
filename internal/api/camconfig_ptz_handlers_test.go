package api

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func TestPtzTourContractDeclaresPermissionAndConflictResponses(t *testing.T) {
	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	collection := spec.Paths.Find("/api/v1/cameras/{cameraId}/ptz/tours")
	item := spec.Paths.Find("/api/v1/cameras/{cameraId}/ptz/tours/{tourId}")
	if collection == nil || collection.Get == nil || collection.Post == nil || item == nil || item.Put == nil || item.Delete == nil {
		t.Fatal("PTZ tour operations are missing from the contract")
	}
	for _, c := range []struct {
		name string
		has  func(int) bool
		want []int
	}{
		{"list", func(s int) bool { return collection.Get.Responses.Status(s) != nil }, []int{401, 403, 404}},
		{"create", func(s int) bool { return collection.Post.Responses.Status(s) != nil }, []int{400, 403, 404, 409}},
		{"update", func(s int) bool { return item.Put.Responses.Status(s) != nil }, []int{400, 403, 404, 409}},
		{"delete", func(s int) bool { return item.Delete.Responses.Status(s) != nil }, []int{403, 404}},
	} {
		for _, s := range c.want {
			if !c.has(s) {
				t.Errorf("%s must declare %d", c.name, s)
			}
		}
	}
}

func TestPtzTourMappingRoundTrips(t *testing.T) {
	name := "Gate"
	repeat := 4
	in := gen.CameraPtzTourInput{Name: "Round", RepeatCount: &repeat, Steps: []gen.CameraPtzTourStep{
		{PresetToken: "1", PresetName: &name, DwellSeconds: 10, Speed: 0.5},
		{PresetToken: "2", DwellSeconds: 0, Speed: 1},
	}}
	got := cameraPtzTourInput(in)
	if got.Name != "Round" || got.RepeatCount != 4 || len(got.Steps) != 2 || got.Steps[0].PresetName != "Gate" || got.Steps[1].PresetName != "" {
		t.Fatalf("input = %+v", got)
	}
	now := time.Now()
	out := toCameraPtzTour(inventory.PTZTour{ID: uuid.New(), CameraID: uuid.New(), Name: "Round", Steps: got.Steps, RepeatCount: 4, CreatedAt: now, UpdatedAt: now})
	if out.Steps[0].PresetName == nil || *out.Steps[0].PresetName != "Gate" || out.Steps[1].PresetName != nil || out.RepeatCount != 4 {
		t.Fatalf("output = %+v", out)
	}
}

type fakePTZ struct {
	mu        sync.Mutex
	cam       db.GetCameraRow
	denied    error
	tours     []inventory.PTZTour
	presets   []map[string]any
	nodes     []map[string]any
	nodesErr  error
	listErr   error
	calls     []onvifdiscover.PTZRequest
	mutations int
	gotos     chan string
}

func newFakePTZ() *fakePTZ {
	return &fakePTZ{
		cam:     db.GetCameraRow{ID: uuid.New(), ServerID: uuid.New()},
		presets: []map[string]any{{"token": "1", "name": "Puerta"}, {"token": "2", "name": "Playa"}},
		nodes:   []map[string]any{{"token": "n", "maximum_presets": 16, "home_supported": true, "fixed_home_position": false, "supports_pan_tilt": true, "supports_zoom": true}},
		gotos:   make(chan string, 32),
	}
}

func (f *fakePTZ) GetCamera(context.Context, authz.Actor, uuid.UUID) (db.GetCameraRow, error) {
	return f.cam, nil
}
func (f *fakePTZ) RequireCameraPTZ(context.Context, authz.Actor, uuid.UUID) error { return f.denied }
func (f *fakePTZ) ListCameraPTZTours(context.Context, authz.Actor, uuid.UUID) ([]inventory.PTZTour, error) {
	return f.tours, nil
}
func (f *fakePTZ) Credentials(context.Context, authz.Actor, db.GetCameraRow) (string, string, string) {
	return "http://192.168.1.50/onvif/device_service", "admin", "pw"
}
func (f *fakePTZ) Mutate(_ context.Context, _ authz.Actor, _ db.GetCameraRow, run func() error) error {
	f.mu.Lock()
	f.mutations++
	f.mu.Unlock()
	return run()
}
func (f *fakePTZ) ExecutePTZ(_ context.Context, _ authz.Actor, _ uuid.UUID, req onvifdiscover.PTZRequest) (map[string]any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()
	switch req.Action {
	case "get_presets":
		if f.listErr != nil {
			return nil, f.listErr
		}
		items := make([]any, 0, len(f.presets))
		for _, p := range f.presets {
			items = append(items, p)
		}
		return map[string]any{"success": true, "presets": items}, nil
	case "get_nodes":
		if f.nodesErr != nil {
			return nil, f.nodesErr
		}
		items := make([]any, 0, len(f.nodes))
		for _, n := range f.nodes {
			items = append(items, n)
		}
		return map[string]any{"success": true, "nodes": items}, nil
	case "set_preset":
		return map[string]any{"success": true, "preset_token": "9"}, nil
	case "goto_preset":
		f.gotos <- req.PresetToken
	}
	return map[string]any{"success": true}, nil
}

func (f *fakePTZ) actions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.Action)
	}
	return out
}

// control mimics the order of ControlCameraOnvifPTZ: the extras first.
func control(t *testing.T, h *Handlers, f *fakePTZ, body gen.CameraOnvifPTZRequest) (gen.ControlCameraOnvifPTZResponseObject, bool, error) {
	t.Helper()
	return h.ptzExtras(writerCtx(), authz.Actor{UserID: uuid.New()}, f.cam, body)
}

func TestPTZPresetAndHomeActionsGoThroughTheAgentWithAudit(t *testing.T) {
	f := newFakePTZ()
	h := &Handlers{camPTZ: f}
	name := " Entrada "
	resp, handled, err := control(t, h, f, gen.CameraOnvifPTZRequest{Action: "set_preset", PresetName: &name})
	ok, isOK := resp.(gen.ControlCameraOnvifPTZ200JSONResponse)
	if err != nil || !handled || !isOK || ok.PresetToken == nil || *ok.PresetToken != "9" {
		t.Fatalf("set_preset: %v %v %+v", err, handled, resp)
	}
	if last := f.calls[len(f.calls)-1]; last.PresetName != "Entrada" || last.Password != "pw" {
		t.Fatalf("agent request: %+v", last)
	}
	tok := "9"
	for _, tc := range []struct {
		action string
		tok    *string
	}{{"remove_preset", &tok}, {"goto_home", nil}, {"set_home", nil}} {
		if _, handled, err := control(t, h, f, gen.CameraOnvifPTZRequest{Action: gen.CameraOnvifPTZRequestAction(tc.action), PresetToken: tc.tok}); err != nil || !handled {
			t.Fatalf("%s: %v %v", tc.action, handled, err)
		}
	}
	if f.mutations != 4 {
		t.Fatalf("every changing action is audited as an ONVIF mutation, got %d", f.mutations)
	}
	resp, _, _ = control(t, h, f, gen.CameraOnvifPTZRequest{Action: "remove_preset"})
	if _, ok := resp.(gen.ControlCameraOnvifPTZ400JSONResponse); !ok {
		t.Fatalf("remove without token: %T", resp)
	}
}

func TestPTZChangingActionsNeedThePTZPermission(t *testing.T) {
	f := newFakePTZ()
	f.denied = access.ErrForbidden
	h := &Handlers{camPTZ: f}
	for _, a := range []string{"set_preset", "remove_preset", "goto_home", "set_home", "continuous_move", "stop", "goto_preset"} {
		tok := "1"
		_, handled, err := control(t, h, f, gen.CameraOnvifPTZRequest{Action: gen.CameraOnvifPTZRequestAction(a), PresetToken: &tok})
		if !errors.Is(err, access.ErrForbidden) || !handled {
			t.Errorf("%s: err %v handled %v", a, err, handled)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("a denied user reached the agent: %v", f.actions())
	}
	// Reading presets and nodes keeps the original rule (no PTZ permission).
	if _, _, err := control(t, h, f, gen.CameraOnvifPTZRequest{Action: "get_presets"}); err != nil {
		t.Fatalf("get_presets: %v", err)
	}
}

func TestPTZGetPresetsCarriesCapabilitiesAndAdmitsWhatItCannotKnow(t *testing.T) {
	f := newFakePTZ()
	h := &Handlers{camPTZ: f}
	resp, _, _ := control(t, h, f, gen.CameraOnvifPTZRequest{Action: "get_presets"})
	out := resp.(gen.ControlCameraOnvifPTZ200JSONResponse)
	if out.Presets == nil || len(*out.Presets) != 2 || out.Capabilities == nil || !out.Capabilities.Known || !out.Capabilities.PresetEdit || !out.Capabilities.HomeSet || out.Capabilities.Focus || out.Capabilities.MaxPresets != 16 {
		t.Fatalf("response: %+v", out)
	}
	f.nodesErr = errors.New("agent returned 400: unknown ptz action")
	resp, _, _ = control(t, h, f, gen.CameraOnvifPTZRequest{Action: "get_presets"})
	out = resp.(gen.ControlCameraOnvifPTZ200JSONResponse)
	if out.Capabilities == nil || out.Capabilities.Known || out.Capabilities.PresetEdit || out.Capabilities.Reason == nil || string(*out.Capabilities.Reason) != "agent_outdated" || out.Presets == nil {
		t.Fatalf("old agent: %+v", out.Capabilities)
	}
	f.nodesErr = errors.New("agent returned 502: ptz get nodes failed")
	resp, _, _ = control(t, h, f, gen.CameraOnvifPTZRequest{Action: "get_presets"})
	if c := resp.(gen.ControlCameraOnvifPTZ200JSONResponse).Capabilities; c == nil || c.Known || string(*c.Reason) != "unavailable" {
		t.Fatalf("unavailable: %+v", c)
	}
	// An old agent rejecting a new action reads as agent_outdated, not as a generic failure.
	f.nodesErr = nil
	old := &oldAgentPTZ{fakePTZ: f}
	h.camPTZ = old
	resp, _, _ = control(t, h, f, gen.CameraOnvifPTZRequest{Action: "set_home"})
	if e, ok := resp.(gen.ControlCameraOnvifPTZ502JSONResponse); !ok || e.Code != "agent_outdated" {
		t.Fatalf("old agent set_home: %+v", resp)
	}
}

type oldAgentPTZ struct{ *fakePTZ }

func (o *oldAgentPTZ) ExecutePTZ(context.Context, authz.Actor, uuid.UUID, onvifdiscover.PTZRequest) (map[string]any, error) {
	return nil, errors.New("agent returned 400: unknown ptz action")
}

func storedTour(tokens ...string) inventory.PTZTour {
	steps := make([]inventory.PTZTourStep, 0, len(tokens))
	for _, t := range tokens {
		steps = append(steps, inventory.PTZTourStep{PresetToken: t, DwellSeconds: 0})
	}
	return inventory.PTZTour{ID: uuid.New(), Name: "Ronda", Steps: steps}
}

func TestTourStartRunsPresetsOnePerCameraAndAManualCommandCancelsIt(t *testing.T) {
	f := newFakePTZ()
	tour := storedTour("1", "2")
	f.tours = []inventory.PTZTour{tour}
	h := &Handlers{camPTZ: f}
	defer h.StopPTZTours()
	req := gen.StartCameraPtzTourRequestObject{CameraId: f.cam.ID, TourId: tour.ID}
	res, err := h.StartCameraPtzTour(writerCtx(), req)
	if st, ok := res.(gen.StartCameraPtzTour200JSONResponse); err != nil || !ok || st.State != "running" {
		t.Fatalf("start: %v %+v", err, res)
	}
	select {
	case tok := <-f.gotos:
		if tok != "1" {
			t.Fatalf("first goto %s", tok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the tour never moved the camera")
	}
	if res, _ := h.StartCameraPtzTour(writerCtx(), req); func() bool { _, ok := res.(gen.StartCameraPtzTour409JSONResponse); return !ok }() {
		t.Fatalf("a second tour on the camera: %T", res)
	} else if res.(gen.StartCameraPtzTour409JSONResponse).Code != "tour_running" {
		t.Fatal("wrong conflict code")
	}
	if s := h.tourRunner().Status(f.cam.ID); s.State != "running" {
		t.Fatalf("status: %+v", s)
	}
	// A user's manual movement cancels the tour.
	if _, handled, err := control(t, h, f, gen.CameraOnvifPTZRequest{Action: "continuous_move"}); err != nil || handled {
		t.Fatalf("continuous_move stays with the original handler: %v %v", handled, err)
	}
	if s := h.tourRunner().Status(f.cam.ID); s.State != "stopped" || s.StopReason != "manual_command" {
		t.Fatalf("status after manual command: %+v", s)
	}
}

func TestTourStartRefusesWithoutPresetsMissingPresetsAndUnavailablePTZ(t *testing.T) {
	tour := storedTour("1", "7")
	for name, tc := range map[string]struct {
		mutate func(*fakePTZ)
		code   string
		status string
	}{
		"no presets":     {func(f *fakePTZ) { f.presets = nil }, "no_presets", "409"},
		"missing preset": {func(f *fakePTZ) {}, "preset_missing", "409"},
		"ptz down":       {func(f *fakePTZ) { f.listErr = errors.New("agent returned 502") }, "ptz_unavailable", "502"},
	} {
		f := newFakePTZ()
		f.tours = []inventory.PTZTour{tour}
		tc.mutate(f)
		h := &Handlers{camPTZ: f}
		res, err := h.StartCameraPtzTour(writerCtx(), gen.StartCameraPtzTourRequestObject{CameraId: f.cam.ID, TourId: tour.ID})
		if err != nil {
			t.Fatal(err)
		}
		switch r := res.(type) {
		case gen.StartCameraPtzTour409JSONResponse:
			if tc.status != "409" || r.Code != tc.code {
				t.Errorf("%s: %+v", name, r)
			}
		case gen.StartCameraPtzTour502JSONResponse:
			if tc.status != "502" || r.Code != tc.code {
				t.Errorf("%s: %+v", name, r)
			}
		default:
			t.Errorf("%s: %T", name, res)
		}
		if s := h.tourRunner().Status(f.cam.ID); s.State != "idle" {
			t.Errorf("%s: a refused tour must not run: %+v", name, s)
		}
		for _, a := range f.actions() {
			if a == "goto_preset" {
				t.Errorf("%s: the camera was moved", name)
			}
		}
	}
}

func TestTourStartAndStopNeedThePTZPermissionAndStopIsIdempotent(t *testing.T) {
	f := newFakePTZ()
	tour := storedTour("1")
	f.tours = []inventory.PTZTour{tour}
	h := &Handlers{camPTZ: f}
	f.denied = access.ErrForbidden
	if _, err := h.StartCameraPtzTour(writerCtx(), gen.StartCameraPtzTourRequestObject{CameraId: f.cam.ID, TourId: tour.ID}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("start: %v", err)
	}
	if _, err := h.StopCameraPtzTour(writerCtx(), gen.StopCameraPtzTourRequestObject{CameraId: f.cam.ID, TourId: tour.ID}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("stop: %v", err)
	}
	f.denied = nil
	res, err := h.StopCameraPtzTour(writerCtx(), gen.StopCameraPtzTourRequestObject{CameraId: f.cam.ID, TourId: tour.ID})
	if st, ok := res.(gen.StopCameraPtzTour200JSONResponse); err != nil || !ok || st.State != "idle" {
		t.Fatalf("stopping an idle camera: %v %+v", err, res)
	}
	if res, _ := h.StartCameraPtzTour(writerCtx(), gen.StartCameraPtzTourRequestObject{CameraId: f.cam.ID, TourId: uuid.New()}); func() bool { _, ok := res.(gen.StartCameraPtzTour404JSONResponse); return !ok }() {
		t.Fatalf("unknown tour: %T", res)
	}
}
