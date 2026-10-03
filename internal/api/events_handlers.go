package api

import (
	"bytes"
	"context"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/events"
)

func uuids(p *[]uuid.UUID) []uuid.UUID {
	if p == nil {
		return nil
	}
	return *p
}

func toEvent(e events.Event) gen.Event {
	out := gen.Event{
		Id: e.ID, TenantId: e.TenantID, SiteId: e.SiteID, SiteName: e.SiteName,
		ServerId: e.ServerID, ServerName: e.ServerName, CameraId: e.CameraID, CameraName: e.CameraName,
		RemoteId: e.RemoteID, Severity: gen.Severity(e.Severity), Labels: e.Labels, SubLabels: e.SubLabels,
		Zones: e.Zones, Plates: e.Plates, StartTime: e.StartTime, EndTime: e.EndTime,
		Reviewed: e.Reviewed, HasThumbnail: e.HasThumbnail,
		HasSnapshot: e.HasSnapshot, HasPreview: e.HasPreview,
	}
	if e.Vehicle != nil || e.Person != nil || e.VehicleJob != "" || e.PersonJob != "" {
		out.Attributes = &gen.EventAttributes{}
	}
	if e.VehicleJob != "" {
		job := gen.EventAttributesVehicleJob(e.VehicleJob)
		out.Attributes.VehicleJob = &job
	}
	if e.PersonJob != "" {
		job := gen.EventAttributesPersonJob(e.PersonJob)
		out.Attributes.PersonJob = &job
	}
	if e.Vehicle != nil {
		out.Attributes.Vehicle = &gen.VehicleAttributes{
			Type: e.Vehicle.Type, TypeConfidence: e.Vehicle.TypeConfidence,
			Color: e.Vehicle.Color, ColorConfidence: e.Vehicle.ColorConfidence,
			ColorQuality: gen.VehicleAttributesColorQuality(e.Vehicle.ColorQuality),
		}
		if e.Vehicle.TrailerColor != "" {
			out.Attributes.Vehicle.TrailerColor = &e.Vehicle.TrailerColor
			conf := e.Vehicle.TrailerColorConfidence
			out.Attributes.Vehicle.TrailerColorConfidence = &conf
		}
	}
	if e.Person != nil {
		out.Attributes.Person = &gen.PersonAttributes{
			UpperColor: e.Person.UpperColor, UpperConfidence: e.Person.UpperConfidence,
			LowerColor: e.Person.LowerColor, LowerConfidence: e.Person.LowerConfidence,
			ColorQuality: gen.PersonAttributesColorQuality(e.Person.ColorQuality),
		}
	}
	return out
}

func nextCursor(c string) *string {
	if c == "" {
		return nil
	}
	return &c
}

func (h *Handlers) ListEvents(ctx context.Context, r gen.ListEventsRequestObject) (gen.ListEventsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	p := r.Params
	f := events.Filter{
		SiteIDs: uuids(p.SiteId), ServerIDs: uuids(p.ServerId), CameraIDs: uuids(p.CameraId),
		CameraGroupIDs: uuids(p.CameraGroupId),
		Plate:          deref(p.Plate), From: p.From, To: p.To, Reviewed: p.Reviewed,
		HasSnapshot: p.HasSnapshot, HasPreview: p.HasPreview,
		Cursor: deref(p.Cursor), Limit: deref(p.Limit),
	}
	if p.VehicleType != nil {
		f.VehicleTypes = *p.VehicleType
	}
	if p.VehicleColor != nil {
		f.VehicleColors = *p.VehicleColor
	}
	if p.Label != nil {
		f.Labels = *p.Label
	}
	if p.Zone != nil {
		f.Zones = *p.Zone
	}
	if p.SubLabel != nil {
		f.SubLabels = *p.SubLabel
	}
	if p.Severity != nil {
		f.Severity = string(*p.Severity)
	}
	page, err := h.Events.ListEvents(ctx, a, f)
	if err != nil {
		return nil, err
	}
	out := gen.ListEvents200JSONResponse{Items: make([]gen.Event, 0, len(page.Items)), NextCursor: nextCursor(page.Next)}
	for _, e := range page.Items {
		out.Items = append(out.Items, toEvent(e))
	}
	return out, nil
}

