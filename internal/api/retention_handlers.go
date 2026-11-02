package api

import (
	"context"
	"math"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/retention"
)

func (h *Handlers) GetRetention(ctx context.Context, _ gen.GetRetentionRequestObject) (gen.GetRetentionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.Retention.Get(ctx, a)
	if err != nil {
		return nil, err
	}
	return gen.GetRetention200JSONResponse{
		Settings:     toRetentionSettings(v.Settings),
		Volume:       gen.RetentionVolume{Measured: v.Volume.Measured, TotalBytes: signedRetentionBytes(v.Volume.TotalBytes), FreeBytes: signedRetentionBytes(v.Volume.FreeBytes), Percent: float32(v.Volume.Percent)},
		FacesPending: gen.RetentionStored{Records: v.FacesPending.Records, Bytes: v.FacesPending.Bytes},
	}, nil
}

func (h *Handlers) UpdateRetention(ctx context.Context, r gen.UpdateRetentionRequestObject) (gen.UpdateRetentionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, retention.ErrInvalid
	}
	saved, err := h.Retention.Update(ctx, a, retention.Settings{
		FaceUnknownDays:          r.Body.FaceUnknownDays,
		NotificationDeliveryDays: r.Body.NotificationDeliveryDays,
		NotificationReadDays:     r.Body.NotificationReadDays,
		PressureCleanup:          r.Body.PressureCleanup,
		PressurePercent:          r.Body.PressurePercent,
	})
	if err != nil {
		return nil, err
	}
	return gen.UpdateRetention200JSONResponse(toRetentionSettings(saved)), nil
}

func (h *Handlers) PreviewRetention(ctx context.Context, r gen.PreviewRetentionRequestObject) (gen.PreviewRetentionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	stored, err := h.Retention.Preview(ctx, a, r.Params.FaceUnknownDays)
	if err != nil {
		return nil, err
	}
	return gen.PreviewRetention200JSONResponse(gen.RetentionStored{Records: stored.Records, Bytes: stored.Bytes}), nil
}

func toRetentionSettings(s retention.Settings) gen.RetentionSettings {
	return gen.RetentionSettings{
		FaceUnknownDays:          s.FaceUnknownDays,
		NotificationDeliveryDays: s.NotificationDeliveryDays,
		NotificationReadDays:     s.NotificationReadDays,
		PressureCleanup:          s.PressureCleanup,
		PressurePercent:          s.PressurePercent,
		UpdatedAt:                s.UpdatedAt,
		UpdatedBy:                s.UpdatedBy,
	}
}

func signedRetentionBytes(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}
