package faces

import (
	"math"

	"github.com/google/uuid"
)

// DefaultClusterThreshold is the cosine similarity between a face and a cluster's centroid at which the
// face joins that cluster. It is stricter than DefaultSuggestionThreshold: a cluster is shown to the
// operator as one person, while a suggestion only asks them to look at two.
const DefaultClusterThreshold = 0.60

// Cluster is a proposal: pending sightings whose embeddings look like one person. Nothing is stored; the
// operator decides what a cluster becomes.
type Cluster struct {
	Profile     string
	Model       string
	SightingIDs []uuid.UUID
	IdentityIDs []uuid.UUID
}

type clusterState struct {
	Cluster
	sum        []float64
	identities map[uuid.UUID]struct{}
}

// ClusterSightings groups sightings by embedding similarity. Callers pass them newest first, and the
// result is deterministic for that order. A face joins the existing cluster whose centroid is closest
// when the similarity reaches threshold, so a chain of look-alikes does not merge into one person. Rules:
//   - vectors of different profile or model are never compared;
//   - sightings that share an identity (an operator's group) always land in the same cluster;
//   - an identity pair the operator ruled out never shares a cluster;
//   - a face without a usable vector stays in a cluster of its own.
func ClusterSightings(items []embeddedSighting, threshold float64, excluded pairSet) []Cluster {
	var clusters []*clusterState
	byIdentity := map[uuid.UUID]*clusterState{}
	for _, it := range items {
		unit := unitVector(it.Vector)
		target := byIdentity[it.IdentityID]
		if target == nil && unit != nil {
			target = bestCluster(clusters, it, unit, threshold, excluded)
		}
		if target == nil {
			target = &clusterState{
				Cluster:    Cluster{Profile: it.Profile, Model: it.Model},
				identities: map[uuid.UUID]struct{}{},
			}
			if unit != nil {
				target.sum = make([]float64, len(unit))
			}
			clusters = append(clusters, target)
		}
		target.SightingIDs = append(target.SightingIDs, it.ID)
		if _, ok := target.identities[it.IdentityID]; !ok {
			target.identities[it.IdentityID] = struct{}{}
			target.IdentityIDs = append(target.IdentityIDs, it.IdentityID)
		}
		byIdentity[it.IdentityID] = target
		if unit != nil && len(target.sum) == len(unit) {
			for i, x := range unit {
				target.sum[i] += x
			}
		}
	}
	out := make([]Cluster, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, c.Cluster)
	}
	return out
}

func bestCluster(clusters []*clusterState, it embeddedSighting, unit []float64, threshold float64, excluded pairSet) *clusterState {
	var best *clusterState
	bestSim := threshold
	for _, c := range clusters {
		if c.Profile != it.Profile || c.Model != it.Model || len(c.sum) != len(unit) {
			continue
		}
		if pairRuledOut(c, it.IdentityID, excluded) {
			continue
		}
		var dot, norm float64
		for i, x := range c.sum {
			dot += x * unit[i]
			norm += x * x
		}
		if norm == 0 {
			continue
		}
		if sim := dot / math.Sqrt(norm); sim >= bestSim {
			best, bestSim = c, sim
		}
	}
	return best
}

func pairRuledOut(c *clusterState, identity uuid.UUID, excluded pairSet) bool {
	for member := range c.identities {
		if _, no := excluded[orderedUUIDPair(member, identity)]; no {
			return true
		}
	}
	return false
}

// unitVector returns v scaled to length 1, or nil when v has no direction.
func unitVector(v []float32) []float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return nil
	}
	n := math.Sqrt(sum)
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = float64(x) / n
	}
	return out
}