func (h *Handlers) ListEventSyncStatus(ctx context.Context, _ gen.ListEventSyncStatusRequestObject) (gen.ListEventSyncStatusResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	sts, err := h.Events.ListSyncStatus(ctx, a)
	if err != nil {
		return nil, err
	}
	out := gen.ListEventSyncStatus200JSONResponse{Items: make([]gen.EventSyncStatus, 0, len(sts))}
	for _, s := range sts {
		out.Items = append(out.Items, gen.EventSyncStatus{
			ServerId: s.ServerID, ReviewCursor: s.ReviewCursor, ObjectCursor: s.ObjectCursor,
			LastSuccessAt: s.LastSuccessAt, LastError: s.LastError, EventCount: s.EventCount,
		})
	}
	return out, nil
}

func (h *Handlers) GetEvent(ctx context.Context, r gen.GetEventRequestObject) (gen.GetEventResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	e, err := h.Events.GetEvent(ctx, a, r.EventId)
	if err != nil {
		return nil, err
	}
	return gen.GetEvent200JSONResponse(toEvent(e)), nil
}

func (h *Handlers) ReprocessEvent(ctx context.Context, r gen.ReprocessEventRequestObject) (gen.ReprocessEventResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	e, err := h.Events.Reprocess(ctx, a, r.EventId)
	if err != nil {
		return nil, err
	}
	return gen.ReprocessEvent200JSONResponse(toEvent(e)), nil
}

func (h *Handlers) UpdateEvent(ctx context.Context, r gen.UpdateEventRequestObject) (gen.UpdateEventResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	e, err := h.Events.SetReviewed(ctx, a, r.EventId, r.Body.Reviewed)
	if err != nil {
		return nil, err
	}
	return gen.UpdateEvent200JSONResponse(toEvent(e)), nil
}

func (h *Handlers) ReviewEvents(ctx context.Context, r gen.ReviewEventsRequestObject) (gen.ReviewEventsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	evs, err := h.Events.SetReviewedBulk(ctx, a, r.Body.Ids, r.Body.Reviewed)
	if err != nil {
		return nil, err
	}
	out := gen.ReviewEvents200JSONResponse{Items: make([]gen.Event, 0, len(evs))}
	for _, e := range evs {
		out.Items = append(out.Items, toEvent(e))
	}
	return out, nil
}

func (h *Handlers) GetEventThumbnail(ctx context.Context, r gen.GetEventThumbnailRequestObject) (gen.GetEventThumbnailResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b, ct, err := h.Events.Thumbnail(ctx, a, r.EventId)
	if err != nil {
		return nil, err
	}
	return gen.GetEventThumbnail200ImageResponse{Body: bytes.NewReader(b), ContentType: ct, ContentLength: int64(len(b))}, nil
}

func (h *Handlers) ListPlateReads(ctx context.Context, r gen.ListPlateReadsRequestObject) (gen.ListPlateReadsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	p := r.Params
	page, err := h.Events.ListPlates(ctx, a, events.PlateFilter{
		Plate: deref(p.Plate), Exact: deref(p.Exact), SiteIDs: uuids(p.SiteId), CameraIDs: uuids(p.CameraId),
		CameraGroupIDs: uuids(p.CameraGroupId),
		From:           p.From, To: p.To, Cursor: deref(p.Cursor), Limit: deref(p.Limit),
	})
	if err != nil {
		return nil, err
	}
	out := gen.ListPlateReads200JSONResponse{Items: make([]gen.PlateRead, 0, len(page.Items)), NextCursor: nextCursor(page.Next)}
	for _, pr := range page.Items {
		var score *float32
		if pr.Score != nil {
			score = pr.Score
		}
		item := gen.PlateRead{
			Id: pr.ID, SiteId: pr.SiteID, SiteName: pr.SiteName, ServerId: pr.ServerID, ServerName: pr.ServerName,
			CameraId: pr.CameraID, CameraName: pr.CameraName, Plate: pr.Plate, PlateNormalized: pr.Normalized,
			Score: score, Label: pr.Label, Zones: pr.Zones, SeenAt: pr.SeenAt, EventId: pr.EventID,
		}
		if pr.Vehicle != nil {
			item.Vehicle = &gen.VehicleAttributes{
				Type: pr.Vehicle.Type, TypeConfidence: pr.Vehicle.TypeConfidence,
				Color: pr.Vehicle.Color, ColorConfidence: pr.Vehicle.ColorConfidence,
				ColorQuality: gen.VehicleAttributesColorQuality(pr.Vehicle.ColorQuality),
			}
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
