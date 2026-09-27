// Package inventory implements tenants, sites, Frigate servers, cameras, camera groups
// and permission grants. Every operation authorizes the actor before touching data.
package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ValidationError is returned for requests that are well-formed but not acceptable.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// FrigateError wraps a failure to reach or authenticate against a Frigate server.
type FrigateError struct{ Err error }

func (e *FrigateError) Error() string { return "frigate: " + e.Err.Error() }
func (e *FrigateError) Unwrap() error { return e.Err }

// Connector opens an adapter; tests replace it.
type Connector func(ctx context.Context, info frigate.ConnInfo) (frigate.Adapter, error)

type Service struct {
	Store   *store.Store
	Sealer  *secrets.Sealer
	Log     *slog.Logger
	Connect Connector
}

func New(st *store.Store, sealer *secrets.Sealer, log *slog.Logger) *Service {
	return &Service{Store: st, Sealer: sealer, Log: log, Connect: frigate.Connect}
}

// tx opens a transaction scoped to the actor's tenants with their grants loaded.
func (s *Service) tx(ctx context.Context, actor authz.Actor, fn func(q *db.Queries, c *access.Checker) error) error {
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		c, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		return fn(q, c)
	})
}

// Audit actions (PRD §66).
const (
	ActionTenantCreated      = "TENANT_CREATED"
	ActionSiteCreated        = "SITE_CREATED"
	ActionSiteUpdated        = "SITE_UPDATED"
	ActionSiteRemoved        = "SITE_REMOVED"
	ActionServerAdded        = "SERVER_ADDED"
	ActionServerUpdated      = "SERVER_UPDATED"
	ActionServerRemoved      = "SERVER_REMOVED"
	ActionServerSynced       = "SERVER_SYNCED"
	ActionCameraUpdated      = "CAMERA_UPDATED"
	ActionCameraGroupChanged = "CAMERA_GROUP_CHANGED"
	ActionPermissionChanged  = "PERMISSION_CHANGED"
)

func audit(ctx context.Context, q *db.Queries, actor authz.Actor, tenantID *uuid.UUID, action, targetType string, targetID uuid.UUID, details map[string]any) error {
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
		TargetType: targetType,
		TargetID:   &targetID,
		RequestID:  logging.RequestID(ctx),
		Ip:         httpx.ClientIP(ctx),
		Details:    b,
	})
}

// notFoundOr turns a missing row into store.ErrNotFound and passes other errors through.
func notFoundOr(err error) error {
	err = store.Classify(err)
	if errors.Is(err, store.ErrNotFound) {
		return store.ErrNotFound
	}
	return err
}

func ptr[T any](v T) *T { return &v }
