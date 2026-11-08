package faces

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ActionRecurrenceUpdated is the audit action written when an operator changes the recurrence rule.
const ActionRecurrenceUpdated = "FACE_RECURRENCE_UPDATED"

// Bounds of the rule, the same the database enforces.
const (
	MaxRecurrenceAppearances = 100
	MaxRecurrenceWindowDays  = 365
)

// Validate names the first field of the rule that is out of range.
func (r ClusterRule) Validate() error {
	if r.MinAppearances < 1 || r.MinAppearances > MaxRecurrenceAppearances {
		return invalid("min_appearances must be between 1 and %d", MaxRecurrenceAppearances)
	}
	if r.WindowDays < 1 || r.WindowDays > MaxRecurrenceWindowDays {
		return invalid("window_days must be between 1 and %d", MaxRecurrenceWindowDays)
	}
	return nil
}

// RecurrenceSettings is the saved platform rule and who last changed it.
type RecurrenceSettings struct {
	Rule      ClusterRule
	UpdatedAt time.Time
	UpdatedBy *uuid.UUID
}

func recurrenceFromRow(r db.FaceRecurrenceSetting) RecurrenceSettings {
	return RecurrenceSettings{
		Rule:      ClusterRule{MinAppearances: int(r.MinAppearances), WindowDays: int(r.WindowDays)},
		UpdatedAt: r.UpdatedAt,
		UpdatedBy: r.UpdatedBy,
	}
}

// SavedClusterRule is the rule the faces page applies when a request does not name its own. It is a
// platform setting with no personal data, so reading it needs no permission beyond the caller's own.
func (s *Service) SavedClusterRule(ctx context.Context) (ClusterRule, error) {
	var out ClusterRule
	err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		row, err := q.GetFaceRecurrenceSettings(ctx)
		if err != nil {
			return store.Classify(err)
		}
		out = recurrenceFromRow(row).Rule
		return nil
	})
	return out, err
}

func recurrenceGate(ctx context.Context, q *db.Queries, actor authz.Actor) error {
	chk, err := access.Load(ctx, q, actor)
	if err != nil {
		return err
	}
	return chk.Require(authz.TenantManage, authz.Resource{Kind: authz.ScopePlatform})
}

// GetRecurrence returns the saved rule for an operator who may change it.
func (s *Service) GetRecurrence(ctx context.Context, actor authz.Actor) (RecurrenceSettings, error) {
	var out RecurrenceSettings
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		if err := recurrenceGate(ctx, q, actor); err != nil {
			return err
		}
		row, err := q.GetFaceRecurrenceSettings(ctx)
		if err != nil {
			return store.Classify(err)
		}
		out = recurrenceFromRow(row)
		return nil
	})
	return out, err
}

// UpdateRecurrence validates and stores the rule and writes an audit entry with the old and new values.
// It needs the platform-level tenant.manage permission, because the rule applies to every tenant.
func (s *Service) UpdateRecurrence(ctx context.Context, actor authz.Actor, rule ClusterRule) (RecurrenceSettings, error) {
	if err := rule.Validate(); err != nil {
		return RecurrenceSettings{}, err
	}
	var out RecurrenceSettings
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		if err := recurrenceGate(ctx, q, actor); err != nil {
			return err
		}
		before, err := q.GetFaceRecurrenceSettings(ctx)
		if err != nil {
			return store.Classify(err)
		}
		row, err := q.UpdateFaceRecurrenceSettings(ctx, db.UpdateFaceRecurrenceSettingsParams{
			MinAppearances: int32(rule.MinAppearances), //nolint:gosec // validated to 1..100
			WindowDays:     int32(rule.WindowDays),     //nolint:gosec // validated to 1..365
			UpdatedBy:      &actor.UserID,
		})
		if err != nil {
			return store.Classify(err)
		}
		out = recurrenceFromRow(row)
		details, err := json.Marshal(map[string]any{
			"before": map[string]any{"min_appearances": before.MinAppearances, "window_days": before.WindowDays},
			"after":  map[string]any{"min_appearances": row.MinAppearances, "window_days": row.WindowDays},
		})
		if err != nil {
			return err
		}
		return q.InsertAudit(ctx, db.InsertAuditParams{
			ActorID: &actor.UserID, ActorName: actor.Username, Action: ActionRecurrenceUpdated,
			TargetType: "face_recurrence_settings", Details: details, RequestID: logging.RequestID(ctx),
		})
	})
	return out, err
}
