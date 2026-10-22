package api

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/visionai"
)

// errVisionAIUnavailable is returned when the Vision AI service is not wired.
var errVisionAIUnavailable = errors.New("vision ai unavailable")

func (h *Handlers) GetVisionAISlots(ctx context.Context, r gen.GetVisionAISlotsRequestObject) (gen.GetVisionAISlotsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if h.VisionAI == nil {
		return nil, errVisionAIUnavailable
	}
	source, layout := "general", "4"
	if r.Params.Source != nil {
		source = string(*r.Params.Source)
	}
	if r.Params.Layout != nil {
		layout = string(*r.Params.Layout)
	}
	var opts []visionai.SlotsOption
	if r.Params.Inactivity != nil {
		opts = append(opts, visionai.WithInactivity(int(*r.Params.Inactivity)))
	}
	if servers := uuids(r.Params.ServerId); len(servers) > 0 {
		opts = append(opts, visionai.WithServers(servers))
	}
	res, err := h.VisionAI.Slots(ctx, a, source, layout, opts...)
	if err != nil {
		return nil, err
	}
	return toVisionAISlots(res), nil
}

func toVisionAISlots(res visionai.Result) gen.GetVisionAISlots200JSONResponse {
	out := gen.GetVisionAISlots200JSONResponse{
		Source: gen.VisionAISource(res.Source), Layout: gen.VisionAILayout(res.Layout),
		GeneratedAt: res.GeneratedAt, PlatesVisible: res.PlatesVisible, Slots: make([]gen.VisionAISlot, 0, len(res.Slots)),
	}
	for _, s := range res.Slots {
		slot := gen.VisionAISlot{Slot: s.SlotIndex, Pinned: s.Pinned}
		if s.CameraID != uuid.Nil {
			id, name, event := s.CameraID, s.CameraName, s.EventID
			object, conf, score := s.Reason.Object, s.Reason.Confidence, s.Score
			assigned := s.AssignedAt
			seconds := max(0, int(res.GeneratedAt.Sub(s.AssignedAt)/time.Second))
			slot.CameraId, slot.CameraName, slot.EventId = &id, &name, &event
			slot.Object, slot.Confidence, slot.Score = &object, &conf, &score
			slot.AssignedAt, slot.DisplaySeconds = &assigned, &seconds
			if !s.DetectedAt.IsZero() {
				detected := s.DetectedAt
				slot.DetectedAt = &detected
			}
			active := s.Active
			slot.Active = &active
			if !s.LastSeenAt.IsZero() {
				seen := s.LastSeenAt
				slot.LastSeenAt = &seen
			}
			slot.Zone, slot.Plate, slot.Alarm = optString(s.Reason.Zone), optString(s.Reason.Plate), optString(s.Reason.Alarm)
			if len(s.Reason.Breakdown) > 0 {
				b := s.Reason.Breakdown
				slot.Breakdown = &b
			}
		}
		out.Slots = append(out.Slots, slot)
	}
	return out
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
