package api

import (
	"context"

	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

func toAlarm(a alarms.Alarm) gen.Alarm {
	out := gen.Alarm{
		Id:                 a.ID,
		TenantId:           a.TenantID,
		SiteId:             a.SiteID,
		SiteName:           a.SiteName,
		CameraId:           a.CameraID,
		CameraName:         a.CameraName,
		ServerName:         &a.ServerName,
		EventId:            a.EventID,
		EventSeverity:      a.EventSeverity,
		EventStartTime:     a.EventStartTime,
		EventEndTime:       a.EventEndTime,
		EventLabels:        a.EventLabels,
		EventSubLabels:     a.EventSubLabels,
		Source:             gen.AlarmSource(a.Source),
		Status:             gen.AlarmStatus(a.Status),
		AssignedTo:         a.AssignedTo,
		AssignedToName:     a.AssignedToName,
		AcknowledgedBy:     a.AcknowledgedBy,
		AcknowledgedByName: a.AcknowledgedByName,
		AcknowledgedAt:     a.AcknowledgedAt,
		ResolvedBy:         a.ResolvedBy,
		ResolvedByName:     a.ResolvedByName,
		ResolvedAt:         a.ResolvedAt,
		ClosedBy:           a.ClosedBy,
		ClosedByName:       a.ClosedByName,
		ClosedAt:           a.ClosedAt,
		CreatedAt:          a.CreatedAt,
		UpdatedAt:          a.UpdatedAt,
	}
	if a.VehicleType != "" {
		out.Vehicle = &gen.VehicleAttributes{
			Type: a.VehicleType, TypeConfidence: a.VehicleTypeConfidence,
			Color: a.VehicleColor, ColorConfidence: a.VehicleColorConfidence,
			ColorQuality: gen.VehicleAttributesColorQuality(a.ColorQuality),
		}
		if a.TrailerColor != "" {
			color := a.TrailerColor
			conf := a.TrailerColorConfidence
			out.Vehicle.TrailerColor = &color
			out.Vehicle.TrailerColorConfidence = &conf
		}
	}
	if a.UpperColor != "" {
		out.Person = &gen.PersonAttributes{
			UpperColor: a.UpperColor, UpperConfidence: a.UpperColorConfidence,
			LowerColor: a.LowerColor, LowerConfidence: a.LowerColorConfidence,
			ColorQuality: gen.PersonAttributesColorQuality(a.PersonColorQuality),
		}
	}
	return out
}

func toTransition(t alarms.Transition) gen.AlarmTransition {
	return gen.AlarmTransition{
		Id:         t.ID,
		TenantId:   t.TenantID,
		AlarmId:    t.AlarmID,
		FromStatus: t.FromStatus,
		ToStatus:   t.ToStatus,
		ActorId:    t.ActorID,
		ActorName:  t.ActorName,
		Comment:    t.Comment,
		At:         t.At,
	}
}

func (h *Handlers) ListAlarms(ctx context.Context, r gen.ListAlarmsRequestObject) (gen.ListAlarmsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	p := r.Params
	var status *string
	if p.Status != nil {
		s := string(*p.Status)
		status = &s
	}
	var statusGroup *string
	if p.StatusGroup != nil {
		sg := string(*p.StatusGroup)
		statusGroup = &sg
	}
	f := alarms.Filter{
		Status:      status,
		StatusGroup: statusGroup,
		SiteID:      p.SiteId,
		CameraID:    p.CameraId,
		AssignedTo:  p.AssignedTo,
	}
	if p.Limit != nil {
		f.Limit = *p.Limit
	}

	items, err := h.Alarms.List(ctx, a, f)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Alarm, len(items))
	for i, item := range items {
		out[i] = toAlarm(item)
	}
	return gen.ListAlarms200JSONResponse{Items: out}, nil
}

func (h *Handlers) GetAlarm(ctx context.Context, r gen.GetAlarmRequestObject) (gen.GetAlarmResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	item, err := h.Alarms.Get(ctx, a, r.AlarmId)
	if err != nil {
		return nil, err
	}
	return gen.GetAlarm200JSONResponse(toAlarm(item)), nil
}

