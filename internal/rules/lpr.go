package rules

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// MaxLprRefs bounds lpr_watchlist_ids of one rule.
const MaxLprRefs = 100

// DefaultLprCacheTTL is how long a tenant's watchlist plate membership stays cached.
const DefaultLprCacheTTL = 15 * time.Second

// HasLprConditions reports whether the rule restricts which plates it matches.
func (c Conditions) HasLprConditions() bool {
	return len(c.LprWatchlistIDs) > 0
}

// MatchesPlates reports whether any plate matches the wanted plates set.
// Plates are compared uppercase and trimmed. An empty set matches nothing.
func MatchesPlates(plates []string, wanted map[string]struct{}) bool {
	if len(wanted) == 0 {
		return false
	}
	for _, p := range plates {
		norm := strings.ToUpper(strings.TrimSpace(p))
		if _, ok := wanted[norm]; ok {
			return true
		}
	}
	return false
}

// LprResolver resolves the watchlists of an LPR condition to the set of normalized plate strings
// they stand for.
type LprResolver interface {
	LprPlates(ctx context.Context, q *db.Queries, tenantID uuid.UUID, watchlistIDs []uuid.UUID) (map[string]struct{}, error)
}

// cachedLprResolver can resolve against a valid in-memory snapshot without querying PostgreSQL.
type cachedLprResolver interface {
	lprPlatesFromCache(tenantID uuid.UUID, watchlistIDs []uuid.UUID) (map[string]struct{}, bool)
}

// lprIndex is the snapshot of one tenant's watchlist membership: list id -> list of uppercase plates.
type lprIndex struct {
	loaded  time.Time
	members map[uuid.UUID][]string
}

// StoreLprResolver resolves LPR conditions from PostgreSQL, caching snapshots per tenant.
type StoreLprResolver struct {
	Store *store.Store
	// TTL is the snapshot lifetime (DefaultLprCacheTTL when zero).
	TTL time.Duration
	// Now is the clock (time.Now when nil).
	Now func() time.Time

	mu    sync.Mutex
	cache map[uuid.UUID]lprIndex
}

func NewStoreLprResolver(st *store.Store) *StoreLprResolver {
	if st == nil {
		return nil
	}
	return &StoreLprResolver{Store: st}
}

// Expire drops every cached snapshot.
func (r *StoreLprResolver) Expire() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = nil
}

func (r *StoreLprResolver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *StoreLprResolver) ttl() time.Duration {
	if r.TTL > 0 {
		return r.TTL
	}
	return DefaultLprCacheTTL
}

func (r *StoreLprResolver) index(ctx context.Context, q *db.Queries, tenantID uuid.UUID) (lprIndex, error) {
	r.mu.Lock()
	cached, ok := r.cache[tenantID]
	r.mu.Unlock()
	if ok && r.now().Sub(cached.loaded) < r.ttl() {
		return cached, nil
	}
	idx := lprIndex{loaded: r.now(), members: map[uuid.UUID][]string{}}
	load := func(q *db.Queries) error {
		items, err := q.ListLprWatchlistPlatesIndex(ctx, tenantID)
		if err != nil {
			return err
		}
		for _, item := range items {
			norm := strings.ToUpper(strings.TrimSpace(item.Plate))
			idx.members[item.WatchlistID] = append(idx.members[item.WatchlistID], norm)
		}
		return nil
	}
	var err error
	if q != nil {
		err = load(q)
	} else {
		err = r.Store.Tx(ctx, store.ScopeFor(authz.Actor{TenantID: &tenantID}), load)
	}
	if err != nil {
		return lprIndex{}, err
	}
	r.mu.Lock()
	if r.cache == nil {
		r.cache = map[uuid.UUID]lprIndex{}
	}
	r.cache[tenantID] = idx
	r.mu.Unlock()
	return idx, nil
}

func (r *StoreLprResolver) lprPlatesFromCache(tenantID uuid.UUID, watchlistIDs []uuid.UUID) (map[string]struct{}, bool) {
	if r == nil || r.Store == nil {
		return map[string]struct{}{}, true
	}
	now := r.now()
	r.mu.Lock()
	idx, ok := r.cache[tenantID]
	r.mu.Unlock()
	if !ok || now.Sub(idx.loaded) >= r.ttl() {
		return nil, false
	}
	return lprPlatesFromIndex(idx, watchlistIDs), true
}

func (r *StoreLprResolver) LprPlates(ctx context.Context, q *db.Queries, tenantID uuid.UUID, watchlistIDs []uuid.UUID) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	if r == nil || r.Store == nil {
		return out, nil
	}
	idx, err := r.index(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	return lprPlatesFromIndex(idx, watchlistIDs), nil
}

func lprPlatesFromIndex(idx lprIndex, watchlistIDs []uuid.UUID) map[string]struct{} {
	out := map[string]struct{}{}
	for _, listID := range watchlistIDs {
		for _, plate := range idx.members[listID] {
			out[plate] = struct{}{}
		}
	}
	return out
}
