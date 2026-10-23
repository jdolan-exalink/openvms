package rules

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// MaxFaceRefs bounds face_person_ids and face_watchlist_ids of one rule.
const MaxFaceRefs = 100

// DefaultFaceCacheTTL is how long a tenant's persons and watchlist membership stay cached. A
// watchlist edit made through another process (the API) reaches the worker within this time.
const DefaultFaceCacheTTL = 15 * time.Second

// HasFaceConditions reports whether the rule restricts which recognized persons it matches.
func (c Conditions) HasFaceConditions() bool {
	return len(c.FacePersonIDs) > 0 || len(c.FaceWatchlistIDs) > 0
}

// MatchesFaces reports whether any recognized sub_label is one of the wanted Frigate names.
// Frigate reports the exact frigate_name of the person, so the comparison is exact. An empty
// set (a list that resolves to nobody) matches nothing.
func MatchesFaces(subLabels []string, names map[string]struct{}) bool {
	for _, l := range subLabels {
		if _, ok := names[l]; ok {
			return true
		}
	}
	return false
}

// FaceResolver resolves the persons and watchlists of a face condition to the set of Frigate
// names (event sub_labels) they stand for. Unknown, deleted or foreign ids resolve to nothing.
// q is the caller's tenant-scoped querier: a caller already inside a transaction passes it so the
// resolver does not take a second pool connection; nil makes the resolver open its own.
type FaceResolver interface {
	FaceNames(ctx context.Context, q *db.Queries, tenantID uuid.UUID, personIDs, watchlistIDs []uuid.UUID) (map[string]struct{}, error)
}

// cachedFaceResolver can resolve against a valid in-memory snapshot without querying PostgreSQL.
// The caller can then avoid a savepoint, which is needed only to isolate a potentially failing read.
type cachedFaceResolver interface {
	faceNamesFromCache(tenantID uuid.UUID, personIDs, watchlistIDs []uuid.UUID) (map[string]struct{}, bool)
}

// faceIndex is the snapshot of one tenant: live persons by id and watchlist membership.
type faceIndex struct {
	loaded  time.Time
	names   map[uuid.UUID]string
	members map[uuid.UUID][]uuid.UUID
}

// StoreFaceResolver resolves face conditions from PostgreSQL, caching one snapshot per tenant
// (two small queries) so evaluating every event of a busy tenant does not hit the database.
type StoreFaceResolver struct {
	Store *store.Store
	// TTL is the snapshot lifetime (DefaultFaceCacheTTL when zero).
	TTL time.Duration
	// Now is the clock (time.Now when nil).
	Now func() time.Time

	mu    sync.Mutex
	cache map[uuid.UUID]faceIndex
}

func NewStoreFaceResolver(st *store.Store) *StoreFaceResolver {
	if st == nil {
		return nil
	}
	return &StoreFaceResolver{Store: st}
}

// Expire drops every cached snapshot.
func (r *StoreFaceResolver) Expire() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = nil
}

func (r *StoreFaceResolver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *StoreFaceResolver) ttl() time.Duration {
	if r.TTL > 0 {
		return r.TTL
	}
	return DefaultFaceCacheTTL
}

func (r *StoreFaceResolver) index(ctx context.Context, q *db.Queries, tenantID uuid.UUID) (faceIndex, error) {
	r.mu.Lock()
	cached, ok := r.cache[tenantID]
	r.mu.Unlock()
	if ok && r.now().Sub(cached.loaded) < r.ttl() {
		return cached, nil
	}
	idx := faceIndex{loaded: r.now(), names: map[uuid.UUID]string{}, members: map[uuid.UUID][]uuid.UUID{}}
	load := func(q *db.Queries) error {
		persons, err := q.ListFaceNameIndex(ctx, tenantID)
		if err != nil {
			return err
		}
		for _, p := range persons {
			idx.names[p.ID] = p.FrigateName
		}
		members, err := q.ListFaceMembershipIndex(ctx, tenantID)
		if err != nil {
			return err
		}
		for _, m := range members {
			idx.members[m.WatchlistID] = append(idx.members[m.WatchlistID], m.PersonID)
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
		return faceIndex{}, err
	}
	r.mu.Lock()
	if r.cache == nil {
		r.cache = map[uuid.UUID]faceIndex{}
	}
	r.cache[tenantID] = idx
	r.mu.Unlock()
	return idx, nil
}

// faceNamesFromCache resolves conditions only when a current tenant snapshot is already cached.
// It returns hit=false when the caller must use the transaction-isolated database path.
func (r *StoreFaceResolver) faceNamesFromCache(tenantID uuid.UUID, personIDs, watchlistIDs []uuid.UUID) (map[string]struct{}, bool) {
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
	return faceNamesFromIndex(idx, personIDs, watchlistIDs), true
}

func (r *StoreFaceResolver) FaceNames(ctx context.Context, q *db.Queries, tenantID uuid.UUID, personIDs, watchlistIDs []uuid.UUID) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	if r == nil || r.Store == nil {
		return out, nil
	}
	idx, err := r.index(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	return faceNamesFromIndex(idx, personIDs, watchlistIDs), nil
}

func faceNamesFromIndex(idx faceIndex, personIDs, watchlistIDs []uuid.UUID) map[string]struct{} {
	out := map[string]struct{}{}
	add := func(id uuid.UUID) {
		if name, ok := idx.names[id]; ok {
			out[name] = struct{}{}
		}
	}
	for _, id := range personIDs {
		add(id)
	}
	for _, list := range watchlistIDs {
		for _, id := range idx.members[list] {
			add(id)
		}
	}
	return out
}
