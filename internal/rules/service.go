package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ValidationError is a request the rules service rejects before touching the store.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

func validTrigger(t TriggerType) bool {
	return t == TriggerEvent || t == TriggerCameraOffline || t == TriggerServerOffline
}

const (
	ActionRuleCreated = "RULE_CREATED"
	ActionRuleUpdated = "RULE_UPDATED"
	ActionRuleDeleted = "RULE_DELETED"
)

type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

// DefaultDebounce is how long a rule stays quiet for one camera after it fired on an event.
const DefaultDebounce = 5 * time.Minute

type Service struct {
	Store *store.Store
	Pub   Publisher
	Log   *slog.Logger
	// Debounce is the per rule and camera quiet period after an event rule fires (default
	// DefaultDebounce). Offline rules fire once per outage instead.
	Debounce time.Duration
}

func NewService(st *store.Store, pub Publisher, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{Store: st, Pub: pub, Log: log, Debounce: DefaultDebounce}
}

// claim records that rule fired for resourceID and reports whether it may run its actions:
// false when it already fired at or after notBefore. It is one atomic upsert, so concurrent
// evaluators cannot both win.
func claim(ctx context.Context, tx pgx.Tx, tenantID, ruleID, resourceID uuid.UUID, notBefore time.Time) (bool, error) {
	var fired time.Time
	err := tx.QueryRow(ctx, `
INSERT INTO rule_firings (rule_id, resource_id, tenant_id, fired_at) VALUES ($1, $2, $3, now())
ON CONFLICT (rule_id, resource_id) DO UPDATE SET fired_at = now() WHERE rule_firings.fired_at < $4
RETURNING fired_at`, ruleID, resourceID, tenantID, notBefore).Scan(&fired)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

type CreateRuleRequest struct {
	Name        string      `json:"name"`
	TriggerType TriggerType `json:"trigger_type"`
	Conditions  Conditions  `json:"conditions"`
	Actions     Actions     `json:"actions"`
	Enabled     bool        `json:"enabled"`
}

type UpdateRuleRequest struct {
	Name        *string      `json:"name,omitempty"`
	TriggerType *TriggerType `json:"trigger_type,omitempty"`
	Conditions  *Conditions  `json:"conditions,omitempty"`
	Actions     *Actions     `json:"actions,omitempty"`
	Enabled     *bool        `json:"enabled,omitempty"`
}

func (s *Service) ListRules(ctx context.Context, actor authz.Actor) ([]Rule, error) {
	if actor.TenantID == nil {
		return nil, &access.ForbiddenError{Permission: authz.NotificationsManage}
	}
	var out []Rule
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.NotificationsManage, access.Tenant(*actor.TenantID)); err != nil {
			return err
		}
		rows, err := q.ListRules(ctx, *actor.TenantID)
		if err != nil {
			return store.Classify(err)
		}
		out = make([]Rule, 0, len(rows))
		for _, r := range rows {
			rule, err := ruleFromRow(r)
			if err != nil {
				return err
			}
			out = append(out, rule)
		}
		return nil
	})
	return out, err
}

func (s *Service) GetRule(ctx context.Context, actor authz.Actor, id uuid.UUID) (Rule, error) {
	if actor.TenantID == nil {
		return Rule{}, &access.ForbiddenError{Permission: authz.NotificationsManage}
	}
	var out Rule
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.NotificationsManage, access.Tenant(*actor.TenantID)); err != nil {
			return err
		}
		row, err := q.GetRule(ctx, db.GetRuleParams{ID: id, TenantID: *actor.TenantID})
		if err != nil {
			return store.Classify(err)
		}
		out, err = ruleFromRow(row)
		return err
	})
	return out, err
}

