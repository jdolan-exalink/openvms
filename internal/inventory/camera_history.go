package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// History and rollback of a camera's configuration (PRD §32). The audit trail already holds,
// for every change, the previous and new values, the origin and the result; the history is a
// view of it and a rollback is a new change built from the old values, sent through the normal
// review/apply pipeline.

// HistoryChange is one field of a recorded change.
type HistoryChange struct {
	Path string
	From any
	To   any
}

// ConfigHistoryEntry is one recorded change of a camera's configuration.
type ConfigHistoryEntry struct {
	ID       int64
	At       time.Time
	UserID   *uuid.UUID
	UserName string
	Origin   string
	Result   string
	Section  string
	Changes  []HistoryChange
	// Error is the failure in plain words, when the change failed.
	Error *camconfig.PlainError
	// RollbackOf is the audit id this change undid; RolledBackBy the id of the change that
	// undid this one (0: none).
	RollbackOf   int64
	RolledBackBy int64
	// Rollbackable says the change can be undone from its recorded old values; when it cannot,
	// RollbackReason says why.
	Rollbackable   bool
	RollbackReason string
}

const (
	defaultHistory = 20
	maxHistory     = 100
)

type auditRecord struct {
	id      int64
	at      time.Time
	userID  *uuid.UUID
	name    string
	details map[string]any
}

