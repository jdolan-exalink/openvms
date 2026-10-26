package api

import (
	"context"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

// Stored PTZ tours: a sequence of device presets per camera. The API persists the sequence only;
// running it is not part of this surface.

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
