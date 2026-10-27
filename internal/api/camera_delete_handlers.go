package api

import (
	"context"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

func (h *Handlers) DeleteCamera(ctx context.Context, r gen.DeleteCameraRequestObject) (gen.DeleteCameraResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Inv.RemoveCamera(ctx, a, r.CameraId); err != nil {
		return nil, err
	}
	return gen.DeleteCamera204Response{}, nil
}

func (h *Handlers) RestoreCamera(ctx context.Context, r gen.RestoreCameraRequestObject) (gen.RestoreCameraResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	cam, err := h.Inv.ReinstateCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	return gen.RestoreCamera200JSONResponse(toCamera(cam)), nil
}

func (h *Handlers) ListDeletedCameras(ctx context.Context, r gen.ListDeletedCamerasRequestObject) (gen.ListDeletedCamerasResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := h.Inv.ListDeletedCameras(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}
	items := make([]gen.DeletedCamera, 0, len(rows))
	for _, d := range rows {
		item := gen.DeletedCamera{
			Id: d.ID, ServerId: d.ServerID, RemoteName: d.RemoteName, DisplayName: d.DisplayName,
			DeletedAt: d.DeletedAt, HasFrigateConfig: d.HasFrigateConfig,
		}
		if d.RetainDays != nil {
			days := int(*d.RetainDays)
			item.RetainDays = &days
		}
		items = append(items, item)
	}
	return gen.ListDeletedCameras200JSONResponse{Items: items}, nil
}