func (s *Service) CreateRule(ctx context.Context, actor authz.Actor, req CreateRuleRequest) (Rule, error) {
	if actor.TenantID == nil {
		return Rule{}, &access.ForbiddenError{Permission: authz.NotificationsManage}
	}
	if strings.TrimSpace(req.Name) == "" {
		return Rule{}, &ValidationError{Msg: "name is required"}
	}
	if !validTrigger(req.TriggerType) {
		return Rule{}, &ValidationError{Msg: "invalid trigger_type " + string(req.TriggerType)}
	}

	condBytes, err := json.Marshal(req.Conditions)
	if err != nil {
		return Rule{}, err
	}
	actBytes, err := json.Marshal(req.Actions)
	if err != nil {
		return Rule{}, err
	}

	var out Rule
	err = s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.NotificationsManage, access.Tenant(*actor.TenantID)); err != nil {
			return err
		}

		row, err := q.CreateRule(ctx, db.CreateRuleParams{
			TenantID:    *actor.TenantID,
			Name:        req.Name,
			TriggerType: string(req.TriggerType),
			Conditions:  condBytes,
			Actions:     actBytes,
			Enabled:     req.Enabled,
		})
		if err != nil {
			return store.Classify(err)
		}

		out, err = ruleFromRow(row)
		if err != nil {
			return err
		}

		return audit(ctx, q, actor, actor.TenantID, ActionRuleCreated, out.ID, map[string]any{
			"name":         out.Name,
			"trigger_type": out.TriggerType,
		})
	})
	return out, err
}

func (s *Service) UpdateRule(ctx context.Context, actor authz.Actor, id uuid.UUID, req UpdateRuleRequest) (Rule, error) {
	if actor.TenantID == nil {
		return Rule{}, &access.ForbiddenError{Permission: authz.NotificationsManage}
	}

	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return Rule{}, &ValidationError{Msg: "name is required"}
	}
	if req.TriggerType != nil && !validTrigger(*req.TriggerType) {
		return Rule{}, &ValidationError{Msg: "invalid trigger_type " + string(*req.TriggerType)}
	}

	var condBytes []byte
	if req.Conditions != nil {
		b, err := json.Marshal(req.Conditions)
		if err != nil {
			return Rule{}, err
		}
		condBytes = b
	}

	var actBytes []byte
	if req.Actions != nil {
		b, err := json.Marshal(req.Actions)
		if err != nil {
			return Rule{}, err
		}
		actBytes = b
	}

	var trType *string
	if req.TriggerType != nil {
		s := string(*req.TriggerType)
		trType = &s
	}

	var out Rule
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.NotificationsManage, access.Tenant(*actor.TenantID)); err != nil {
			return err
		}

		row, err := q.UpdateRule(ctx, db.UpdateRuleParams{
			ID:          id,
			TenantID:    *actor.TenantID,
			Name:        req.Name,
			TriggerType: trType,
			Conditions:  condBytes,
			Actions:     actBytes,
			Enabled:     req.Enabled,
		})
		if err != nil {
			return store.Classify(err)
		}

		out, err = ruleFromRow(row)
		if err != nil {
			return err
		}

		return audit(ctx, q, actor, actor.TenantID, ActionRuleUpdated, out.ID, map[string]any{
			"name":         out.Name,
			"trigger_type": out.TriggerType,
			"enabled":      out.Enabled,
		})
	})
	return out, err
}

func (s *Service) DeleteRule(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	if actor.TenantID == nil {
		return &access.ForbiddenError{Permission: authz.NotificationsManage}
	}
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.NotificationsManage, access.Tenant(*actor.TenantID)); err != nil {
			return err
		}

		rule, err := q.GetRule(ctx, db.GetRuleParams{ID: id, TenantID: *actor.TenantID})
		if err != nil {
			return store.Classify(err)
		}

		if err := q.DeleteRule(ctx, db.DeleteRuleParams{ID: id, TenantID: *actor.TenantID}); err != nil {
			return store.Classify(err)
		}

		return audit(ctx, q, actor, actor.TenantID, ActionRuleDeleted, id, map[string]any{
			"name": rule.Name,
		})
	})
}