func (h *Handlers) AcknowledgeAlarm(ctx context.Context, r gen.AcknowledgeAlarmRequestObject) (gen.AcknowledgeAlarmResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	item, err := h.Alarms.Acknowledge(ctx, a, r.AlarmId)
	if err != nil {
		return nil, err
	}
	return gen.AcknowledgeAlarm200JSONResponse(toAlarm(item)), nil
}

func (h *Handlers) AssignAlarm(ctx context.Context, r gen.AssignAlarmRequestObject) (gen.AssignAlarmResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &alarms.ValidationError{Msg: "missing request body"}
	}
	item, err := h.Alarms.Assign(ctx, a, r.AlarmId, r.Body.UserId)
	if err != nil {
		return nil, err
	}
	return gen.AssignAlarm200JSONResponse(toAlarm(item)), nil
}

func (h *Handlers) InvestigateAlarm(ctx context.Context, r gen.InvestigateAlarmRequestObject) (gen.InvestigateAlarmResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	var comment string
	if r.Body != nil && r.Body.Comment != nil {
		comment = *r.Body.Comment
	}
	item, err := h.Alarms.Investigate(ctx, a, r.AlarmId, comment)
	if err != nil {
		return nil, err
	}
	return gen.InvestigateAlarm200JSONResponse(toAlarm(item)), nil
}

func (h *Handlers) ResolveAlarm(ctx context.Context, r gen.ResolveAlarmRequestObject) (gen.ResolveAlarmResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	item, err := h.Alarms.Resolve(ctx, a, r.AlarmId)
	if err != nil {
		return nil, err
	}
	return gen.ResolveAlarm200JSONResponse(toAlarm(item)), nil
}

func (h *Handlers) CloseAlarm(ctx context.Context, r gen.CloseAlarmRequestObject) (gen.CloseAlarmResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	var comment string
	if r.Body != nil && r.Body.Comment != nil {
		comment = *r.Body.Comment
	}
	item, err := h.Alarms.Close(ctx, a, r.AlarmId, comment)
	if err != nil {
		return nil, err
	}
	return gen.CloseAlarm200JSONResponse(toAlarm(item)), nil
}

func (h *Handlers) AddAlarmComment(ctx context.Context, r gen.AddAlarmCommentRequestObject) (gen.AddAlarmCommentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &alarms.ValidationError{Msg: "missing request body"}
	}
	item, err := h.Alarms.AddComment(ctx, a, r.AlarmId, r.Body.Comment)
	if err != nil {
		return nil, err
	}
	return gen.AddAlarmComment201JSONResponse(toTransition(item)), nil
}

func (h *Handlers) ListAlarmTransitions(ctx context.Context, r gen.ListAlarmTransitionsRequestObject) (gen.ListAlarmTransitionsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.Alarms.ListTransitions(ctx, a, r.AlarmId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.AlarmTransition, len(items))
	for i, item := range items {
		out[i] = toTransition(item)
	}
	return gen.ListAlarmTransitions200JSONResponse(out), nil
}

func (h *Handlers) BulkAlarmAction(ctx context.Context, r gen.BulkAlarmActionRequestObject) (gen.BulkAlarmActionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &alarms.ValidationError{Msg: "missing request body"}
	}
	res, err := h.Alarms.Bulk(ctx, a, r.Body.AlarmIds, string(r.Body.Action))
	if err != nil {
		return nil, err
	}
	return gen.BulkAlarmAction200JSONResponse(gen.AlarmBulkResult{Updated: res.Updated}), nil
}

func (h *Handlers) ListAlarmAssignees(ctx context.Context, r gen.ListAlarmAssigneesRequestObject) (gen.ListAlarmAssigneesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	assignees, err := h.Alarms.AssignableUsers(ctx, a, r.AlarmId)
	if err != nil {
		return nil, err
	}
	items := make([]gen.AlarmAssignee, len(assignees))
	for i, u := range assignees {
		items[i] = gen.AlarmAssignee{
			Id:          u.ID,
			Username:    u.Username,
			DisplayName: u.DisplayName,
		}
	}
	return gen.ListAlarmAssignees200JSONResponse{Items: items}, nil
}
