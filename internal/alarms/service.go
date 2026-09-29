package alarms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

var (
	ErrInvalidTransition = errors.New("invalid alarm status transition")
)

type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

const (
	ActionAlarmAcknowledged = "ALARM_ACKNOWLEDGED"
	ActionAlarmAssigned     = "ALARM_ASSIGNED"
	ActionAlarmResolved     = "ALARM_RESOLVED"
)

type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

type Service struct {
	Store *store.Store
	Pub   Publisher
	Log   *slog.Logger
}

type Alarm struct {
	ID                 uuid.UUID  `json:"id"`
	TenantID           uuid.UUID  `json:"tenant_id"`
	SiteID             uuid.UUID  `json:"site_id"`
	SiteName           string     `json:"site_name"`
	CameraID           uuid.UUID  `json:"camera_id"`
	CameraName         string     `json:"camera_name"`
	EventID            uuid.UUID  `json:"event_id"`
	EventSeverity      string     `json:"event_severity"`
	EventStartTime     time.Time  `json:"event_start_time"`
	EventEndTime       *time.Time `json:"event_end_time,omitempty"`
	EventLabels        []string   `json:"event_labels"`
	EventSubLabels     []string   `json:"event_sub_labels"`
	Source             string     `json:"source"`
	Status             string     `json:"status"`
	AssignedTo         *uuid.UUID `json:"assigned_to,omitempty"`
	AssignedToName     *string    `json:"assigned_to_name,omitempty"`
	AcknowledgedBy     *uuid.UUID `json:"acknowledged_by,omitempty"`
	AcknowledgedByName *string    `json:"acknowledged_by_name,omitempty"`
	AcknowledgedAt     *time.Time `json:"acknowledged_at,omitempty"`
	ResolvedBy         *uuid.UUID `json:"resolved_by,omitempty"`
	ResolvedByName     *string    `json:"resolved_by_name,omitempty"`
	ResolvedAt         *time.Time `json:"resolved_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type Assignee struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
}

type Filter struct {
	Status     *string
	SiteID     *uuid.UUID
	CameraID   *uuid.UUID
	AssignedTo *uuid.UUID
	Limit      int
}

type BulkResult struct {
	Updated int
}

func (s *Service) List(ctx context.Context, actor authz.Actor, f Filter) ([]Alarm, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	var items []Alarm
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		cameraIDs, err := chk.CameraIDs(ctx, authz.AlarmsView)
		if err != nil {
			return err
		}
		if len(cameraIDs) == 0 {
			items = []Alarm{}
			return nil
		}

		if f.CameraID != nil {
			has := false
			for _, cid := range cameraIDs {
				if cid == *f.CameraID {
					has = true
					break
				}
			}
			if !has {
				items = []Alarm{}
				return nil
			}
			cameraIDs = []uuid.UUID{*f.CameraID}
		}

		rows, err := q.ListAlarms(ctx, db.ListAlarmsParams{
			CameraIds:  cameraIDs,
			Status:     f.Status,
			SiteID:     f.SiteID,
			CameraID:   f.CameraID,
			AssignedTo: f.AssignedTo,
			LimitCount: int32(limit),
		})
		if err != nil {
			return store.Classify(err)
		}

		items = make([]Alarm, len(rows))
		for i, r := range rows {
			items[i] = mapListAlarmRow(r)
		}
		return nil
	})
	return items, err
}

func (s *Service) Get(ctx context.Context, actor authz.Actor, id uuid.UUID) (Alarm, error) {
	var res Alarm
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		row, err := q.GetAlarm(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}

		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.AlarmsView, access.Camera(row.TenantID, row.SiteID, uuid.Nil, row.CameraID, nil)); err != nil {
			return err
		}

		res = mapGetAlarmRow(row)
		return nil
	})
	return res, err
}

func (s *Service) Acknowledge(ctx context.Context, actor authz.Actor, id uuid.UUID) (Alarm, error) {
	var res Alarm
	var published bool
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		row, err := q.GetAlarm(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}

		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.AlarmsManage, access.Camera(row.TenantID, row.SiteID, uuid.Nil, row.CameraID, nil)); err != nil {
			return err
		}

		if row.Status == "resolved" {
			return ErrInvalidTransition
		}
		if row.Status == "acknowledged" {
			res = mapGetAlarmRow(row)
			return nil
		}

		if _, err := q.UpdateAlarmStatus(ctx, db.UpdateAlarmStatusParams{
			ID:      id,
			Status:  "acknowledged",
			ActorID: &actor.UserID,
		}); err != nil {
			return store.Classify(err)
		}

		if err := audit(ctx, q, actor, &row.TenantID, ActionAlarmAcknowledged, id, map[string]any{
			"previous_status": row.Status,
		}); err != nil {
			return err
		}

		updatedRow, err := q.GetAlarm(ctx, id)
		if err != nil {
			return store.Classify(err)
		}
		res = mapGetAlarmRow(updatedRow)
		published = true
		return nil
	})
	if err == nil && published {
		s.publish(ctx, "acknowledged", res)
	}
	return res, err
}

func (s *Service) Assign(ctx context.Context, actor authz.Actor, id uuid.UUID, assigneeID uuid.UUID) (Alarm, error) {
	var res Alarm
	var published bool
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		row, err := q.GetAlarm(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}

		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.AlarmsManage, access.Camera(row.TenantID, row.SiteID, uuid.Nil, row.CameraID, nil)); err != nil {
			return err
		}

		if row.Status == "resolved" {
			return ErrInvalidTransition
		}

		eligible, err := q.UsersWithCameraPermission(ctx, db.UsersWithCameraPermissionParams{
			CameraID:   row.CameraID,
			Permission: string(authz.AlarmsManage),
		})
		if err != nil {
			return store.Classify(err)
		}
		found := false
		for _, u := range eligible {
			if u.ID == assigneeID {
				found = true
				break
			}
		}
		if !found {
			return &ValidationError{Msg: "assigned user does not hold alarms.manage on this camera"}
		}

		if _, err := q.AssignAlarm(ctx, db.AssignAlarmParams{
			ID:         id,
			AssignedTo: &assigneeID,
		}); err != nil {
			return store.Classify(err)
		}

		if err := audit(ctx, q, actor, &row.TenantID, ActionAlarmAssigned, id, map[string]any{
			"assigned_to": assigneeID,
		}); err != nil {
			return err
		}

		updatedRow, err := q.GetAlarm(ctx, id)
		if err != nil {
			return store.Classify(err)
		}
		res = mapGetAlarmRow(updatedRow)
		published = true
		return nil
	})
	if err == nil && published {
		s.publish(ctx, "assigned", res)
	}
	return res, err
}

func (s *Service) Resolve(ctx context.Context, actor authz.Actor, id uuid.UUID) (Alarm, error) {
	var res Alarm
	var published bool
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		row, err := q.GetAlarm(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}

		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.AlarmsManage, access.Camera(row.TenantID, row.SiteID, uuid.Nil, row.CameraID, nil)); err != nil {
			return err
		}

		if row.Status == "resolved" {
			res = mapGetAlarmRow(row)
			return nil
		}

		if _, err := q.UpdateAlarmStatus(ctx, db.UpdateAlarmStatusParams{
			ID:      id,
			Status:  "resolved",
			ActorID: &actor.UserID,
		}); err != nil {
			return store.Classify(err)
		}

		if err := audit(ctx, q, actor, &row.TenantID, ActionAlarmResolved, id, map[string]any{
			"previous_status": row.Status,
		}); err != nil {
			return err
		}

		updatedRow, err := q.GetAlarm(ctx, id)
		if err != nil {
			return store.Classify(err)
		}
		res = mapGetAlarmRow(updatedRow)
		published = true
		return nil
	})
	if err == nil && published {
		s.publish(ctx, "resolved", res)
	}
	return res, err
}

func (s *Service) Bulk(ctx context.Context, actor authz.Actor, ids []uuid.UUID, action string) (BulkResult, error) {
	if action != "acknowledge" && action != "resolve" {
		return BulkResult{}, &ValidationError{Msg: "action must be acknowledge or resolve"}
	}

	var updatedCount int
	var toPublish []Alarm

	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}

		for _, id := range ids {
			row, err := q.GetAlarm(ctx, id)
			if err != nil {
				return notFoundOr(err)
			}
			if err := chk.Require(authz.AlarmsManage, access.Camera(row.TenantID, row.SiteID, uuid.Nil, row.CameraID, nil)); err != nil {
				return err
			}

			if action == "acknowledge" {
				if row.Status == "resolved" {
					return ErrInvalidTransition
				}
				if row.Status == "acknowledged" {
					continue
				}
				if _, err := q.UpdateAlarmStatus(ctx, db.UpdateAlarmStatusParams{
					ID:      id,
					Status:  "acknowledged",
					ActorID: &actor.UserID,
				}); err != nil {
					return store.Classify(err)
				}
				if err := audit(ctx, q, actor, &row.TenantID, ActionAlarmAcknowledged, id, map[string]any{
					"previous_status": row.Status,
					"bulk":            true,
				}); err != nil {
					return err
				}
				updatedCount++
				updatedRow, _ := q.GetAlarm(ctx, id)
				toPublish = append(toPublish, mapGetAlarmRow(updatedRow))
			} else if action == "resolve" {
				if row.Status == "resolved" {
					continue
				}
				if _, err := q.UpdateAlarmStatus(ctx, db.UpdateAlarmStatusParams{
					ID:      id,
					Status:  "resolved",
					ActorID: &actor.UserID,
				}); err != nil {
					return store.Classify(err)
				}
				if err := audit(ctx, q, actor, &row.TenantID, ActionAlarmResolved, id, map[string]any{
					"previous_status": row.Status,
					"bulk":            true,
				}); err != nil {
					return err
				}
				updatedCount++
				updatedRow, _ := q.GetAlarm(ctx, id)
				toPublish = append(toPublish, mapGetAlarmRow(updatedRow))
			}
		}
		return nil
	})

	if err == nil {
		for _, a := range toPublish {
			s.publish(ctx, action+"d", a)
		}
	}
	return BulkResult{Updated: updatedCount}, err
}

func (s *Service) AssignableUsers(ctx context.Context, actor authz.Actor, alarmID uuid.UUID) ([]Assignee, error) {
	var res []Assignee
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		row, err := q.GetAlarm(ctx, alarmID)
		if err != nil {
			return notFoundOr(err)
		}
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.AlarmsView, access.Camera(row.TenantID, row.SiteID, uuid.Nil, row.CameraID, nil)); err != nil {
			return err
		}

		users, err := q.UsersWithCameraPermission(ctx, db.UsersWithCameraPermissionParams{
			CameraID:   row.CameraID,
			Permission: string(authz.AlarmsManage),
		})
		if err != nil {
			return store.Classify(err)
		}
		res = make([]Assignee, len(users))
		for i, u := range users {
			res[i] = Assignee{
				ID:          u.ID,
				Username:    u.Username,
				DisplayName: u.DisplayName,
			}
		}
		return nil
	})
	return res, err
}

func (s *Service) publish(ctx context.Context, action string, a Alarm) {
	if s.Pub == nil {
		return
	}
	payload := map[string]any{
		"id":         a.ID,
		"tenant_id":  a.TenantID,
		"site_id":    a.SiteID,
		"camera_id":  a.CameraID,
		"event_id":   a.EventID,
		"status":     a.Status,
		"updated_at": a.UpdatedAt,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		if s.Log != nil {
			s.Log.WarnContext(ctx, "marshal alarm event", "error", err)
		}
		return
	}
	subject := fmt.Sprintf("alarm.%s.%s", action, a.TenantID.String())
	if err := s.Pub.Publish(ctx, subject, data); err != nil {
		if s.Log != nil {
			s.Log.WarnContext(ctx, "publish alarm event", "subject", subject, "error", err)
		}
	}
}

func audit(ctx context.Context, q *db.Queries, actor authz.Actor, tenantID *uuid.UUID, action string, alarmID uuid.UUID, details map[string]any) error {
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	if details == nil {
		b = []byte("{}")
	}
	return q.InsertAudit(ctx, db.InsertAuditParams{
		TenantID:   tenantID,
		ActorID:    &actor.UserID,
		ActorName:  actor.Username,
		Action:     action,
		TargetType: "alarm",
		TargetID:   &alarmID,
		RequestID:  logging.RequestID(ctx),
		Ip:         httpx.ClientIP(ctx),
		Details:    b,
	})
}

func notFoundOr(err error) error {
	err = store.Classify(err)
	if errors.Is(err, store.ErrNotFound) {
		return store.ErrNotFound
	}
	return err
}

func mapGetAlarmRow(r db.GetAlarmRow) Alarm {
	var assignedToName *string
	if r.AssignedToName != "" {
		assignedToName = &r.AssignedToName
	}
	var ackByName *string
	if r.AcknowledgedByName != "" {
		ackByName = &r.AcknowledgedByName
	}
	var resByName *string
	if r.ResolvedByName != "" {
		resByName = &r.ResolvedByName
	}
	return Alarm{
		ID:                 r.ID,
		TenantID:           r.TenantID,
		SiteID:             r.SiteID,
		SiteName:           r.SiteName,
		CameraID:           r.CameraID,
		CameraName:         r.CameraName,
		EventID:            r.EventID,
		EventSeverity:      r.EventSeverity,
		EventStartTime:     r.EventStartTime,
		EventEndTime:       r.EventEndTime,
		EventLabels:        r.EventLabels,
		EventSubLabels:     r.EventSubLabels,
		Source:             r.Source,
		Status:             r.Status,
		AssignedTo:         r.AssignedTo,
		AssignedToName:     assignedToName,
		AcknowledgedBy:     r.AcknowledgedBy,
		AcknowledgedByName: ackByName,
		AcknowledgedAt:     r.AcknowledgedAt,
		ResolvedBy:         r.ResolvedBy,
		ResolvedByName:     resByName,
		ResolvedAt:         r.ResolvedAt,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
}

func mapListAlarmRow(r db.ListAlarmsRow) Alarm {
	var assignedToName *string
	if r.AssignedToName != "" {
		assignedToName = &r.AssignedToName
	}
	var ackByName *string
	if r.AcknowledgedByName != "" {
		ackByName = &r.AcknowledgedByName
	}
	var resByName *string
	if r.ResolvedByName != "" {
		resByName = &r.ResolvedByName
	}
	return Alarm{
		ID:                 r.ID,
		TenantID:           r.TenantID,
		SiteID:             r.SiteID,
		SiteName:           r.SiteName,
		CameraID:           r.CameraID,
		CameraName:         r.CameraName,
		EventID:            r.EventID,
		EventSeverity:      r.EventSeverity,
		EventStartTime:     r.EventStartTime,
		EventEndTime:       r.EventEndTime,
		EventLabels:        r.EventLabels,
		EventSubLabels:     r.EventSubLabels,
		Source:             r.Source,
		Status:             r.Status,
		AssignedTo:         r.AssignedTo,
		AssignedToName:     assignedToName,
		AcknowledgedBy:     r.AcknowledgedBy,
		AcknowledgedByName: ackByName,
		AcknowledgedAt:     r.AcknowledgedAt,
		ResolvedBy:         r.ResolvedBy,
		ResolvedByName:     resByName,
		ResolvedAt:         r.ResolvedAt,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
}