func (s *Service) ListNotifications(ctx context.Context, actor authz.Actor, unreadOnly bool, limit int) ([]Notification, int64, error) {
	if actor.TenantID == nil {
		return nil, 0, &access.ForbiddenError{Permission: authz.EventsView}
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var items []Notification
	var unreadCount int64

	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		rows, err := q.ListNotifications(ctx, db.ListNotificationsParams{
			TenantID:   *actor.TenantID,
			UserID:     &actor.UserID,
			UnreadOnly: &unreadOnly,
			LimitCount: int32(limit), //nolint:gosec // bounded
		})
		if err != nil {
			return store.Classify(err)
		}

		count, err := q.CountUnreadNotifications(ctx, db.CountUnreadNotificationsParams{
			TenantID: *actor.TenantID,
			UserID:   &actor.UserID,
		})
		if err != nil {
			return store.Classify(err)
		}
		unreadCount = count

		items = make([]Notification, 0, len(rows))
		for _, r := range rows {
			items = append(items, notificationFromRow(r))
		}
		return nil
	})
	return items, unreadCount, err
}

func (s *Service) MarkNotificationRead(ctx context.Context, actor authz.Actor, id uuid.UUID) (Notification, error) {
	if actor.TenantID == nil {
		return Notification{}, &access.ForbiddenError{Permission: authz.EventsView}
	}
	var out Notification
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		row, err := q.MarkNotificationRead(ctx, db.MarkNotificationReadParams{
			ID:       id,
			TenantID: *actor.TenantID,
			UserID:   &actor.UserID,
		})
		if err != nil {
			return store.Classify(err)
		}
		out = notificationFromRow(row)
		return nil
	})
	return out, err
}

func (s *Service) MarkAllNotificationsRead(ctx context.Context, actor authz.Actor) (int64, error) {
	if actor.TenantID == nil {
		return 0, &access.ForbiddenError{Permission: authz.EventsView}
	}
	var affected int64
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		n, err := q.MarkAllNotificationsRead(ctx, db.MarkAllNotificationsReadParams{
			TenantID: *actor.TenantID,
			UserID:   &actor.UserID,
		})
		if err != nil {
			return store.Classify(err)
		}
		affected = n
		return nil
	})
	return affected, err
}

type NotificationCreatedPayload struct {
	ID        uuid.UUID  `json:"id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	UserID    *uuid.UUID `json:"user_id,omitempty"`
	RuleID    *uuid.UUID `json:"rule_id,omitempty"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Link      *string    `json:"link,omitempty"`
	Severity  string     `json:"severity"`
	CreatedAt time.Time  `json:"created_at"`
}

