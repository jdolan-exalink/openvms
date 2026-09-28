package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ActionAccessDenied audits a denied (403) request (PRD §66 names no such action, so this
// follows the catalog's existing PAST_TENSE naming convention).
const ActionAccessDenied = "ACCESS_DENIED"

// auditDenied records one ACCESS_DENIED row for an authenticated actor refused by
// access.ErrForbidden. It runs in a transaction detached from the request's own — which may
// be about to roll back the denied write — so the audit row survives regardless, the same
// way internal/media's Gateway.audit does for its side effects. It never fails the request:
// a write failure is only logged. Unauthenticated (401) requests never reach here, since
// this is called only for errors returned by an authenticated handler.
func (h *Handlers) auditDenied(ctx context.Context, actor authz.Actor, err error, method, path string) {
	details := map[string]any{"method": method, "path": path}
	tenantID := actor.TenantID
	var targetType string
	var targetID *uuid.UUID
	var fe *access.ForbiddenError
	if errors.As(err, &fe) {
		details["permission"] = string(fe.Permission)
		if fe.Resource.Kind != "" {
			targetType = string(fe.Resource.Kind)
		}
		if fe.Resource.ID != uuid.Nil {
			id := fe.Resource.ID
			targetID = &id
		}
		if tenantID == nil && fe.Resource.TenantID != uuid.Nil {
			t := fe.Resource.TenantID
			tenantID = &t
		}
	}
	b, marshalErr := json.Marshal(details)
	if marshalErr != nil {
		h.Log.ErrorContext(ctx, "access-denied audit", "error", marshalErr)
		return
	}
	st := h.Inv.Store
	writeErr := st.Tx(context.WithoutCancel(ctx), store.ScopeFor(actor), func(q *db.Queries) error {
		return q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID: tenantID, ActorID: &actor.UserID, ActorName: actor.Username, Action: ActionAccessDenied,
			TargetType: targetType, TargetID: targetID, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: b,
		})
	})
	if writeErr != nil {
		h.Log.ErrorContext(ctx, "access-denied audit", "error", writeErr)
	}
}
