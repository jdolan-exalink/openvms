package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/onvif"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Stored PTZ tours: a sequence of device presets per camera, and the runner that executes one
// (see camconfig.TourRunner: in memory, one tour per camera, cancelled by any manual movement).

func (h *Handlers) ListCameraPtzTours(ctx context.Context, r gen.ListCameraPtzToursRequestObject) (gen.ListCameraPtzToursResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	tours, err := h.Inv.ListCameraPTZTours(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	out := gen.CameraPtzTourList{Tours: make([]gen.CameraPtzTour, 0, len(tours))}
	for _, t := range tours {
		out.Tours = append(out.Tours, toCameraPtzTour(t))
	}
	st := toGenTourStatus(h.tourRunner().Status(r.CameraId))
	out.Status = &st
	return gen.ListCameraPtzTours200JSONResponse(out), nil
}

func (h *Handlers) CreateCameraPtzTour(ctx context.Context, r gen.CreateCameraPtzTourRequestObject) (gen.CreateCameraPtzTourResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	var input inventory.PTZTourInput
	if r.Body != nil {
		input = cameraPtzTourInput(*r.Body)
	}
	tour, err := h.Inv.CreateCameraPTZTour(ctx, a, r.CameraId, input)
	if err != nil {
		return nil, err
	}
	return gen.CreateCameraPtzTour201JSONResponse(toCameraPtzTour(tour)), nil
}

func (h *Handlers) UpdateCameraPtzTour(ctx context.Context, r gen.UpdateCameraPtzTourRequestObject) (gen.UpdateCameraPtzTourResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	var input inventory.PTZTourInput
	if r.Body != nil {
		input = cameraPtzTourInput(*r.Body)
	}
	tour, err := h.Inv.UpdateCameraPTZTour(ctx, a, r.CameraId, r.TourId, input)
	if err != nil {
		return nil, err
	}
	h.tourRunner().CancelTour(r.CameraId, r.TourId, camconfig.TourStopTourChanged)
	return gen.UpdateCameraPtzTour200JSONResponse(toCameraPtzTour(tour)), nil
}

func (h *Handlers) DeleteCameraPtzTour(ctx context.Context, r gen.DeleteCameraPtzTourRequestObject) (gen.DeleteCameraPtzTourResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Inv.DeleteCameraPTZTour(ctx, a, r.CameraId, r.TourId); err != nil {
		return nil, err
	}
	h.tourRunner().CancelTour(r.CameraId, r.TourId, camconfig.TourStopTourChanged)
	return gen.DeleteCameraPtzTour204Response{}, nil
}

func cameraPtzTourInput(in gen.CameraPtzTourInput) inventory.PTZTourInput {
	out := inventory.PTZTourInput{Name: in.Name, Steps: make([]inventory.PTZTourStep, 0, len(in.Steps))}
	if in.RepeatCount != nil {
		out.RepeatCount = *in.RepeatCount
	}
	for _, s := range in.Steps {
		step := inventory.PTZTourStep{PresetToken: s.PresetToken, DwellSeconds: s.DwellSeconds, Speed: float64(s.Speed)}
		if s.PresetName != nil {
			step.PresetName = *s.PresetName
		}
		out.Steps = append(out.Steps, step)
	}
	return out
}

func toCameraPtzTour(t inventory.PTZTour) gen.CameraPtzTour {
	steps := make([]gen.CameraPtzTourStep, 0, len(t.Steps))
	for _, s := range t.Steps {
		step := gen.CameraPtzTourStep{PresetToken: s.PresetToken, DwellSeconds: s.DwellSeconds, Speed: float32(s.Speed)}
		if s.PresetName != "" {
			name := s.PresetName
			step.PresetName = &name
		}
		steps = append(steps, step)
	}
	return gen.CameraPtzTour{Id: t.ID, CameraId: t.CameraID, Name: t.Name, Steps: steps, RepeatCount: t.RepeatCount, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
}

// ptzBackend is what the PTZ handlers need from the services; tests replace it.
type ptzBackend interface {
	GetCamera(ctx context.Context, a authz.Actor, id uuid.UUID) (db.GetCameraRow, error)
	RequireCameraPTZ(ctx context.Context, a authz.Actor, id uuid.UUID) error
	ListCameraPTZTours(ctx context.Context, a authz.Actor, id uuid.UUID) ([]inventory.PTZTour, error)
	Credentials(ctx context.Context, a authz.Actor, cam db.GetCameraRow) (endpoint, user, pass string)
	ExecutePTZ(ctx context.Context, a authz.Actor, serverID uuid.UUID, req onvifdiscover.PTZRequest) (map[string]any, error)
	// Mutate runs a change to the camera under the ONVIF mutation audit (started and result).
	Mutate(ctx context.Context, a authz.Actor, cam db.GetCameraRow, run func() error) error
}

type ptzReal struct{ h *Handlers }

func (p ptzReal) GetCamera(ctx context.Context, a authz.Actor, id uuid.UUID) (db.GetCameraRow, error) {
	return p.h.Inv.GetCamera(ctx, a, id)
}

func (p ptzReal) RequireCameraPTZ(ctx context.Context, a authz.Actor, id uuid.UUID) error {
	return p.h.Inv.RequireCameraPTZ(ctx, a, id)
}

func (p ptzReal) ListCameraPTZTours(ctx context.Context, a authz.Actor, id uuid.UUID) ([]inventory.PTZTour, error) {
	return p.h.Inv.ListCameraPTZTours(ctx, a, id)
}

func (p ptzReal) Credentials(ctx context.Context, a authz.Actor, cam db.GetCameraRow) (string, string, string) {
	return p.h.resolveCameraOnvifCredentials(ctx, a, cam, nil, nil, nil)
}

func (p ptzReal) ExecutePTZ(ctx context.Context, a authz.Actor, serverID uuid.UUID, req onvifdiscover.PTZRequest) (map[string]any, error) {
	return p.h.Provision.ExecuteOnvifPTZ(ctx, a, serverID, req)
}

func (p ptzReal) Mutate(ctx context.Context, a authz.Actor, cam db.GetCameraRow, run func() error) error {
	return p.h.executeOnvifMutation(ctx, a, cam.ID, cam.ServerID, "ptz", run)
}

func (h *Handlers) ptz() ptzBackend {
	if h.camPTZ != nil {
		return h.camPTZ
	}
	return ptzReal{h}
}

// tourRunner returns the process-wide tour runner, created on first use.
func (h *Handlers) tourRunner() *camconfig.TourRunner {
	h.ptzRunnerOnce.Do(func() { h.ptzRunner = camconfig.NewTourRunner() })
	return h.ptzRunner
}

// StopPTZTours cancels every running PTZ tour; call it on shutdown.
func (h *Handlers) StopPTZTours() { h.tourRunner().Close() }

func toGenTourStatus(s camconfig.TourStatus) gen.CameraPtzTourStatus {
	out := gen.CameraPtzTourStatus{State: gen.CameraPtzTourStatusState(s.State)}
	if s.TourID != uuid.Nil {
		id, name := s.TourID, s.TourName
		out.TourId, out.TourName = &id, &name
		step, round := s.Step, s.Round
		out.Step, out.Round = &step, &round
	}
	if !s.StartedAt.IsZero() {
		t := s.StartedAt
		out.StartedAt = &t
	}
	if !s.EndedAt.IsZero() {
		t := s.EndedAt
		out.EndedAt = &t
	}
	if s.StopReason != "" {
		r := gen.CameraPtzTourStatusStopReason(s.StopReason)
		out.StopReason = &r
	}
	if s.Error != "" {
		e := s.Error
		out.Error = &e
	}
	return out
}

// isPTZActionUnknown tells an agent that predates a PTZ action: it answers 400 "unknown ptz action".
func isPTZActionUnknown(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unknown ptz action")
}

// ptzProfile picks the ONVIF profile a PTZ command goes to: the one the client named, else the
// camera's primary profile when it is known.
func ptzProfile(cam db.GetCameraRow, requested string) string {
	if requested != "" {
		return requested
	}
	primary, _ := camconfig.ProfileTokens(inventory.CameraConfigInventory(cam, ""))
	return primary
}

func ptzConflict(code, message string) gen.StartCameraPtzTour409JSONResponse {
	return gen.StartCameraPtzTour409JSONResponse{ConflictJSONResponse: gen.ConflictJSONResponse{Code: code, Message: message}}
}

// ptzExtras handles the PTZ actions the original handler does not: the preset, home and node
// actions, get_presets with the device capabilities, and the cancel of a running tour by any manual
// movement. handled is false for the actions the original handler still serves.
func (h *Handlers) ptzExtras(ctx context.Context, a authz.Actor, cam db.GetCameraRow, body gen.CameraOnvifPTZRequest) (gen.ControlCameraOnvifPTZResponseObject, bool, error) {
	be := h.ptz()
	action := string(body.Action)
	switch action {
	case "continuous_move", "stop", "goto_preset":
		// Served by the original handler; a user's manual movement ends the running tour first.
		if err := be.RequireCameraPTZ(ctx, a, cam.ID); err != nil {
			return nil, true, err
		}
		h.tourRunner().Cancel(cam.ID, camconfig.TourStopManualCommand)
		return nil, false, nil
	case "get_presets", "get_nodes", "set_preset", "remove_preset", "goto_home", "set_home":
	default:
		return nil, false, nil
	}
	readOnly := action == "get_presets" || action == "get_nodes"
	if !readOnly {
		if err := be.RequireCameraPTZ(ctx, a, cam.ID); err != nil {
			return nil, true, err
		}
	}
	if action == "goto_home" {
		h.tourRunner().Cancel(cam.ID, camconfig.TourStopManualCommand)
	}
	ep, user, pass := be.Credentials(ctx, a, cam)
	if ep == "" {
		return gen.ControlCameraOnvifPTZ400JSONResponse{BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "endpoint_required", Message: "Camera ONVIF endpoint could not be determined"}}, true, nil
	}
	req := onvifdiscover.PTZRequest{Endpoint: ep, Username: user, Password: pass, Action: action}
	if body.ProfileToken != nil {
		req.ProfileToken = *body.ProfileToken
	}
	req.ProfileToken = ptzProfile(cam, req.ProfileToken)
	if body.PresetToken != nil {
		req.PresetToken = *body.PresetToken
	}
	if body.PresetName != nil {
		req.PresetName = strings.TrimSpace(*body.PresetName)
	}
	if action == "remove_preset" && req.PresetToken == "" {
		return gen.ControlCameraOnvifPTZ400JSONResponse{BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "preset_token_required", Message: "preset_token is required"}}, true, nil
	}

	var out map[string]any
	var err error
	if readOnly {
		out, err = be.ExecutePTZ(ctx, a, cam.ServerID, req)
	} else {
		err = be.Mutate(ctx, a, cam, func() error {
			var callErr error
			out, callErr = be.ExecutePTZ(ctx, a, cam.ServerID, req)
			return callErr
		})
	}
	if err != nil {
		if isOnvifAuthorizationError(err) {
			return nil, true, err
		}
		code, msg := "ptz_failed", "The PTZ command failed"
		if isPTZActionUnknown(err) {
			code, msg = "agent_outdated", "The server's agent is too old for this PTZ action; update the agent"
		}
		return gen.ControlCameraOnvifPTZ502JSONResponse{FrigateUnreachableJSONResponse: gen.FrigateUnreachableJSONResponse{Code: code, Message: msg}}, true, nil
	}

	resp := gen.CameraOnvifPTZResult{Success: true}
	if tok, ok := out["preset_token"].(string); ok && tok != "" {
		resp.PresetToken = &tok
	}
	if action == "get_presets" {
		resp.Presets = ptzPresets(out)
	}
	if readOnly {
		caps := h.ptzCapabilities(ctx, a, cam, req, out)
		resp.Capabilities = &caps
	}
	return gen.ControlCameraOnvifPTZ200JSONResponse(resp), true, nil
}

