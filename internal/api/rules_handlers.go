package api

import (
	"context"
	"strconv"
	"strings"

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
	if len(c.SiteIDs) > 0 {
		cond.SiteIds = &c.SiteIDs
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
	if len(c.VehicleTypes) > 0 {
		cond.VehicleTypes = &c.VehicleTypes
	}
	if len(c.VehicleColors) > 0 {
		cond.VehicleColors = &c.VehicleColors
	}
	if c.DurationSeconds > 0 {
		cond.DurationSeconds = &c.DurationSeconds
	}
	if len(c.CameraGroupIDs) > 0 {
		cond.CameraGroupIds = &c.CameraGroupIDs
	}
	if len(c.MapZoneIDs) > 0 {
		cond.MapZoneIds = &c.MapZoneIDs
	}
	if c.CooldownSeconds != nil {
		v := *c.CooldownSeconds
		cond.CooldownSeconds = &v
	}
	if c.Schedule != nil {
		sc := gen.RuleSchedule{}
		if len(c.Schedule.DaysOfWeek) > 0 {
			sc.DaysOfWeek = &c.Schedule.DaysOfWeek
		}
		if c.Schedule.StartTime != "" {
			sc.StartTime = &c.Schedule.StartTime
		}
		if c.Schedule.EndTime != "" {
			sc.EndTime = &c.Schedule.EndTime
		}
		if c.Schedule.Timezone != "" {
			sc.Timezone = &c.Schedule.Timezone
		}
		cond.Schedule = &sc
	}
	createAlarm, notify := r.Actions.CreateAlarm, r.Actions.NotifyInApp
	act := gen.RuleActions{CreateAlarm: &createAlarm, NotifyInApp: &notify}
	if len(r.Actions.ChannelIDs) > 0 {
		act.ChannelIds = &r.Actions.ChannelIDs
	}
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
		Version:     r.Version,
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
	if c.SiteIds != nil {
		out.SiteIDs = *c.SiteIds
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
	if c.VehicleTypes != nil {
		out.VehicleTypes = *c.VehicleTypes
	}
	if c.VehicleColors != nil {
		out.VehicleColors = *c.VehicleColors
	}
	if c.DurationSeconds != nil {
		out.DurationSeconds = *c.DurationSeconds
	}
	if c.CameraGroupIds != nil {
		out.CameraGroupIDs = *c.CameraGroupIds
	}
	if c.MapZoneIds != nil {
		out.MapZoneIDs = *c.MapZoneIds
	}
	if c.CooldownSeconds != nil {
		v := *c.CooldownSeconds
		out.CooldownSeconds = &v
	}
	if c.Schedule != nil {
		sc := &rules.RuleSchedule{}
		if c.Schedule.DaysOfWeek != nil {
			sc.DaysOfWeek = *c.Schedule.DaysOfWeek
		}
		if c.Schedule.StartTime != nil {
			sc.StartTime = *c.Schedule.StartTime
		}
		if c.Schedule.EndTime != nil {
			sc.EndTime = *c.Schedule.EndTime
		}
		if c.Schedule.Timezone != nil {
			sc.Timezone = *c.Schedule.Timezone
		}
		out.Schedule = sc
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
	if a.ChannelIds != nil {
		out.ChannelIDs = *a.ChannelIds
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
	version, err := parseRuleVersion(r.Params.IfMatch)
	if err != nil {
		return nil, err
	}
	req := rules.UpdateRuleRequest{ExpectedVersion: version, Name: r.Body.Name, Enabled: r.Body.Enabled}
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

// parseRuleVersion reads the rule version from an If-Match value: a plain or quoted integer.
// An absent or blank header returns 0, meaning "no version check".
func parseRuleVersion(ifMatch *string) (int, error) {
	if ifMatch == nil || strings.TrimSpace(*ifMatch) == "" {
		return 0, nil
	}
	raw := strings.TrimPrefix(strings.TrimSpace(*ifMatch), "W/")
	raw = strings.Trim(raw, `"`)
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 {
		return 0, &rules.ValidationError{Msg: "If-Match must carry the current rule version (a positive integer)"}
	}
	return v, nil
}
