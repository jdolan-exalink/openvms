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
	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/notify"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ValidationError is a request the rules service rejects before touching the store.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

// ErrVersionConflict is returned when an update carries a stale rule version.
var ErrVersionConflict = errors.New("rule was modified by someone else; reload and retry with the current version")

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

// ZoneResolver resolves which map zones apply to an event on a camera.
type ZoneResolver interface {
	ZonesForEvent(ctx context.Context, cameraID uuid.UUID, frigateZones []string) ([]uuid.UUID, error)
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
	// ZoneResolver optionally maps frigate zones to map zones for an event.
	ZoneResolver ZoneResolver
	// Faces resolves person and watchlist conditions to recognized names. Without it a rule
	// with person conditions never matches (fail closed).
	Faces FaceResolver
}

// RuleVersionSnapshot is an immutable rule version exposed to authorized tenant users.
type RuleVersionSnapshot struct {
	Version   int             `json:"version"`
	Snapshot  json.RawMessage `json:"snapshot"`
	ChangedBy *uuid.UUID      `json:"changed_by,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// AuthorizeSimulation requires rule-management permission without changing rule state.
func (s *Service) AuthorizeSimulation(ctx context.Context, actor authz.Actor) error {
	if actor.TenantID == nil {
		return &access.ForbiddenError{Permission: authz.RulesManage}
	}
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		return chk.Require(authz.RulesManage, access.Tenant(*actor.TenantID))
	})
}

// SimulationEventMetadata resolves the same site timezone, group membership, and map-zone
// relationships used by production evaluation for a camera visible to the caller.
func (s *Service) SimulationEventMetadata(ctx context.Context, actor authz.Actor, cameraID, siteID uuid.UUID, zones []string) (string, []uuid.UUID, []uuid.UUID, error) {
	if actor.TenantID == nil {
		return "", nil, nil, &access.ForbiddenError{Permission: authz.RulesManage}
	}
	var timezone string
	var groups, mapZones []uuid.UUID
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.RulesManage, access.Tenant(*actor.TenantID)); err != nil {
			return err
		}
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return store.Classify(err)
		}
		if cam.TenantID != *actor.TenantID || cam.SiteID != siteID {
			return store.ErrNotFound
		}
		site, err := q.GetSite(ctx, siteID)
		if err != nil {
			return store.Classify(err)
		}
		timezone = site.Timezone
		groups, err = q.ListCameraGroupsForCamera(ctx, db.ListCameraGroupsForCameraParams{TenantID: *actor.TenantID, CameraID: cameraID})
		return err
	})
	if err != nil {
		return "", nil, nil, err
	}
	if s.ZoneResolver != nil {
		mapZones, err = s.ZoneResolver.ZonesForEvent(ctx, cameraID, zones)
		if err != nil {
			return "", nil, nil, err
		}
	}
	return timezone, groups, mapZones, nil
}

// ListRuleVersions returns snapshots only when the rule belongs to the actor's tenant.
func (s *Service) ListRuleVersions(ctx context.Context, actor authz.Actor, id uuid.UUID) ([]RuleVersionSnapshot, error) {
	if actor.TenantID == nil {
		return nil, &access.ForbiddenError{Permission: authz.RulesRead}
	}
	var out []RuleVersionSnapshot
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.RulesRead, access.Tenant(*actor.TenantID)); err != nil {
			return err
		}
		if _, err := q.GetRule(ctx, db.GetRuleParams{ID: id, TenantID: *actor.TenantID}); err != nil {
			return store.Classify(err)
		}
		rows, err := q.ListRuleVersions(ctx, db.ListRuleVersionsParams{TenantID: *actor.TenantID, RuleID: id})
		if err != nil {
			return err
		}
		out = make([]RuleVersionSnapshot, 0)
		for _, row := range rows {
			var item RuleVersionSnapshot
			item.Version = int(row.Version)
			item.Snapshot = row.Snapshot
			item.ChangedBy = row.ChangedBy
			item.CreatedAt = row.CreatedAt
			out = append(out, item)
		}
		return nil
	})
	return out, err
}

func NewService(st *store.Store, pub Publisher, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	var zr ZoneResolver
	var fr FaceResolver
	if st != nil {
		zr = NewStoreZoneResolver(st)
		fr = NewStoreFaceResolver(st)
	}
	return &Service{Store: st, Pub: pub, Log: log, Debounce: DefaultDebounce, ZoneResolver: zr, Faces: fr}
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
	// ExpectedVersion is the rule version the caller based the change on (optimistic locking).
	// It is optional: 0 skips the check, a stale value yields ErrVersionConflict.
	ExpectedVersion int          `json:"version"`
	Name            *string      `json:"name,omitempty"`
	TriggerType     *TriggerType `json:"trigger_type,omitempty"`
	Conditions      *Conditions  `json:"conditions,omitempty"`
	Actions         *Actions     `json:"actions,omitempty"`
	Enabled         *bool        `json:"enabled,omitempty"`
}

func (s *Service) ListRules(ctx context.Context, actor authz.Actor) ([]Rule, error) {
	if actor.TenantID == nil {
		return nil, &access.ForbiddenError{Permission: authz.RulesRead}
	}
	var out []Rule
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.RulesRead, access.Tenant(*actor.TenantID)); err != nil {
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
		return Rule{}, &access.ForbiddenError{Permission: authz.RulesRead}
	}
	var out Rule
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.RulesRead, access.Tenant(*actor.TenantID)); err != nil {
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
		return Rule{}, &access.ForbiddenError{Permission: authz.RulesManage}
	}
	if strings.TrimSpace(req.Name) == "" {
		return Rule{}, &ValidationError{Msg: "name is required"}
	}
	if !validTrigger(req.TriggerType) {
		return Rule{}, &ValidationError{Msg: "invalid trigger_type " + string(req.TriggerType)}
	}
	if err := ValidateConditions(req.TriggerType, req.Conditions); err != nil {
		return Rule{}, err
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
		if err := chk.Require(authz.RulesManage, access.Tenant(*actor.TenantID)); err != nil {
			return err
		}
		if err := checkChannels(ctx, q, *actor.TenantID, req.Actions.ChannelIDs); err != nil {
			return err
		}
		if err := checkConditionRefs(ctx, q, chk, *actor.TenantID, req.Conditions); err != nil {
			return err
		}
		if err := checkFaceRefs(ctx, q, chk, *actor.TenantID, req.Conditions, Conditions{}); err != nil {
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
		if err := recordVersion(ctx, q, actor, out); err != nil {
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
		return Rule{}, &access.ForbiddenError{Permission: authz.RulesManage}
	}

	if req.ExpectedVersion < 0 {
		return Rule{}, &ValidationError{Msg: "the rule version must be a positive integer (If-Match header)"}
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
		if err := chk.Require(authz.RulesManage, access.Tenant(*actor.TenantID)); err != nil {
			return err
		}
		if req.Actions != nil {
			if err := checkChannels(ctx, q, *actor.TenantID, req.Actions.ChannelIDs); err != nil {
				return err
			}
		}

		current, err := q.GetRule(ctx, db.GetRuleParams{ID: id, TenantID: *actor.TenantID})
		if err != nil {
			return store.Classify(err)
		}
		if req.ExpectedVersion != 0 && int(current.Version) != req.ExpectedVersion {
			return ErrVersionConflict
		}
		// Validate the merged rule only when the patch touches what the validator
		// checks, so unrelated edits (e.g. disabling) still work on legacy rows.
		if req.Conditions != nil || req.TriggerType != nil {
			effTrigger := TriggerType(current.TriggerType)
			if req.TriggerType != nil {
				effTrigger = *req.TriggerType
			}
			effCond := Conditions{}
			if req.Conditions != nil {
				effCond = *req.Conditions
			} else if len(current.Conditions) > 0 {
				if err := json.Unmarshal(current.Conditions, &effCond); err != nil {
					return fmt.Errorf("decode stored conditions for rule %s: %w", id, err)
				}
			}
			if err := ValidateConditions(effTrigger, effCond); err != nil {
				return err
			}
			if req.Conditions != nil {
				if err := checkConditionRefs(ctx, q, chk, *actor.TenantID, effCond); err != nil {
					return err
				}
				prev := Conditions{}
				if len(current.Conditions) > 0 {
					if err := json.Unmarshal(current.Conditions, &prev); err != nil {
						return fmt.Errorf("decode stored conditions for rule %s: %w", id, err)
					}
				}
				if err := checkFaceRefs(ctx, q, chk, *actor.TenantID, effCond, prev); err != nil {
					return err
				}
			}
		}

		row, err := q.UpdateRule(ctx, db.UpdateRuleParams{
			ID:              id,
			TenantID:        *actor.TenantID,
			ExpectedVersion: current.Version,
			Name:            req.Name,
			TriggerType:     trType,
			Conditions:      condBytes,
			Actions:         actBytes,
			Enabled:         req.Enabled,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVersionConflict // changed between the read and the guarded update
		}
		if err != nil {
			return store.Classify(err)
		}

		out, err = ruleFromRow(row)
		if err != nil {
			return err
		}
		if err := recordVersion(ctx, q, actor, out); err != nil {
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
		return &access.ForbiddenError{Permission: authz.RulesManage}
	}
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.RulesManage, access.Tenant(*actor.TenantID)); err != nil {
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

func (s *Service) debounce() time.Duration {
	if s.Debounce <= 0 {
		return DefaultDebounce
	}
	return s.Debounce
}

func (s *Service) debounceFor(r Rule) time.Duration {
	if r.Conditions.CooldownSeconds != nil && *r.Conditions.CooldownSeconds > 0 {
		return time.Duration(*r.Conditions.CooldownSeconds) * time.Second
	}
	return s.debounce()
}

func loadCameraGroupIDs(ctx context.Context, tx pgx.Tx, tenantID, cameraID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT group_id FROM camera_group_members WHERE tenant_id = $1 AND camera_id = $2`, tenantID, cameraID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func loadSiteTimezone(ctx context.Context, tx pgx.Tx, tenantID, siteID, cameraID uuid.UUID) string {
	var tz string
	if siteID != uuid.Nil {
		_ = tx.QueryRow(ctx, `SELECT timezone FROM sites WHERE id = $1 AND tenant_id = $2`, siteID, tenantID).Scan(&tz)
		if tz != "" {
			return tz
		}
	}
	if cameraID != uuid.Nil {
		_ = tx.QueryRow(ctx, `
SELECT s.timezone FROM cameras c
JOIN sites s ON s.id = c.site_id AND s.tenant_id = c.tenant_id
WHERE c.id = $1 AND c.tenant_id = $2`, cameraID, tenantID).Scan(&tz)
	}
	return tz
}

// FaceNames resolves the face conditions of c to the recognized names they stand for. Without
// a resolver it resolves to nobody, so a person rule never matches by accident.
func (s *Service) FaceNames(ctx context.Context, tenantID uuid.UUID, c Conditions) (map[string]struct{}, error) {
	return s.faceNames(ctx, nil, tenantID, c)
}

// faceNames is FaceNames reading through q, the caller's transaction, when it is not nil.
func (s *Service) faceNames(ctx context.Context, q *db.Queries, tenantID uuid.UUID, c Conditions) (map[string]struct{}, error) {
	if s.Faces == nil || !c.HasFaceConditions() {
		return map[string]struct{}{}, nil
	}
	return s.Faces.FaceNames(ctx, q, tenantID, c.FacePersonIDs, c.FaceWatchlistIDs)
}

// matchesFaces is the person part of an event rule, resolved through q (the evaluation transaction). A resolver failure does not match (and is
// logged without names): missing an alarm for one event is safer than alarming on the wrong one.
func (s *Service) matchesFaces(ctx context.Context, q *db.Queries, ev EventContext, rule Rule) bool {
	if len(ev.SubLabels) == 0 {
		return false
	}
	names, err := s.faceNames(ctx, q, ev.TenantID, rule.Conditions)
	if err != nil {
		s.Log.WarnContext(ctx, "resolve face conditions", "error", err, "rule_id", rule.ID)
		return false
	}
	return MatchesFaces(ev.SubLabels, names)
}

// EvaluateEvent runs every enabled event rule of the tenant against a newly indexed event.
func (s *Service) EvaluateEvent(ctx context.Context, ev EventContext) error {
	if s.ZoneResolver != nil && len(ev.MapZoneIDs) == 0 {
		if zids, err := s.ZoneResolver.ZonesForEvent(ctx, ev.CameraID, ev.Zones); err == nil {
			ev.MapZoneIDs = zids
		}
	}
	actor := authz.Actor{TenantID: &ev.TenantID}
	// Alarm announcements wait for the commit: a rolled-back transaction must not publish.
	var published []publishedAlarm
	var closed []alarms.ClosedAlarm
	err := s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		q := db.New(tx)
		rulesRows, err := q.ListActiveRulesByTrigger(ctx, db.ListActiveRulesByTriggerParams{
			TenantID:    ev.TenantID,
			TriggerType: string(TriggerEvent),
		})
		if err != nil {
			return store.Classify(err)
		}
		if len(rulesRows) == 0 {
			return nil
		}
		if len(ev.CameraGroupIDs) == 0 && ev.CameraID != uuid.Nil {
			groupIDs, err := loadCameraGroupIDs(ctx, tx, ev.TenantID, ev.CameraID)
			if err != nil {
				return store.Classify(err)
			}
			ev.CameraGroupIDs = groupIDs
		}
		if ev.SiteTimezone == "" {
			ev.SiteTimezone = loadSiteTimezone(ctx, tx, ev.TenantID, ev.SiteID, ev.CameraID)
		}

		for _, rRow := range rulesRows {
			rule, err := ruleFromRow(rRow)
			if err != nil {
				continue
			}
			hasFaces := rule.Conditions.HasFaceConditions()
			if ev.FaceUpdate && !hasFaces {
				continue
			}
			if !rule.Conditions.MatchesEvent(ev) {
				continue
			}
			if hasFaces && !s.matchesFaces(ctx, q, ev, rule) {
				continue
			}

			ok, err := claim(ctx, tx, ev.TenantID, rule.ID, ev.CameraID, time.Now().Add(-s.debounceFor(rule)))
			if err != nil {
				return store.Classify(err)
			}
			if !ok {
				continue // fired for this camera inside the debounce window
			}

			// Matched! Execute actions
			var alarmID *uuid.UUID
			if rule.Actions.CreateAlarm {
				// One alarm per ongoing situation of this rule on this camera and label.
				res, err := alarms.Correlate(ctx, tx, alarms.CorrelateInput{
					TenantID: ev.TenantID, SiteID: ev.SiteID, CameraID: ev.CameraID, EventID: ev.ID,
					Source: "rule", RuleID: &rule.ID, Labels: ev.Labels, At: ev.Start,
				})
				if err != nil {
					return fmt.Errorf("correlate alarm for rule %s: %w", rule.ID, err)
				}
				if res.Outcome != alarms.Skipped {
					id := res.AlarmID
					alarmID = &id
					action := "updated"
					if res.Outcome == alarms.Created {
						action = "opened"
					}
					published = append(published, publishedAlarm{action: action, res: res})
				}
				closed = append(closed, res.Closed...)
			}

			severity := rule.Actions.Severity
			if severity == "" {
				severity = "warning"
			}
			body := fmt.Sprintf("Evento detectado en cámara %s: %s", ev.CameraName, strings.Join(ev.Labels, ", "))
			if len(ev.Labels) == 0 {
				body = fmt.Sprintf("Evento detectado en cámara %s (%s)", ev.CameraName, ev.Severity)
			}
			link := fmt.Sprintf("/events?selected=%s", ev.ID)
			if alarmID != nil {
				link = "/alarms"
			}
			s.deliver(ctx, tx, q, rule, ev.TenantID, resourceRef{Server: ev.ServerID, Camera: ev.CameraID}, rule.Name, body, link, severity)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, p := range published {
		alarms.PublishAlarm(ctx, s.Pub, s.Log, p.action, p.res.Status, ev.TenantID, ev.SiteID, ev.CameraID, ev.ID, p.res.AlarmID, p.res.UpdatedAt)
	}
	for _, c := range closed {
		alarms.PublishAlarm(ctx, s.Pub, s.Log, "closed", "closed", c.TenantID, c.SiteID, c.CameraID, c.EventID, c.ID, c.UpdatedAt)
	}
	return nil
}

// publishedAlarm is an alarm announcement held back until the transaction commits.
type publishedAlarm struct {
	action string
	res    alarms.CorrelateResult
}

// Outage describes a camera or server that is currently down. Since is the outage start and
// Duration how long it has lasted, both measured by the database so that the app clock never
// enters the once-per-outage decision.
type Outage struct {
	TenantID   uuid.UUID
	ResourceID uuid.UUID
	SiteID     uuid.UUID
	// ServerID is the server of a camera outage; for a server outage it equals ResourceID.
	ServerID uuid.UUID
	Name     string
	Since    time.Time
	Duration time.Duration
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

			severity := rule.Actions.Severity
			if severity == "" {
				severity = "critical"
			}
			kind, link := "Cámara", "/cameras"
			ref := resourceRef{Server: o.ServerID, Camera: resourceID}
			if triggerType == TriggerServerOffline {
				kind, link = "Servidor", "/servers"
				ref = resourceRef{Server: resourceID}
			}
			body := fmt.Sprintf("%s '%s' está desconectado.", kind, resourceName)
			s.deliver(ctx, tx, q, rule, tenantID, ref, rule.Name, body, link, severity)
		}
		return nil
	})
}

