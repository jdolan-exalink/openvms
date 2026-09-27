package inventory

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type GrantFilter struct {
	SubjectType *string
	SubjectID   *uuid.UUID
}

// ListGrants returns grants whose scope the caller may manage.
func (s *Service) ListGrants(ctx context.Context, actor authz.Actor, f GrantFilter) ([]db.PermissionGrant, error) {
	var out []db.PermissionGrant
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		rows, err := q.ListGrants(ctx, db.ListGrantsParams{SubjectType: f.SubjectType, SubjectID: f.SubjectID, TenantID: actor.TenantID})
		if err != nil {
			return err
		}
		for _, g := range rows {
			res, _, err := access.ScopeResource(ctx, q, authz.ScopeType(g.ScopeType), deref(g.ScopeID))
			if err != nil {
				continue // scope object deleted or not visible
			}
			if c.Can(authz.PermissionsManage, res) {
				out = append(out, g)
			}
		}
		return nil
	})
	return out, err
}

type GrantInput struct {
	SubjectType string
	SubjectID   uuid.UUID
	Permission  authz.Permission
	Effect      authz.Effect
	ScopeType   authz.ScopeType
	ScopeID     *uuid.UUID
}

// CreateGrant requires permissions.manage on the scope and, for ALLOW, that the caller
// holds the permission there too, so nobody can hand out rights they do not have.
func (s *Service) CreateGrant(ctx context.Context, actor authz.Actor, in GrantInput) (db.PermissionGrant, error) {
	var out db.PermissionGrant
	if in.SubjectType != "user" && in.SubjectType != "group" {
		return out, invalid("subject_type must be user or group")
	}
	if in.Effect != authz.Allow && in.Effect != authz.Deny {
		return out, invalid("effect must be allow or deny")
	}
	if !authz.Grantable(in.Permission, in.ScopeType) {
		return out, invalid("%s cannot be granted at %s scope", in.Permission, in.ScopeType)
	}
	if (in.ScopeType == authz.ScopePlatform) != (in.ScopeID == nil) {
		return out, invalid("scope_id is required for every scope except platform")
	}
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		res, scopeTenant, err := access.ScopeResource(ctx, q, in.ScopeType, deref(in.ScopeID))
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return invalid("scope %s %s does not exist", in.ScopeType, deref(in.ScopeID))
			}
			return err
		}
		if in.ScopeType == authz.ScopePlatform && !actor.IsPlatform() {
			return access.ErrForbidden
		}
		if !actor.IsPlatform() && scopeTenant != *actor.TenantID {
			return invalid("scope %s %s does not exist", in.ScopeType, deref(in.ScopeID))
		}
		if err := c.Require(authz.PermissionsManage, res); err != nil {
			return err
		}
		if in.Effect == authz.Allow {
			if err := c.Require(in.Permission, res); err != nil {
				return err
			}
		}
		if err := checkSubject(ctx, q, actor, in, scopeTenant); err != nil {
			return err
		}
		var tenantID *uuid.UUID
		if in.ScopeType != authz.ScopePlatform {
			tenantID = &scopeTenant
		}
		out, err = q.CreateGrant(ctx, db.CreateGrantParams{
			TenantID: tenantID, SubjectType: in.SubjectType, SubjectID: in.SubjectID,
			Permission: string(in.Permission), Effect: string(in.Effect),
			ScopeType: string(in.ScopeType), ScopeID: in.ScopeID, CreatedBy: &actor.UserID,
		})
		if err != nil {
			return store.Classify(err)
		}
		return audit(ctx, q, actor, tenantID, ActionPermissionChanged, "grant", out.ID, map[string]any{
			"op": "grant", "subject_type": in.SubjectType, "subject_id": in.SubjectID, "permission": in.Permission,
			"effect": in.Effect, "scope_type": in.ScopeType, "scope_id": in.ScopeID,
		})
	})
	return out, err
}

// checkSubject makes sure the user or group exists and lives in the scope's tenant.
// Platform-level subjects (no tenant) may receive grants in any tenant.
func checkSubject(ctx context.Context, q *db.Queries, actor authz.Actor, in GrantInput, scopeTenant uuid.UUID) error {
	var subjectTenant *uuid.UUID
	switch in.SubjectType {
	case "user":
		u, err := q.GetUser(ctx, in.SubjectID)
		if err != nil {
			return invalid("user %s does not exist", in.SubjectID)
		}
		subjectTenant = u.TenantID
	case "group":
		g, err := q.GetUserGroup(ctx, in.SubjectID)
		if err != nil {
			return invalid("group %s does not exist", in.SubjectID)
		}
		subjectTenant = g.TenantID
	}
	if subjectTenant == nil {
		if !actor.IsPlatform() {
			return invalid("%s %s does not exist", in.SubjectType, in.SubjectID)
		}
		return nil
	}
	if in.ScopeType == authz.ScopePlatform || *subjectTenant != scopeTenant {
		return invalid("%s %s belongs to another tenant", in.SubjectType, in.SubjectID)
	}
	return nil
}

func (s *Service) DeleteGrant(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		g, err := q.GetGrant(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if !actor.IsPlatform() && (g.TenantID == nil || *g.TenantID != *actor.TenantID) {
			return store.ErrNotFound
		}
		res, _, err := access.ScopeResource(ctx, q, authz.ScopeType(g.ScopeType), deref(g.ScopeID))
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		// A grant on a deleted object can be revoked by whoever manages its tenant.
		if errors.Is(err, store.ErrNotFound) {
			if g.TenantID == nil {
				return store.ErrNotFound
			}
			res = access.Tenant(*g.TenantID)
		}
		if err := c.Require(authz.PermissionsManage, res); err != nil {
			return err
		}
		if err := q.DeleteGrant(ctx, id); err != nil {
			return err
		}
		return audit(ctx, q, actor, g.TenantID, ActionPermissionChanged, "grant", id, map[string]any{
			"op": "revoke", "subject_type": g.SubjectType, "subject_id": g.SubjectID, "permission": g.Permission,
			"effect": g.Effect, "scope_type": g.ScopeType, "scope_id": g.ScopeID,
		})
	})
}

// Me returns the actor's user row and the grants that apply to them.
func (s *Service) Me(ctx context.Context, actor authz.Actor) (db.User, []db.PermissionGrant, error) {
	var user db.User
	var grants []db.PermissionGrant
	err := s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		var err error
		user, err = q.GetUser(ctx, actor.UserID)
		if err != nil {
			return notFoundOr(err)
		}
		rows, err := q.ListGrantsForUser(ctx, actor.UserID)
		if err != nil {
			return err
		}
		for _, g := range rows {
			if actor.IsPlatform() || (g.TenantID != nil && *g.TenantID == *actor.TenantID && g.ScopeType != string(authz.ScopePlatform)) {
				grants = append(grants, g)
			}
		}
		return nil
	})
	return user, grants, err
}

func deref(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}
