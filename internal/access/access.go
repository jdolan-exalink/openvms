// Package access loads an actor's grants from the database and answers authorization
// questions for single resources (authz.Decide) and for lists (SQL filters).
package access

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ErrForbidden means the actor lacks the permission on the resource.
var ErrForbidden = errors.New("forbidden")

// ForbiddenError wraps ErrForbidden with the permission and resource that were checked, so
// a caller can audit exactly what was denied (PRD §66) without re-deriving it. errors.Is
// against ErrForbidden still matches it.
type ForbiddenError struct {
	Permission authz.Permission
	Resource   authz.Resource
}

func (e *ForbiddenError) Error() string { return "forbidden" }
func (e *ForbiddenError) Unwrap() error { return ErrForbidden }

// Checker is built once per transaction for one actor.
type Checker struct {
	Actor  authz.Actor
	q      *db.Queries
	grants []authz.Grant
}

// Load reads every grant that reaches the actor. Grants outside the actor's tenant are
// ignored, so a misconfigured grant can never widen a tenant user's reach.
func Load(ctx context.Context, q *db.Queries, actor authz.Actor) (*Checker, error) {
	rows, err := q.ListGrantsForUser(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	c := &Checker{Actor: actor, q: q}
	for _, g := range rows {
		if !actor.IsPlatform() {
			if g.TenantID == nil || *g.TenantID != *actor.TenantID || g.ScopeType == string(authz.ScopePlatform) {
				continue
			}
		}
		scope := authz.Scope{Type: authz.ScopeType(g.ScopeType)}
		if g.ScopeID != nil {
			scope.ID = *g.ScopeID
		}
		c.grants = append(c.grants, authz.Grant{Permission: authz.Permission(g.Permission), Effect: authz.Effect(g.Effect), Scope: scope})
	}
	return c, nil
}

// Grants returns the grants considered for this actor.
func (c *Checker) Grants() []authz.Grant { return c.grants }

func (c *Checker) Can(p authz.Permission, r authz.Resource) bool {
	return authz.Decide(c.grants, p, r.Covering())
}

// Require returns a *ForbiddenError (matching ErrForbidden via errors.Is) unless the actor
// has p on r.
func (c *Checker) Require(p authz.Permission, r authz.Resource) error {
	if !c.Can(p, r) {
		return &ForbiddenError{Permission: p, Resource: r}
	}
	return nil
}

func (c *Checker) CameraIDs(ctx context.Context, p authz.Permission) ([]uuid.UUID, error) {
	return c.q.AuthorizedCameraIDs(ctx, db.AuthorizedCameraIDsParams{UserID: c.Actor.UserID, Permission: string(p)})
}

func (c *Checker) ServerIDs(ctx context.Context, p authz.Permission) ([]uuid.UUID, error) {
	return c.q.AuthorizedServerIDs(ctx, db.AuthorizedServerIDsParams{UserID: c.Actor.UserID, Permission: string(p)})
}

func (c *Checker) SiteIDs(ctx context.Context, p authz.Permission) ([]uuid.UUID, error) {
	return c.q.AuthorizedSiteIDs(ctx, db.AuthorizedSiteIDsParams{UserID: c.Actor.UserID, Permission: string(p)})
}

// Tenant, Site, Server, Camera and CameraGroup build resources with their ancestry.

func Tenant(id uuid.UUID) authz.Resource {
	return authz.Resource{Kind: authz.ScopeTenant, ID: id, TenantID: id}
}

func Site(tenantID, id uuid.UUID) authz.Resource {
	return authz.Resource{Kind: authz.ScopeSite, ID: id, TenantID: tenantID}
}

func Server(tenantID, siteID, id uuid.UUID) authz.Resource {
	return authz.Resource{Kind: authz.ScopeServer, ID: id, TenantID: tenantID, SiteID: siteID}
}

func Camera(tenantID, siteID, serverID, id uuid.UUID, groups []uuid.UUID) authz.Resource {
	return authz.Resource{Kind: authz.ScopeCamera, ID: id, TenantID: tenantID, SiteID: siteID, ServerID: serverID, GroupIDs: groups}
}

func CameraGroup(tenantID, id uuid.UUID) authz.Resource {
	return authz.Resource{Kind: authz.ScopeCameraGroup, ID: id, TenantID: tenantID}
}

// ScopeResource resolves a grant scope to a resource so the granter's own rights on it
// can be checked. It returns store.ErrNotFound when the scope object is not visible.
func ScopeResource(ctx context.Context, q *db.Queries, t authz.ScopeType, id uuid.UUID) (authz.Resource, uuid.UUID, error) {
	switch t {
	case authz.ScopePlatform:
		return authz.Resource{Kind: authz.ScopePlatform}, uuid.Nil, nil
	case authz.ScopeTenant:
		tn, err := q.GetTenant(ctx, id)
		if err != nil {
			return authz.Resource{}, uuid.Nil, store.Classify(err)
		}
		return Tenant(tn.ID), tn.ID, nil
	case authz.ScopeSite:
		s, err := q.GetSite(ctx, id)
		if err != nil {
			return authz.Resource{}, uuid.Nil, store.Classify(err)
		}
		return Site(s.TenantID, s.ID), s.TenantID, nil
	case authz.ScopeServer:
		s, err := q.GetServer(ctx, id)
		if err != nil {
			return authz.Resource{}, uuid.Nil, store.Classify(err)
		}
		return Server(s.TenantID, s.SiteID, s.ID), s.TenantID, nil
	case authz.ScopeCameraGroup:
		g, err := q.GetCameraGroup(ctx, id)
		if err != nil {
			return authz.Resource{}, uuid.Nil, store.Classify(err)
		}
		return CameraGroup(g.TenantID, g.ID), g.TenantID, nil
	case authz.ScopeCamera:
		c, err := q.GetCamera(ctx, id)
		if err != nil {
			return authz.Resource{}, uuid.Nil, store.Classify(err)
		}
		return Camera(c.TenantID, c.SiteID, c.ServerID, c.ID, c.GroupIds), c.TenantID, nil
	}
	return authz.Resource{}, uuid.Nil, store.ErrNotFound
}
