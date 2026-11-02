package faces

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

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

func seenAt(r db.ListFaceReviewSightingsRow) time.Time {
	if r.SourceSeenAt != nil {
		return *r.SourceSeenAt
	}
	return r.FirstSeenAt
}

// SummarizeClusters describes each cluster from the sighting rows. Sightings that belong to no cluster
// (no embedding yet) become a cluster of one, so nothing disappears from review. The result is ordered by
// size, then by how recently the group was seen.
func SummarizeClusters(clusters []Cluster, rows []db.ListFaceReviewSightingsRow, rule ClusterRule, now time.Time) []UnknownCluster {
	byID := make(map[uuid.UUID]db.ListFaceReviewSightingsRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	covered := make(map[uuid.UUID]struct{}, len(rows))
	all := make([]Cluster, 0, len(clusters)+len(rows))
	for _, c := range clusters {
		all = append(all, c)
		for _, id := range c.SightingIDs {
			covered[id] = struct{}{}
		}
	}
	for _, r := range rows {
		if _, ok := covered[r.ID]; !ok {
			all = append(all, Cluster{SightingIDs: []uuid.UUID{r.ID}, IdentityIDs: []uuid.UUID{r.IdentityID}})
		}
	}
	windowStart := now.Add(-time.Duration(rule.WindowDays) * 24 * time.Hour)
	out := make([]UnknownCluster, 0, len(all))
	for _, c := range all {
		u := UnknownCluster{IdentityIDs: c.IdentityIDs}
		days := map[string]struct{}{}
		servers := map[uuid.UUID]struct{}{}
		inWindow := 0
		var oldest db.ListFaceReviewSightingsRow
		var haveOldest bool
		for _, id := range c.SightingIDs {
			r, ok := byID[id]
			if !ok {
				continue
			}
			u.SightingIDs = append(u.SightingIDs, id)
			at := seenAt(r)
			days[at.UTC().Format("2006-01-02")] = struct{}{}
			if !at.Before(windowStart) {
				inWindow++
			}
			if u.FirstSeenAt.IsZero() || at.Before(u.FirstSeenAt) {
				u.FirstSeenAt = at
			}
			if at.After(u.LastSeenAt) {
				u.LastSeenAt = at
			}
			if !haveOldest || at.Before(seenAt(oldest)) {
				oldest, haveOldest = r, true
			}
			if _, seen := servers[r.ServerID]; !seen {
				servers[r.ServerID] = struct{}{}
				u.ServerIDs = append(u.ServerIDs, r.ServerID)
				u.ServerNames = append(u.ServerNames, r.ServerName)
			}
			if r.SourceScore != nil && (u.BestScore == nil || *r.SourceScore > *u.BestScore) {
				score := *r.SourceScore
				u.BestScore, u.BestSightingID = &score, id
			}
		}
		if !haveOldest {
			continue
		}
		u.ID = oldest.ID
		if u.BestSightingID == uuid.Nil {
			u.BestSightingID = oldest.ID
		}
		u.SightingCount = len(u.SightingIDs)
		u.DistinctDays = len(days)
		u.Recurrent = rule.MinAppearances > 0 && inWindow >= rule.MinAppearances
		out = append(out, u)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SightingCount != out[j].SightingCount {
			return out[i].SightingCount > out[j].SightingCount
		}
		return out[i].LastSeenAt.After(out[j].LastSeenAt)
	})
	return out
}

// ListUnknownClusters groups the pending sightings the actor can read by face similarity. It stores
// nothing: the operator names, joins, watches or dismisses a group, and only then does anything change.
func (s *Service) ListUnknownClusters(ctx context.Context, actor authz.Actor, f TrainFilter, rule ClusterRule) ([]UnknownCluster, error) {
	d, err := s.loadReviewData(ctx, actor, f)
	if err != nil {
		return nil, err
	}
	clusters := ClusterSightings(embeddedCandidates(d.embeddings), DefaultClusterThreshold, d.ruledOut)
	out := SummarizeClusters(clusters, d.rows, rule, time.Now())
	counts := make(map[uuid.UUID]int, len(d.tenants))
	for _, r := range d.rows {
		counts[r.TenantID]++
	}
	err = s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		for _, tenantID := range d.tenants {
			if err := s.audit(ctx, q, actor, tenantID, ActionSightingRead, "face_sighting", uuid.Nil,
				map[string]any{"scope": "clusters", "sightings": counts[tenantID], "clusters": len(out)}); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}
