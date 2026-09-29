package api

import (
	"context"

	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

func toAlarm(a alarms.Alarm) gen.Alarm {
	return gen.Alarm{
		Id:                 a.ID,
		TenantId:           a.TenantID,
		SiteId:             a.SiteID,
		SiteName:           a.SiteName,
		CameraId:           a.CameraID,
		CameraName:         a.CameraName,
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
		CreatedAt:          a.CreatedAt,
		UpdatedAt:          a.UpdatedAt,
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
	f := alarms.Filter{
		Status:     status,
		SiteID:     p.SiteId,
		CameraID:   p.CameraId,
		AssignedTo: p.AssignedTo,
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
