package faces

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ClusterRule decides when a group of unknown faces is "recurrent": MinAppearances sightings inside the
// last WindowDays days. A recurrent group is the one the operator should name or dismiss.
type ClusterRule struct {
	MinAppearances int
	WindowDays     int
}

// DefaultClusterRule is three appearances in a week.
var DefaultClusterRule = ClusterRule{MinAppearances: 3, WindowDays: 7}

// UnknownCluster is a proposed person: pending sightings that look alike, with what the operator needs to
// decide quickly (how often, how recently, where).
type UnknownCluster struct {
	// ID is the oldest member's sighting ID, so it does not change as newer faces join the group.
	ID             uuid.UUID
	SightingIDs    []uuid.UUID
	IdentityIDs    []uuid.UUID
	SightingCount  int
	DistinctDays   int
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
	ServerIDs      []uuid.UUID
	ServerNames    []string
	BestSightingID uuid.UUID
	BestScore      *float64
	Recurrent      bool
}

// Sort orders for ListUnknownClusters.
const (
	ClusterSortRecurrence = "recurrence"
	ClusterSortRecent     = "recent"
)

// MaxClusterPage is the most groups one request returns.
const MaxClusterPage = 200

// ClusterPage selects a page of groups.
type ClusterPage struct {
	Limit  int
	Offset int
	// Sort is ClusterSortRecurrence (recurrent groups first, then the largest) or ClusterSortRecent.
	Sort string
}

// UnknownClusterPage is one page of groups plus how many groups match in all.
type UnknownClusterPage struct {
	Items []UnknownCluster
	Total int
}

// clusterBatch bounds one UPDATE when a pass stores the groups of a tenant.
const clusterBatch = 5000

