package api

import (
	"bytes"
	"context"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/clipwatermark"
)

func toClipWatermarkJob(j clipwatermark.Job) gen.ClipWatermarkJob {
	return gen.ClipWatermarkJob{
		Id: j.ID, LprReadId: j.LPRReadID, Status: gen.ClipWatermarkJobStatus(j.Status),
		Error: j.Error, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
}

func (h *Handlers) CreateClipWatermarkJob(ctx context.Context, r gen.CreateClipWatermarkJobRequestObject) (gen.CreateClipWatermarkJobResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	j, err := h.ClipWatermark.CreateJob(ctx, a, r.ReadId)
	if err != nil {
		return nil, err
	}
	return gen.CreateClipWatermarkJob202JSONResponse(toClipWatermarkJob(j)), nil
}

func (h *Handlers) GetClipWatermarkJob(ctx context.Context, r gen.GetClipWatermarkJobRequestObject) (gen.GetClipWatermarkJobResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	j, err := h.ClipWatermark.GetJob(ctx, a, r.ReadId, r.JobId)
	if err != nil {
		return nil, err
	}
	return gen.GetClipWatermarkJob200JSONResponse(toClipWatermarkJob(j)), nil
}

func (h *Handlers) DownloadClipWatermarkJob(ctx context.Context, r gen.DownloadClipWatermarkJobRequestObject) (gen.DownloadClipWatermarkJobResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	data, err := h.ClipWatermark.Download(ctx, a, r.ReadId, r.JobId)
	if err != nil {
		return nil, err
	}
	return gen.DownloadClipWatermarkJob200Videomp4Response{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
}
