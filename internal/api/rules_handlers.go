package api

import (
	"context"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/rules"
)

func toRule(r rules.Rule) gen.Rule {
	c := r.Conditions
	cond := gen.RuleConditions{}
	if len(c.CameraIDs) > 0 {
		cond.CameraIds = &c.CameraIDs
	}
	if len(c.ServerIDs) > 0 {
		cond.ServerIds = &c.ServerIDs
	}
	if len(c.Labels) > 0 {
		cond.Labels = &c.Labels
	}
	if len(c.Zones) > 0 {
		cond.Zones = &c.Zones
	}
	if len(c.Severities) > 0 {
		cond.Severities = &c.Severities
	}
	if c.DurationSeconds > 0 {
		cond.DurationSeconds = &c.DurationSeconds
	}
	createAlarm, notify := r.Actions.CreateAlarm, r.Actions.NotifyInApp
	act := gen.RuleActions{CreateAlarm: &createAlarm, NotifyInApp: &notify}
	if r.Actions.Severity != "" {
		sev := gen.RuleActionsSeverity(r.Actions.Severity)
		act.Severity = &sev
	}
	return gen.Rule{
		Id:          r.ID,
		TenantId:    r.TenantID,
		Name:        r.Name,
		TriggerType: gen.RuleTriggerType(r.TriggerType),
		Conditions:  cond,
		Actions:     act,
		Enabled:     r.Enabled,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func fromRuleConditions(c *gen.RuleConditions) rules.Conditions {
	var out rules.Conditions
	if c == nil {
		return out
	}
	if c.CameraIds != nil {
		out.CameraIDs = *c.CameraIds
	}
	if c.ServerIds != nil {
		out.ServerIDs = *c.ServerIds
	}
	if c.Labels != nil {
		out.Labels = *c.Labels
	}
	if c.Zones != nil {
		out.Zones = *c.Zones
	}
	if c.Severities != nil {
		out.Severities = *c.Severities
	}
	if c.DurationSeconds != nil {
		out.DurationSeconds = *c.DurationSeconds
	}
	return out
}

func fromRuleActions(a *gen.RuleActions) rules.Actions {
	var out rules.Actions
	if a == nil {
		return out
	}
	if a.CreateAlarm != nil {
		out.CreateAlarm = *a.CreateAlarm
	}
	if a.NotifyInApp != nil {
		out.NotifyInApp = *a.NotifyInApp
	}
	if a.Severity != nil {
		out.Severity = string(*a.Severity)
	}
	return out
}

func toNotification(n rules.Notification) gen.Notification {
	return gen.Notification{
		Id:        n.ID,
		TenantId:  n.TenantID,
		UserId:    n.UserID,
		RuleId:    n.RuleID,
		Title:     n.Title,
		Body:      n.Body,
		Link:      n.Link,
		Severity:  gen.NotificationSeverity(n.Severity),
		ReadAt:    n.ReadAt,
		CreatedAt: n.CreatedAt,
	}
}

func (h *Handlers) ListRules(ctx context.Context, _ gen.ListRulesRequestObject) (gen.ListRulesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.Rules.ListRules(ctx, a)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Rule, len(items))
	for i, item := range items {
		out[i] = toRule(item)
	}
	return gen.ListRules200JSONResponse{Items: out}, nil
}

func (h *Handlers) GetRule(ctx context.Context, r gen.GetRuleRequestObject) (gen.GetRuleResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	item, err := h.Rules.GetRule(ctx, a, r.RuleId)
	if err != nil {
		return nil, err
	}
	return gen.GetRule200JSONResponse(toRule(item)), nil
}

func (h *Handlers) CreateRule(ctx context.Context, r gen.CreateRuleRequestObject) (gen.CreateRuleResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &rules.ValidationError{Msg: "missing request body"}
	}
	enabled := true
	if r.Body.Enabled != nil {
		enabled = *r.Body.Enabled
	}
	item, err := h.Rules.CreateRule(ctx, a, rules.CreateRuleRequest{
		Name:        r.Body.Name,
		TriggerType: rules.TriggerType(r.Body.TriggerType),
		Conditions:  fromRuleConditions(r.Body.Conditions),
		Actions:     fromRuleActions(r.Body.Actions),
		Enabled:     enabled,
	})
	if err != nil {
		return nil, err
	}
	return gen.CreateRule201JSONResponse(toRule(item)), nil
}

func (h *Handlers) UpdateRule(ctx context.Context, r gen.UpdateRuleRequestObject) (gen.UpdateRuleResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &rules.ValidationError{Msg: "missing request body"}
	}
	req := rules.UpdateRuleRequest{Name: r.Body.Name, Enabled: r.Body.Enabled}
	if r.Body.TriggerType != nil {
		t := rules.TriggerType(*r.Body.TriggerType)
		req.TriggerType = &t
	}
	if r.Body.Conditions != nil {
		c := fromRuleConditions(r.Body.Conditions)
		req.Conditions = &c
	}
	if r.Body.Actions != nil {
		act := fromRuleActions(r.Body.Actions)
		req.Actions = &act
	}
	item, err := h.Rules.UpdateRule(ctx, a, r.RuleId, req)
	if err != nil {
		return nil, err
	}
	return gen.UpdateRule200JSONResponse(toRule(item)), nil
}

func (h *Handlers) DeleteRule(ctx context.Context, r gen.DeleteRuleRequestObject) (gen.DeleteRuleResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Rules.DeleteRule(ctx, a, r.RuleId); err != nil {
		return nil, err
	}
	return gen.DeleteRule204Response{}, nil
}

func (h *Handlers) ListNotifications(ctx context.Context, r gen.ListNotificationsRequestObject) (gen.ListNotificationsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	unreadOnly := r.Params.UnreadOnly != nil && *r.Params.UnreadOnly
	limit := 0
	if r.Params.Limit != nil {
		limit = *r.Params.Limit
	}
	items, unread, err := h.Rules.ListNotifications(ctx, a, unreadOnly, limit)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Notification, len(items))
	for i, item := range items {
		out[i] = toNotification(item)
	}
	return gen.ListNotifications200JSONResponse{Items: out, UnreadCount: int(unread)}, nil
}

func (h *Handlers) MarkNotificationRead(ctx context.Context, r gen.MarkNotificationReadRequestObject) (gen.MarkNotificationReadResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	item, err := h.Rules.MarkNotificationRead(ctx, a, r.Id)
	if err != nil {
		return nil, err
	}
	return gen.MarkNotificationRead200JSONResponse(toNotification(item)), nil
}

func (h *Handlers) MarkAllNotificationsRead(ctx context.Context, _ gen.MarkAllNotificationsReadRequestObject) (gen.MarkAllNotificationsReadResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	n, err := h.Rules.MarkAllNotificationsRead(ctx, a)
	if err != nil {
		return nil, err
	}
	return gen.MarkAllNotificationsRead200JSONResponse{Count: int(n)}, nil
}
