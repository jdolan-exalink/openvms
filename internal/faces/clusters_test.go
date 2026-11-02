package faces

import (
	"sort"
	"testing"

	"github.com/google/uuid"
)

func sightingOf(identity uuid.UUID, profile, model string, x, y float32) embeddedSighting {
	return embeddedSighting{ID: uuid.New(), IdentityID: identity, Profile: profile, Model: model, Vector: vec(x, y)}
}

func memberCounts(clusters []Cluster) []int {
	counts := make([]int, 0, len(clusters))
	for _, c := range clusters {
		counts = append(counts, len(c.SightingIDs))
	}
	sort.Sort(sort.Reverse(sort.IntSlice(counts)))
	return counts
}

func TestClusterSightingsGroupsSimilarFacesAndKeepsOthersApart(t *testing.T) {
	a, b, c, d, e := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	items := []embeddedSighting{
		sightingOf(a, "cpu", "m1", 1, 0),
		sightingOf(b, "cpu", "m1", 0.95, 0.1),
		sightingOf(c, "cpu", "m1", 0.9, 0.2),
		sightingOf(d, "cpu", "m1", 0, 1),
		sightingOf(e, "cpu", "m1", 0.1, 0.95),
	}
	got := ClusterSightings(items, 0.8, nil)
	if counts := memberCounts(got); len(counts) != 2 || counts[0] != 3 || counts[1] != 2 {
		t.Fatalf("cluster sizes = %v, want [3 2]", counts)
	}
}

func TestClusterSightingsNeverMixesProfileOrModel(t *testing.T) {
	items := []embeddedSighting{
		sightingOf(uuid.New(), "cpu", "m1", 1, 0),
		sightingOf(uuid.New(), "hailo8", "m1", 1, 0),
		sightingOf(uuid.New(), "cpu", "m2", 1, 0),
	}
	if got := ClusterSightings(items, 0.1, nil); len(got) != 3 {
		t.Fatalf("clusters = %d, want 3 (one per profile/model)", len(got))
	}
}

func TestClusterSightingsHonoursPairsTheOperatorRuledOut(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	items := []embeddedSighting{sightingOf(a, "cpu", "m1", 1, 0), sightingOf(b, "cpu", "m1", 1, 0)}
	ruled := pairSet{orderedUUIDPair(a, b): {}}
	if got := ClusterSightings(items, 0.5, ruled); len(got) != 2 {
		t.Fatalf("clusters = %d, want 2: the operator said these are different people", len(got))
	}
}

func TestClusterSightingsKeepsAnOperatorGroupTogether(t *testing.T) {
	g := uuid.New()
	items := []embeddedSighting{sightingOf(g, "cpu", "m1", 1, 0), sightingOf(g, "cpu", "m1", 0, 1)}
	got := ClusterSightings(items, 0.9, nil)
	if len(got) != 1 || len(got[0].SightingIDs) != 2 {
		t.Fatalf("clusters = %+v, want one cluster holding both members of the operator's group", got)
	}
}

func TestClusterSightingsDoesNotChainDistantFaces(t *testing.T) {
	// Each neighbour is close to the next one but the ends are not: a centroid cluster must not chain them.
	items := []embeddedSighting{
		sightingOf(uuid.New(), "cpu", "m1", 1, 0),
		sightingOf(uuid.New(), "cpu", "m1", 0.85, 0.5),
		sightingOf(uuid.New(), "cpu", "m1", 0.5, 0.85),
		sightingOf(uuid.New(), "cpu", "m1", 0, 1),
	}
	if got := ClusterSightings(items, 0.9, nil); len(got) < 2 {
		t.Fatalf("clusters = %d, want the chain split into at least two", len(got))
	}
}

func TestClusterSightingsIgnoresZeroVectorsAndEmptyInput(t *testing.T) {
	if got := ClusterSightings(nil, 0.5, nil); len(got) != 0 {
		t.Fatalf("clusters of nothing = %v", got)
	}
	zero := embeddedSighting{ID: uuid.New(), IdentityID: uuid.New(), Profile: "cpu", Model: "m1", Vector: vec(0, 0)}
	if got := ClusterSightings([]embeddedSighting{zero}, 0.5, nil); len(got) != 1 || len(got[0].SightingIDs) != 1 {
		t.Fatalf("a face with no usable vector must stay alone, got %+v", got)
	}
}
