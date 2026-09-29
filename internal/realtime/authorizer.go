package realtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// LoadFunc returns the IDs of the resources of a kind on which actor holds permission. It
// must be the same query the list endpoints use.
type LoadFunc func(ctx context.Context, actor authz.Actor, kind authz.ScopeType, p authz.Permission) ([]uuid.UUID, error)

// StoreLoader loads authorized IDs from the database under the actor's tenant scope (RLS),
// exactly like events.Service.ListEvents does for cameras.
func StoreLoader(st *store.Store) LoadFunc {
	return func(ctx context.Context, actor authz.Actor, kind authz.ScopeType, p authz.Permission) ([]uuid.UUID, error) {
		var ids []uuid.UUID
		err := st.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
			c, err := access.Load(ctx, db.New(tx), actor)
			if err != nil {
				return err
			}
			switch kind {
			case authz.ScopeCamera:
				ids, err = c.CameraIDs(ctx, p)
			case authz.ScopeServer:
				ids, err = c.ServerIDs(ctx, p)
			case authz.ScopeSite:
				ids, err = c.SiteIDs(ctx, p)
			default:
				err = fmt.Errorf("realtime: unsupported scope kind %q", kind)
			}
			return err
		})
		return ids, err
	}
}

// CachedAuthorizer answers Allow from per-(user, kind, permission) ID sets that are reloaded
// after a TTL, so a burst of messages costs one query, and a permission change reaches open
// connections within the TTL. Errors are never cached.
type CachedAuthorizer struct {
	load LoadFunc
	ttl  time.Duration

	mu    sync.Mutex
	now   func() time.Time
	cache map[cacheKey]cacheEntry
}

type cacheKey struct {
	user uuid.UUID
	kind authz.ScopeType
	perm authz.Permission
}

type cacheEntry struct {
	ids     map[uuid.UUID]struct{}
	expires time.Time
}

const maxCacheEntries = 4096

func NewCachedAuthorizer(load LoadFunc, ttl time.Duration) *CachedAuthorizer {
	return &CachedAuthorizer{load: load, ttl: ttl, now: time.Now, cache: map[cacheKey]cacheEntry{}}
}

// SetClock replaces the time source (tests).
func (a *CachedAuthorizer) SetClock(now func() time.Time) {
	a.mu.Lock()
	a.now = now
	a.mu.Unlock()
}

func (a *CachedAuthorizer) Allow(ctx context.Context, actor authz.Actor, s Scope) (bool, error) {
	switch s.Kind {
	case authz.ScopeCamera, authz.ScopeServer, authz.ScopeSite:
	case authz.ScopeTenant:
		return actor.TenantID != nil && *actor.TenantID == s.ID, nil
	default:
		return false, fmt.Errorf("realtime: unsupported scope kind %q", s.Kind)
	}
	k := cacheKey{user: actor.UserID, kind: s.Kind, perm: s.Permission}
	a.mu.Lock()
	e, ok := a.cache[k]
	now := a.now()
	a.mu.Unlock()
	if !ok || !now.Before(e.expires) {
		ids, err := a.load(ctx, actor, s.Kind, s.Permission)
		if err != nil {
			return false, err
		}
		e = cacheEntry{ids: make(map[uuid.UUID]struct{}, len(ids)), expires: now.Add(a.ttl)}
		for _, id := range ids {
			e.ids[id] = struct{}{}
		}
		a.mu.Lock()
		if len(a.cache) >= maxCacheEntries {
			for key, v := range a.cache {
				if !now.Before(v.expires) {
					delete(a.cache, key)
				}
			}
			if len(a.cache) >= maxCacheEntries {
				a.cache = map[cacheKey]cacheEntry{}
			}
		}
		a.cache[k] = e
		a.mu.Unlock()
	}
	_, allowed := e.ids[s.ID]
	return allowed, nil
}