// resourceRef is the server and camera a notification was raised for; zero means none. It is
// stored on the in-app notification so it can be removed when the server is deleted.
type resourceRef struct{ Server, Camera uuid.UUID }

func idPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// deliver runs the notification actions of a fired rule: the in-app notification (published on
// the bus) and the external channel deliveries. The deliveries are enqueued in the caller's
// transaction, atomically with the firing claim; sending happens later in the worker.
func (s *Service) deliver(ctx context.Context, tx pgx.Tx, q *db.Queries, rule Rule, tenantID uuid.UUID, ref resourceRef, title, body, link, severity string) {
	var notifID *uuid.UUID
	if rule.Actions.NotifyInApp {
		notifRow, err := q.CreateNotification(ctx, db.CreateNotificationParams{
			TenantID: tenantID,
			UserID:   nil, // tenant-wide
			RuleID:   &rule.ID,
			Title:    title,
			Body:     body,
			Link:     &link,
			Severity: severity,
			ServerID: idPtr(ref.Server),
			CameraID: idPtr(ref.Camera),
		})
		if err != nil {
			s.Log.WarnContext(ctx, "failed to create notification", "error", err, "rule_id", rule.ID)
		} else {
			notifID = &notifRow.ID
			if s.Pub != nil {
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
	if len(rule.Actions.ChannelIDs) > 0 {
		notify.EnqueueLogged(ctx, tx, s.Log, notify.EnqueueInput{
			TenantID: tenantID, RuleID: &rule.ID, NotificationID: notifID, ChannelIDs: rule.Actions.ChannelIDs,
			Title: title, Body: body, Link: link, Severity: severity, OccurredAt: time.Now(),
		})
	}
}

// checkChannels rejects channel ids that do not belong to the tenant.
func checkChannels(ctx context.Context, q *db.Queries, tenantID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	unique := map[uuid.UUID]struct{}{}
	for _, id := range ids {
		unique[id] = struct{}{}
	}
	n, err := q.CountNotificationChannelsByIDs(ctx, db.CountNotificationChannelsByIDsParams{TenantID: tenantID, Ids: ids})
	if err != nil {
		return store.Classify(err)
	}
	if int(n) != len(unique) {
		return &ValidationError{Msg: "unknown notification channel in actions.channel_ids"}
	}
	return nil
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
		Version:     int(row.Version),
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

// recordVersion stores a snapshot of the rule as of its current version, in the caller's transaction.
func recordVersion(ctx context.Context, q *db.Queries, actor authz.Actor, r Rule) error {
	snap, err := json.Marshal(r)
	if err != nil {
		return err
	}
	changedBy := actor.UserID
	var by *uuid.UUID
	if changedBy != uuid.Nil {
		by = &changedBy
	}
	return store.Classify(q.InsertRuleVersion(ctx, db.InsertRuleVersionParams{
		RuleID:    r.ID,
		TenantID:  r.TenantID,
		Version:   int32(r.Version), //nolint:gosec // small counter
		Snapshot:  snap,
		ChangedBy: by,
	}))
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

// checkConditionRefs rejects ids in the conditions that do not exist in the caller's tenant
// (row-level security and the tenant filter hide other tenants' rows from the count).
func checkConditionRefs(ctx context.Context, q *db.Queries, chk *access.Checker, tenantID uuid.UUID, c Conditions) error {
	checks := []struct {
		field string
		ids   []uuid.UUID
		count func(ids []uuid.UUID) (int64, error)
	}{
		{"camera_ids", c.CameraIDs, func(ids []uuid.UUID) (int64, error) {
			return q.CountCamerasByIDs(ctx, db.CountCamerasByIDsParams{TenantID: tenantID, Ids: ids})
		}},
		{"camera_group_ids", c.CameraGroupIDs, func(ids []uuid.UUID) (int64, error) {
			return q.CountCameraGroupsByIDs(ctx, db.CountCameraGroupsByIDsParams{TenantID: tenantID, Ids: ids})
		}},
		{"server_ids", c.ServerIDs, func(ids []uuid.UUID) (int64, error) {
			return q.CountServersByIDs(ctx, db.CountServersByIDsParams{TenantID: tenantID, Ids: ids})
		}},
		{"site_ids", c.SiteIDs, func(ids []uuid.UUID) (int64, error) {
			return q.CountSitesByIDs(ctx, db.CountSitesByIDsParams{TenantID: tenantID, Ids: ids})
		}},
		{"map_zone_ids", c.MapZoneIDs, func(ids []uuid.UUID) (int64, error) {
			return q.CountMapZonesByIDs(ctx, db.CountMapZonesByIDsParams{TenantID: tenantID, Ids: ids})
		}},
	}
	for _, ch := range checks {
		if len(ch.ids) == 0 {
			continue
		}
		ids := uniqueIDs(ch.ids)
		n, err := ch.count(ids)
		if err != nil {
			return store.Classify(err)
		}
		if int(n) != len(ids) {
			return &ValidationError{Msg: "unknown or inaccessible id in conditions." + ch.field}
		}
	}
	for _, id := range uniqueIDs(c.CameraIDs) {
		cam, err := q.GetCamera(ctx, id)
		if err != nil {
			return store.Classify(err)
		}
		if cam.TenantID != tenantID || !chk.Can(authz.CamerasView, access.Camera(tenantID, cam.SiteID, cam.ServerID, cam.ID, cam.GroupIds)) {
			return &ValidationError{Msg: "unknown or inaccessible id in conditions.camera_ids"}
		}
	}
	for _, id := range uniqueIDs(c.CameraGroupIDs) {
		if !chk.Can(authz.CamerasView, access.CameraGroup(tenantID, id)) {
			return &ValidationError{Msg: "unknown or inaccessible id in conditions.camera_group_ids"}
		}
	}
	for _, id := range uniqueIDs(c.MapZoneIDs) {
		zone, err := q.GetMapZone(ctx, db.GetMapZoneParams{ID: id, TenantID: &tenantID})
		if err != nil {
			return store.Classify(err)
		}
		if !chk.Can(authz.MapsView, access.Site(tenantID, zone.SiteID)) {
			return &ValidationError{Msg: "unknown or inaccessible id in conditions.map_zone_ids"}
		}
	}
	return nil
}

// checkFaceRefs validates the person and watchlist conditions. They name registered persons, so
// setting or changing them needs faces.view; re-sending the stored values (an unrelated edit by
// someone without faces.view) is accepted. Ids that no longer exist are refused only when they
// are new: an existing rule may keep a reference to a person deleted since, which matches nobody.
func checkFaceRefs(ctx context.Context, q *db.Queries, chk *access.Checker, tenantID uuid.UUID, c, prev Conditions) error {
	newPersons := subtractIDs(uniqueIDs(c.FacePersonIDs), prev.FacePersonIDs)
	newLists := subtractIDs(uniqueIDs(c.FaceWatchlistIDs), prev.FaceWatchlistIDs)
	removed := len(subtractIDs(uniqueIDs(prev.FacePersonIDs), c.FacePersonIDs)) + len(subtractIDs(uniqueIDs(prev.FaceWatchlistIDs), c.FaceWatchlistIDs))
	if len(newPersons) == 0 && len(newLists) == 0 && removed == 0 {
		return nil
	}
	if err := chk.Require(authz.FacesView, access.Tenant(tenantID)); err != nil {
		return err
	}
	if len(newPersons) > 0 {
		n, err := q.CountFacePersonsByIDs(ctx, db.CountFacePersonsByIDsParams{TenantID: tenantID, Ids: newPersons})
		if err != nil {
			return store.Classify(err)
		}
		if int(n) != len(newPersons) {
			return &ValidationError{Msg: "unknown or inaccessible id in conditions.face_person_ids"}
		}
	}
	if len(newLists) > 0 {
		n, err := q.CountFaceWatchlistsByIDs(ctx, db.CountFaceWatchlistsByIDsParams{TenantID: tenantID, Ids: newLists})
		if err != nil {
			return store.Classify(err)
		}
		if int(n) != len(newLists) {
			return &ValidationError{Msg: "unknown or inaccessible id in conditions.face_watchlist_ids"}
		}
	}
	return nil
}

// subtractIDs returns the ids of a that are not in b.
func subtractIDs(a, b []uuid.UUID) []uuid.UUID {
	var out []uuid.UUID
	for _, id := range a {
		if !containsID(b, id) {
			out = append(out, id)
		}
	}
	return out
}
