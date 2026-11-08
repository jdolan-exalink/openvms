package api

import (
	"context"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/faces"
)

func (h *Handlers) GetFaceRecurrence(ctx context.Context, _ gen.GetFaceRecurrenceRequestObject) (gen.GetFaceRecurrenceResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.Faces.GetRecurrence(ctx, a)
	if err != nil {
		return nil, err
	}
	return gen.GetFaceRecurrence200JSONResponse(toFaceRecurrenceSettings(v)), nil
}

func (h *Handlers) UpdateFaceRecurrence(ctx context.Context, r gen.UpdateFaceRecurrenceRequestObject) (gen.UpdateFaceRecurrenceResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &faces.ValidationError{Msg: "a request body is required"}
	}
	saved, err := h.Faces.UpdateRecurrence(ctx, a, faces.ClusterRule{MinAppearances: r.Body.MinAppearances, WindowDays: r.Body.WindowDays})
	if err != nil {
		return nil, err
	}
	return gen.UpdateFaceRecurrence200JSONResponse(toFaceRecurrenceSettings(saved)), nil
}

func toFaceRecurrenceSettings(s faces.RecurrenceSettings) gen.FaceRecurrenceSettings {
	return gen.FaceRecurrenceSettings{
		MinAppearances: s.Rule.MinAppearances,
		WindowDays:     s.Rule.WindowDays,
		UpdatedAt:      s.UpdatedAt,
		UpdatedBy:      s.UpdatedBy,
	}
}