type OpenedAlarmPayload struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	SiteID    uuid.UUID `json:"site_id"`
	CameraID  uuid.UUID `json:"camera_id"`
	EventID   uuid.UUID `json:"event_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) debounce() time.Duration {
	if s.Debounce <= 0 {
		return DefaultDebounce
	}
	return s.Debounce
}

// EvaluateEvent runs every enabled event rule of the tenant against a newly indexed event.
func (s *Service) EvaluateEvent(ctx context.Context, ev EventContext) error {
	actor := authz.Actor{TenantID: &ev.TenantID}
	return s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		q := db.New(tx)
		rulesRows, err := q.ListActiveRulesByTrigger(ctx, db.ListActiveRulesByTriggerParams{
			TenantID:    ev.TenantID,
			TriggerType: string(TriggerEvent),
		})
		if err != nil {
			return store.Classify(err)
		}

		for _, rRow := range rulesRows {
			rule, err := ruleFromRow(rRow)
			if err != nil {
				continue
			}
			if !rule.Conditions.MatchesEvent(ev) {
				continue
			}

			ok, err := claim(ctx, tx, ev.TenantID, rule.ID, ev.CameraID, time.Now().Add(-s.debounce()))
			if err != nil {
				return store.Classify(err)
			}
			if !ok {
				continue // fired for this camera inside the debounce window
			}

			// Matched! Execute actions
			var alarmID *uuid.UUID
			if rule.Actions.CreateAlarm {
				var openedID uuid.UUID
				var status string
				var createdAt time.Time
				err := tx.QueryRow(ctx, `
					INSERT INTO alarms (tenant_id, site_id, camera_id, event_id, source)
					VALUES ($1, $2, $3, $4, 'rule')
					ON CONFLICT (event_id, source) DO NOTHING
					RETURNING id, status, created_at`,
					ev.TenantID, ev.SiteID, ev.CameraID, ev.ID,
				).Scan(&openedID, &status, &createdAt)
				if err == nil {
					alarmID = &openedID
					if s.Pub != nil {
						payload, _ := json.Marshal(OpenedAlarmPayload{
							ID:        openedID,
							TenantID:  ev.TenantID,
							SiteID:    ev.SiteID,
							CameraID:  ev.CameraID,
							EventID:   ev.ID,
							Status:    status,
							CreatedAt: createdAt,
						})
						_ = s.Pub.Publish(ctx, "alarm.opened."+ev.TenantID.String(), payload)
					}
				} else if !errors.Is(err, pgx.ErrNoRows) {
					s.Log.WarnContext(ctx, "failed to create rule alarm", "error", err, "rule_id", rule.ID)
				}
			}

			if rule.Actions.NotifyInApp {
				severity := rule.Actions.Severity
				if severity == "" {
					severity = "warning"
				}
				title := rule.Name
				labelsStr := strings.Join(ev.Labels, ", ")
				body := fmt.Sprintf("Evento detectado en cámara %s: %s", ev.CameraName, labelsStr)
				if len(ev.Labels) == 0 {
					body = fmt.Sprintf("Evento detectado en cámara %s (%s)", ev.CameraName, ev.Severity)
				}
				link := fmt.Sprintf("/events?selected=%s", ev.ID)
				if alarmID != nil {
					link = "/alarms"
				}

				notifRow, err := q.CreateNotification(ctx, db.CreateNotificationParams{
					TenantID: ev.TenantID,
					UserID:   nil, // tenant-wide
					RuleID:   &rule.ID,
					Title:    title,
					Body:     body,
					Link:     &link,
					Severity: severity,
				})
				if err == nil && s.Pub != nil {
					payload, _ := json.Marshal(NotificationCreatedPayload{
						ID:        notifRow.ID,
						TenantID:  notifRow.TenantID,
						RuleID:    notifRow.RuleID,
						Title:     notifRow.Title,
						Body:      notifRow.Body,
						Link:      notifRow.Link,
						Severity:  notifRow.Severity,
						CreatedAt: notifRow.CreatedAt,
					})
					_ = s.Pub.Publish(ctx, "notification.created."+ev.TenantID.String(), payload)
				} else if err != nil {
					s.Log.WarnContext(ctx, "failed to create notification", "error", err, "rule_id", rule.ID)
				}
			}
		}
		return nil
	})
}

// Outage describes a camera or server that is currently down. Since is the outage start and
// Duration how long it has lasted, both measured by the database so that the app clock never
// enters the once-per-outage decision.
type Outage struct {
	TenantID   uuid.UUID
	ResourceID uuid.UUID
	SiteID     uuid.UUID
	Name       string
	Since      time.Time
	Duration   time.Duration
}

// EvaluateOffline runs the tenant's offline rules for a camera or server outage. Callers may
// invoke it on every tick, and from any process: each rule notifies once per outage, because a
// firing is only repeated when it predates the outage start.
func (s *Service) EvaluateOffline(ctx context.Context, triggerType TriggerType, o Outage) error {
	tenantID, resourceID, resourceName := o.TenantID, o.ResourceID, o.Name
	actor := authz.Actor{TenantID: &tenantID}
	return s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		q := db.New(tx)
		rulesRows, err := q.ListActiveRulesByTrigger(ctx, db.ListActiveRulesByTriggerParams{
			TenantID:    tenantID,
			TriggerType: string(triggerType),
		})
		if err != nil {
			return store.Classify(err)
		}

		for _, rRow := range rulesRows {
			rule, err := ruleFromRow(rRow)
			if err != nil {
				continue
			}

			matched := false
			switch triggerType {
			case TriggerCameraOffline:
				matched = rule.Conditions.MatchesCameraOffline(resourceID, o.SiteID, o.Duration)
			case TriggerServerOffline:
				matched = rule.Conditions.MatchesServerOffline(resourceID, o.SiteID, o.Duration)
			}
			if !matched {
				continue
			}

			// Once per outage: skip when the rule already fired since the resource went down.
			ok, err := claim(ctx, tx, tenantID, rule.ID, resourceID, o.Since)
			if err != nil {
				return store.Classify(err)
			}
			if !ok {
				continue
			}

			if rule.Actions.NotifyInApp {
				severity := rule.Actions.Severity
				if severity == "" {
					severity = "critical"
				}
				title := rule.Name
				kind := "Cámara"
				link := "/cameras"
				if triggerType == TriggerServerOffline {
					kind = "Servidor"
					link = "/servers"
				}
				body := fmt.Sprintf("%s '%s' está desconectado.", kind, resourceName)

				notifRow, err := q.CreateNotification(ctx, db.CreateNotificationParams{
					TenantID: tenantID,
					UserID:   nil,
					RuleID:   &rule.ID,
					Title:    title,
					Body:     body,
					Link:     &link,
					Severity: severity,
				})
				if err == nil && s.Pub != nil {
					payload, _ := json.Marshal(NotificationCreatedPayload{
						ID:        notifRow.ID,
						TenantID:  notifRow.TenantID,
						RuleID:    notifRow.RuleID,
						Title:     notifRow.Title,
						Body:      notifRow.Body,
						Link:      notifRow.Link,
						Severity:  notifRow.Severity,
						CreatedAt: notifRow.CreatedAt,
					})
					_ = s.Pub.Publish(ctx, "notification.created."+tenantID.String(), payload)
				}
			}
		}
		return nil
	})
}

func ruleFromRow(row db.Rule) (Rule, error) {
	var c Conditions
	if len(row.Conditions) > 0 {
		_ = json.Unmarshal(row.Conditions, &c)
	}
	var a Actions
	if len(row.Actions) > 0 {
		_ = json.Unmarshal(row.Actions, &a)
	}
	return Rule{
		ID:          row.ID,
		TenantID:    row.TenantID,
		Name:        row.Name,
		TriggerType: TriggerType(row.TriggerType),
		Conditions:  c,
		Actions:     a,
		Enabled:     row.Enabled,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func notificationFromRow(row db.Notification) Notification {
	return Notification{
		ID:        row.ID,
		TenantID:  row.TenantID,
		UserID:    row.UserID,
		RuleID:    row.RuleID,
		Title:     row.Title,
		Body:      row.Body,
		Link:      row.Link,
		Severity:  row.Severity,
		ReadAt:    row.ReadAt,
		CreatedAt: row.CreatedAt,
	}
}

func audit(ctx context.Context, q *db.Queries, actor authz.Actor, tenantID *uuid.UUID, action string, targetID uuid.UUID, details map[string]any) error {
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	var aid *uuid.UUID
	if actor.UserID != uuid.Nil {
		aid = &actor.UserID
	}
	return q.InsertAudit(ctx, db.InsertAuditParams{
		TenantID:   tenantID,
		ActorID:    aid,
		Action:     action,
		TargetType: "rule",
		TargetID:   &targetID,
		Details:    b,
	})
}