func ptzPresets(out map[string]any) *[]struct {
	Name  *string `json:"name,omitempty"`
	Token *string `json:"token,omitempty"`
} {
	list, ok := out["presets"].([]any)
	if !ok || len(list) == 0 {
		return nil
	}
	presets := make([]struct {
		Name  *string `json:"name,omitempty"`
		Token *string `json:"token,omitempty"`
	}, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			token, _ := m["token"].(string)
			name, _ := m["name"].(string)
			presets = append(presets, struct {
				Name  *string `json:"name,omitempty"`
				Token *string `json:"token,omitempty"`
			}{Name: &name, Token: &token})
		}
	}
	return &presets
}

// ptzCapabilities reads what the device supports. get_nodes already carries it; for get_presets one
// more agent call reads the nodes. A failure never fails the original request: the capabilities say
// they are unknown and why.
func (h *Handlers) ptzCapabilities(ctx context.Context, a authz.Actor, cam db.GetCameraRow, req onvifdiscover.PTZRequest, out map[string]any) gen.CameraPtzCapabilities {
	nodes := out
	if req.Action != "get_nodes" {
		req.Action = "get_nodes"
		var err error
		nodes, err = h.ptz().ExecutePTZ(ctx, a, cam.ServerID, req)
		if err != nil {
			reason := camconfig.PTZReasonUnavailable
			if isPTZActionUnknown(err) {
				reason = camconfig.PTZReasonAgentOutdated
			}
			return toGenPTZCapabilities(camconfig.PTZCapabilitiesUnknown(reason))
		}
	}
	var parsed []onvif.PTZNode
	if raw, err := json.Marshal(nodes["nodes"]); err == nil {
		_ = json.Unmarshal(raw, &parsed)
	}
	return toGenPTZCapabilities(camconfig.PTZCapabilitiesFromNodes(parsed))
}