// cameraConfigAudit reads the CAMERA_CONFIG_CHANGED rows of one camera, newest first. A single
// row is selected when auditID is not zero.
func (s *Service) cameraConfigAudit(ctx context.Context, actor authz.Actor, cameraID uuid.UUID, auditID int64, limit int) ([]auditRecord, error) {
	var out []auditRecord
	err := s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, occurred_at, actor_id, actor_name, details
			FROM audit_log
			WHERE action = $1 AND target_type = 'camera' AND target_id = $2 AND ($3::bigint = 0 OR id = $3)
			ORDER BY id DESC LIMIT $4`, ActionCameraConfigChanged, cameraID, auditID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r auditRecord
			var raw []byte
			if err := rows.Scan(&r.id, &r.at, &r.userID, &r.name, &raw); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &r.details); err != nil {
				r.details = map[string]any{}
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

func mapOf(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }

func intOf(m map[string]any, k string) int64 { f, _ := m[k].(float64); return int64(f) }

func historyEntry(r auditRecord) ConfigHistoryEntry {
	e := ConfigHistoryEntry{
		ID: r.id, At: r.at, UserID: r.userID, UserName: r.name, Origin: str(r.details, "origin"),
		Result: str(r.details, "result"), Section: str(r.details, "section"), RollbackOf: intOf(r.details, "rollback_of"),
		Changes: []HistoryChange{},
	}
	prev, next := mapOf(r.details, "previous_values"), mapOf(r.details, "new_values")
	paths := map[string]bool{}
	for p := range prev {
		paths[p] = true
	}
	for p := range next {
		paths[p] = true
	}
	for p := range paths {
		e.Changes = append(e.Changes, HistoryChange{Path: p, From: prev[p], To: next[p]})
	}
	sort.Slice(e.Changes, func(i, j int) bool { return e.Changes[i].Path < e.Changes[j].Path })
	if pm := mapOf(r.details, "error_plain"); pm != nil {
		e.Error = &camconfig.PlainError{Message: str(pm, "message"), Technical: str(pm, "technical")}
	}
	return e
}

// ListCameraConfigHistory lists the recent changes of a camera's configuration, newest first.
// It needs the permission to change it (servers.config), like the review.
func (s *Service) ListCameraConfigHistory(ctx context.Context, actor authz.Actor, id uuid.UUID, limit int) ([]ConfigHistoryEntry, error) {
	if err := s.requireHistory(ctx, actor, id); err != nil {
		return nil, err
	}
	switch {
	case limit <= 0:
		limit = defaultHistory
	case limit > maxHistory:
		limit = maxHistory
	}
	records, err := s.cameraConfigAudit(ctx, actor, id, 0, limit)
	if err != nil {
		return nil, err
	}
	return classifyHistory(records), nil
}

// classifyHistory turns audit rows (newest first) into entries and marks which ones can still
// be rolled back: the recorded old values must be restorable and no later change may already
// have undone it.
func classifyHistory(records []auditRecord) []ConfigHistoryEntry {
	out := make([]ConfigHistoryEntry, 0, len(records))
	undone := map[int64]int64{}
	for _, r := range records {
		if of := intOf(r.details, "rollback_of"); of != 0 {
			if _, seen := undone[of]; !seen {
				undone[of] = r.id
			}
		}
	}
	for _, r := range records {
		e := historyEntry(r)
		e.RolledBackBy = undone[r.id]
		ok, why := camconfig.Rollbackable(camconfig.RollbackSource{Origin: e.Origin, Result: e.Result, Previous: mapOf(r.details, "previous_values")})
		if ok && e.RolledBackBy != 0 {
			ok, why = false, "el cambio ya fue restaurado"
		}
		e.Rollbackable, e.RollbackReason = ok, why
		out = append(out, e)
	}
	return out
}

func (s *Service) requireHistory(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		t, err := s.loadConfigTarget(ctx, id, q, c, camconfig.Patch{})
		if err != nil {
			return err
		}
		// History shows other people's names and the old and new values: the camera must be
		// visible to the reader, not only its server configurable.
		if err := c.Require(authz.CamerasView, cameraResource(t.cam)); err != nil {
			return err
		}
		return c.RequireCompat(authz.CamerasConfigView, cameraResource(t.cam))
	})
}

// RollbackInput selects the change to undo: AuditID 0 means the latest change that can be
// undone, leaving out earlier rollbacks.
type RollbackInput struct {
	AuditID          int64
	ExpectedRevision int64
}

// RollbackResult is the outcome of a rollback: the change it undid and the apply it produced.
type RollbackResult struct {
	AuditID int64
	Apply   ConfigApplyResult
}

// RollbackCameraConfig restores the old values of a recorded change by applying them as a new
// change through the normal pipeline (review, permissions per section, desired state, audit).
// The audit row of the rollback names the change it undid. It refuses, without writing, when
// the old values are not available or not restorable.
func (s *Service) RollbackCameraConfig(ctx context.Context, actor authz.Actor, id uuid.UUID, in RollbackInput, link VideoLinker) (RollbackResult, error) {
	if err := s.requireHistory(ctx, actor, id); err != nil {
		return RollbackResult{}, err
	}
	var entry ConfigHistoryEntry
	var prev map[string]any
	if in.AuditID != 0 {
		recs, err := s.cameraConfigAudit(ctx, actor, id, in.AuditID, 1)
		if err != nil {
			return RollbackResult{}, err
		}
		if len(recs) == 0 {
			return RollbackResult{}, store.ErrNotFound
		}
		// The undone-by check needs the newer rows too.
		all, err := s.cameraConfigAudit(ctx, actor, id, 0, maxHistory)
		if err != nil {
			return RollbackResult{}, err
		}
		for _, e := range classifyHistory(all) {
			if e.ID == in.AuditID {
				entry = e
			}
		}
		if entry.ID == 0 { // older than the window: classify the row alone
			entry = classifyHistory(recs)[0]
		}
		prev = mapOf(recs[0].details, "previous_values")
	} else {
		all, err := s.cameraConfigAudit(ctx, actor, id, 0, maxHistory)
		if err != nil {
			return RollbackResult{}, err
		}
		found := false
		for i, e := range classifyHistory(all) {
			// A rollback is not a default target: repeating "restore" walks back through the
			// person's changes instead of redoing the last restore.
			if e.Rollbackable && e.RollbackOf == 0 {
				entry, prev, found = e, mapOf(all[i].details, "previous_values"), true
				break
			}
		}
		if !found {
			return RollbackResult{}, invalid("no hay un cambio reciente que se pueda restaurar")
		}
	}
	if !entry.Rollbackable {
		return RollbackResult{}, invalid("no se puede restaurar la configuración anterior: %s", entry.RollbackReason)
	}
	patch, err := camconfig.InversePatch(prev)
	if err != nil {
		var ve *camconfig.ValidationError
		if errors.As(err, &ve) {
			return RollbackResult{}, invalid("%s", ve.Msg)
		}
		return RollbackResult{}, err
	}
	res, err := s.ApplyCameraConfigWith(ctx, actor, id, ConfigApplyInput{
		Patch: patch, ExpectedRevision: in.ExpectedRevision, Origin: camconfig.OriginUI, RollbackOf: entry.ID,
	}, link)
	if err != nil {
		return RollbackResult{}, err
	}
	return RollbackResult{AuditID: entry.ID, Apply: res}, nil
}
