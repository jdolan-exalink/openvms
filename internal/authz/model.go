// Package authz implements the VMS authorization model: permission + scope, descending
// inheritance and DENY over ALLOW (PRD §27-31). Every endpoint checks here, server-side.
package authz

import "github.com/google/uuid"

type Permission string

type ScopeType string

const (
	ScopePlatform    ScopeType = "platform"
	ScopeTenant      ScopeType = "tenant"
	ScopeSite        ScopeType = "site"
	ScopeServer      ScopeType = "server"
	ScopeCameraGroup ScopeType = "camera_group"
	ScopeCamera      ScopeType = "camera"
)

// Scope identifies where a grant applies. ID is uuid.Nil for ScopePlatform.
type Scope struct {
	Type ScopeType
	ID   uuid.UUID
}

type Effect string

const (
	Allow Effect = "allow"
	Deny  Effect = "deny"
)

type Grant struct {
	Permission Permission
	Effect     Effect
	Scope      Scope
}

// Actor is the authenticated principal making a request.
type Actor struct {
	UserID   uuid.UUID
	Username string
	// TenantID is nil for platform-level users.
	TenantID *uuid.UUID
	// SessionID is set when the request is authenticated by a browser session.
	SessionID *uuid.UUID
}

func (a Actor) IsPlatform() bool { return a.TenantID == nil }

// Resource is anything a permission can be checked against, with its ancestry.
// Fill only the fields that apply to Kind; Covering derives the scopes from them.
type Resource struct {
	Kind     ScopeType
	ID       uuid.UUID
	TenantID uuid.UUID
	SiteID   uuid.UUID
	ServerID uuid.UUID
	// GroupIDs lists the camera groups a camera belongs to.
	GroupIDs []uuid.UUID
}

// Covering returns every scope whose grants apply to r, from broadest to narrowest.
// Inheritance is descending: a grant on a site covers its servers and their cameras.
func (r Resource) Covering() []Scope {
	s := []Scope{{Type: ScopePlatform}}
	if r.Kind == ScopePlatform {
		return s
	}
	s = append(s, Scope{ScopeTenant, r.TenantID})
	switch r.Kind {
	case ScopeTenant:
	case ScopeSite:
		s = append(s, Scope{ScopeSite, r.ID})
	case ScopeServer:
		s = append(s, Scope{ScopeSite, r.SiteID}, Scope{ScopeServer, r.ID})
	case ScopeCameraGroup:
		s = append(s, Scope{ScopeCameraGroup, r.ID})
	case ScopeCamera:
		s = append(s, Scope{ScopeSite, r.SiteID}, Scope{ScopeServer, r.ServerID})
		for _, g := range r.GroupIDs {
			s = append(s, Scope{ScopeCameraGroup, g})
		}
		s = append(s, Scope{ScopeCamera, r.ID})
	}
	return s
}

// Decide applies the rules to grants already loaded for one subject:
// allowed only if some ALLOW covers the resource and no DENY does.
func Decide(grants []Grant, perm Permission, covering []Scope) bool {
	in := make(map[Scope]bool, len(covering))
	for _, s := range covering {
		in[s] = true
	}
	allowed := false
	for _, g := range grants {
		if g.Permission != perm || !in[g.Scope] {
			continue
		}
		if g.Effect == Deny {
			return false
		}
		allowed = true
	}
	return allowed
}