func toGenPTZCapabilities(c camconfig.PTZCapabilities) gen.CameraPtzCapabilities {
	out := gen.CameraPtzCapabilities{Known: c.Known, PresetEdit: c.PresetEdit, MaxPresets: c.MaxPresets, Home: c.Home, HomeSet: c.HomeSet, PanTilt: c.PanTilt, Zoom: c.Zoom, Focus: c.Focus}
	if c.Reason != "" {
		r := gen.CameraPtzCapabilitiesReason(c.Reason)
		out.Reason = &r
	}
	return out
}

func tourFromStored(t inventory.PTZTour) camconfig.Tour {
	steps := make([]camconfig.TourStep, 0, len(t.Steps))
	for _, s := range t.Steps {
		steps = append(steps, camconfig.TourStep{PresetToken: s.PresetToken, Dwell: time.Duration(s.DwellSeconds) * time.Second, Speed: s.Speed})
	}
	return camconfig.Tour{ID: t.ID, Name: t.Name, Steps: steps, Repeat: t.RepeatCount}
}

// StartCameraPtzTour runs a stored tour. It refuses, with a clear code, when the camera already
// runs one, has no presets, lacks a preset the tour uses, or its PTZ does not answer.
func (h *Handlers) StartCameraPtzTour(ctx context.Context, r gen.StartCameraPtzTourRequestObject) (gen.StartCameraPtzTourResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	be := h.ptz()
	cam, err := be.GetCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	if err := be.RequireCameraPTZ(ctx, a, cam.ID); err != nil {
		return nil, err
	}
	tours, err := be.ListCameraPTZTours(ctx, a, cam.ID)
	if err != nil {
		return nil, err
	}
	var stored *inventory.PTZTour
	for i := range tours {
		if tours[i].ID == r.TourId {
			stored = &tours[i]
		}
	}
	if stored == nil {
		return gen.StartCameraPtzTour404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{Code: "not_found", Message: "PTZ tour not found"}}, nil
	}
	if st := h.tourRunner().Status(cam.ID); st.State == camconfig.TourRunning {
		return ptzConflict("tour_running", "The camera already runs a PTZ tour; stop it first"), nil
	}
	ep, user, pass := be.Credentials(ctx, a, cam)
	if ep == "" {
		return gen.StartCameraPtzTour502JSONResponse{FrigateUnreachableJSONResponse: gen.FrigateUnreachableJSONResponse{Code: "ptz_unavailable", Message: "Camera ONVIF endpoint could not be determined"}}, nil
	}
	profile := ptzProfile(cam, "")
	base := onvifdiscover.PTZRequest{Endpoint: ep, Username: user, Password: pass, ProfileToken: profile}
	list := base
	list.Action = "get_presets"
	out, err := be.ExecutePTZ(ctx, a, cam.ServerID, list)
	if err != nil {
		if isOnvifAuthorizationError(err) {
			return nil, err
		}
		return gen.StartCameraPtzTour502JSONResponse{FrigateUnreachableJSONResponse: gen.FrigateUnreachableJSONResponse{Code: "ptz_unavailable", Message: "The camera PTZ did not answer"}}, nil
	}
	have := map[string]bool{}
	if p := ptzPresets(out); p != nil {
		for _, item := range *p {
			if item.Token != nil {
				have[*item.Token] = true
			}
		}
	}
	if len(have) == 0 {
		return ptzConflict("no_presets", "The camera has no presets, so a tour cannot run"), nil
	}
	for _, s := range stored.Steps {
		if !have[s.PresetToken] {
			return ptzConflict("preset_missing", "A preset of the tour no longer exists on the camera: "+s.PresetToken), nil
		}
	}
	runCtx := context.WithoutCancel(ctx)
	move := func(mctx context.Context, step camconfig.TourStep) error {
		req := base
		req.Action, req.PresetToken = "goto_preset", step.PresetToken
		_, err := be.ExecutePTZ(runCtx, a, cam.ServerID, req)
		if err != nil && mctx.Err() != nil {
			return mctx.Err()
		}
		return err
	}
	var status camconfig.TourStatus
	err = be.Mutate(ctx, a, cam, func() error {
		var startErr error
		status, startErr = h.tourRunner().Start(cam.ID, tourFromStored(*stored), move)
		return startErr
	})
	switch {
	case errors.Is(err, camconfig.ErrTourRunning):
		return ptzConflict("tour_running", "The camera already runs a PTZ tour; stop it first"), nil
	case err != nil && isOnvifAuthorizationError(err):
		return nil, err
	case err != nil:
		return gen.StartCameraPtzTour502JSONResponse{FrigateUnreachableJSONResponse: gen.FrigateUnreachableJSONResponse{Code: "tour_start_failed", Message: "The tour could not be started"}}, nil
	}
	return gen.StartCameraPtzTour200JSONResponse(toGenTourStatus(status)), nil
}

// StopCameraPtzTour stops the camera's running tour; stopping one that is not running is not an error.
func (h *Handlers) StopCameraPtzTour(ctx context.Context, r gen.StopCameraPtzTourRequestObject) (gen.StopCameraPtzTourResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	be := h.ptz()
	cam, err := be.GetCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	if err := be.RequireCameraPTZ(ctx, a, cam.ID); err != nil {
		return nil, err
	}
	h.tourRunner().CancelTour(cam.ID, r.TourId, camconfig.TourStopUser)
	return gen.StopCameraPtzTour200JSONResponse(toGenTourStatus(h.tourRunner().Status(cam.ID))), nil
}