// ListUnknownClusters pages the pending sightings the actor can read, grouped by the clusters the worker
// stored. A sighting the worker has not grouped yet is a group of one. Nothing changes here: the operator
// names, joins, watches or dismisses a group, and only then does anything change.
func (s *Service) ListUnknownClusters(ctx context.Context, actor authz.Actor, f TrainFilter, rule ClusterRule, page ClusterPage) (UnknownClusterPage, error) {
	if page.Limit < 1 || page.Limit > MaxClusterPage {
		page.Limit = MaxClusterPage
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	if page.Sort != ClusterSortRecent {
		page.Sort = ClusterSortRecurrence
	}
	var tenants []uuid.UUID
	var rows []db.ListFaceUnknownClustersRow
	if err := s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		q := db.New(tx)
		c, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if tenants, err = readableTenants(ctx, tx, c, actor, f.TenantID); err != nil {
			return err
		}
		rows, err = q.ListFaceUnknownClusters(ctx, db.ListFaceUnknownClustersParams{
			TenantIds: nonNil(tenants), SiteIds: nonNil(f.SiteIDs), ServerIds: nonNil(f.ServerIDs),
			WindowDays: int32(rule.WindowDays), MinAppearances: int32(rule.MinAppearances), //nolint:gosec // validated range 1-365 and 1-100.
			Sort: page.Sort, RowLimit: int32(page.Limit), RowOffset: int32(page.Offset), //nolint:gosec // bounded above.
		})
		return err
	}); err != nil {
		return UnknownClusterPage{}, err
	}
	out := UnknownClusterPage{Items: make([]UnknownCluster, 0, len(rows))}
	for _, r := range rows {
		out.Total = int(r.Total)
		u := UnknownCluster{
			ID: r.ID, SightingIDs: r.SightingIds, IdentityIDs: r.IdentityIds, SightingCount: int(r.SightingCount),
			DistinctDays: int(r.DistinctDays), FirstSeenAt: r.FirstSeenAt, LastSeenAt: r.LastSeenAt,
			BestSightingID: r.BestSightingID, Recurrent: r.Recurrent,
		}
		if r.HasScore {
			score := r.BestScore
			u.BestScore = &score
		}
		var servers []struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		}
		if err := json.Unmarshal(r.Servers, &servers); err != nil {
			return UnknownClusterPage{}, err
		}
		for _, sv := range servers {
			u.ServerIDs = append(u.ServerIDs, sv.ID)
			u.ServerNames = append(u.ServerNames, sv.Name)
		}
		out.Items = append(out.Items, u)
	}
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		for _, tenantID := range tenants {
			if err := s.audit(ctx, q, actor, tenantID, ActionSightingRead, "face_sighting", uuid.Nil,
				map[string]any{"scope": "clusters", "clusters": out.Total, "offset": page.Offset, "returned": len(out.Items)}); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// storedClusterIDs names each cluster by its oldest member and returns, for every sighting, the name of
// the cluster it belongs to. The name does not change when newer faces join.
func storedClusterIDs(clusters []Cluster, seen map[uuid.UUID]time.Time) (sightings, names []uuid.UUID) {
	for _, c := range clusters {
		if len(c.SightingIDs) == 0 {
			continue
		}
		oldest := c.SightingIDs[0]
		for _, id := range c.SightingIDs[1:] {
			if at, ot := seen[id], seen[oldest]; at.Before(ot) || (at.Equal(ot) && id.String() < oldest.String()) {
				oldest = id
			}
		}
		for _, id := range c.SightingIDs {
			sightings = append(sightings, id)
			names = append(names, oldest)
		}
	}
	return sightings, names
}

// RefreshUnknownClusters groups the pending sightings of every tenant that has embedded faces no pass has
// grouped yet, and stores the result so the faces page reads it instead of computing it. It returns how
// many tenants it grouped. Grouping uses the same similarity rules as before and runs outside any
// transaction, because it is the slow step.
func (s *Service) RefreshUnknownClusters(ctx context.Context) (int, error) {
	var tenants []uuid.UUID
	if err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		tenants, err = q.ListFaceClusterTenants(ctx)
		return err
	}); err != nil {
		return 0, err
	}
	done := 0
	for _, tenantID := range tenants {
		if err := s.refreshTenantClusters(ctx, tenantID); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

func (s *Service) refreshTenantClusters(ctx context.Context, tenantID uuid.UUID) error {
	var inputs []db.ListFaceClusterInputsRow
	var pairs []db.ListFaceDifferentSightingPairsRow
	if err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		if inputs, err = q.ListFaceClusterInputs(ctx, tenantID); err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(inputs))
		for i, in := range inputs {
			ids[i] = in.ID
		}
		pairs, err = q.ListFaceDifferentSightingPairs(ctx, db.ListFaceDifferentSightingPairsParams{TenantIds: []uuid.UUID{tenantID}, SightingIds: ids})
		return err
	}); err != nil {
		return err
	}
	candidates := make([]embeddedSighting, 0, len(inputs))
	identityOf := make(map[uuid.UUID]uuid.UUID, len(inputs))
	seen := make(map[uuid.UUID]time.Time, len(inputs))
	for _, in := range inputs {
		identityOf[in.ID] = in.IdentityID
		seen[in.ID] = in.SeenAt
		if in.EmbeddingProfile == nil || in.EmbeddingModel == nil {
			continue
		}
		candidates = append(candidates, embeddedSighting{ID: in.ID, IdentityID: in.IdentityID, Profile: *in.EmbeddingProfile, Model: *in.EmbeddingModel, Vector: in.Embedding})
	}
	ruledOut := make(pairSet, len(pairs))
	for _, p := range pairs {
		lo, okLo := identityOf[p.SightingLo]
		hi, okHi := identityOf[p.SightingHi]
		if okLo && okHi && lo != hi {
			ruledOut[orderedUUIDPair(lo, hi)] = struct{}{}
		}
	}
	sightings, names := storedClusterIDs(ClusterSightings(candidates, DefaultClusterThreshold, ruledOut), seen)
	return s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		for start := 0; start < len(sightings); start += clusterBatch {
			end := min(start+clusterBatch, len(sightings))
			if err := q.SetFaceSightingClusters(ctx, db.SetFaceSightingClustersParams{
				TenantID: tenantID, SightingIds: sightings[start:end], ClusterIds: names[start:end],
			}); err != nil {
				return err
			}
		}
		return nil
	})
}
