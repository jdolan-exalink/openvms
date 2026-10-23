package faces

import (
	"math"
	"sort"

	"github.com/google/uuid"
)

// DefaultSuggestionThreshold is the cosine similarity at which two ArcFace embeddings are offered
// to the operator as a possible match. It is deliberately conservative; the operator always decides.
const DefaultSuggestionThreshold = 0.5

const (
	maxSuggestionPerIdentity = 3
	maxSuggestionCandidates  = 2000
)

// Suggestion points to another pending identity that may be the same person.
type Suggestion struct {
	IdentityID uuid.UUID
	Similarity float64
}

type embeddedSighting struct {
	ID         uuid.UUID
	IdentityID uuid.UUID
	Profile    string
	Model      string
	Vector     []float32
}

// suggestIdentities returns, per identity, up to perIdentity other identities whose best member pair
// reaches threshold. Vectors are only compared when profile and model id match.
func suggestIdentities(items []embeddedSighting, threshold float64, perIdentity int) map[uuid.UUID][]Suggestion {
	if len(items) > maxSuggestionCandidates {
		items = items[:maxSuggestionCandidates]
	}
	norms := make([]float64, len(items))
	for i, it := range items {
		var sum float64
		for _, x := range it.Vector {
			sum += float64(x) * float64(x)
		}
		norms[i] = math.Sqrt(sum)
	}
	best := map[uuid.UUID]map[uuid.UUID]float64{}
	record := func(from, to uuid.UUID, sim float64) {
		if best[from] == nil {
			best[from] = map[uuid.UUID]float64{}
		}
		if sim > best[from][to] {
			best[from][to] = sim
		}
	}
	for i := range items {
		if norms[i] == 0 {
			continue
		}
		for j := i + 1; j < len(items); j++ {
			a, b := items[i], items[j]
			if norms[j] == 0 || a.IdentityID == b.IdentityID || a.Profile != b.Profile || a.Model != b.Model ||
				len(a.Vector) != len(b.Vector) {
				continue
			}
			var dot float64
			for k := range a.Vector {
				dot += float64(a.Vector[k]) * float64(b.Vector[k])
			}
			if sim := dot / (norms[i] * norms[j]); sim >= threshold {
				record(a.IdentityID, b.IdentityID, sim)
				record(b.IdentityID, a.IdentityID, sim)
			}
		}
	}
	out := make(map[uuid.UUID][]Suggestion, len(best))
	for from, targets := range best {
		list := make([]Suggestion, 0, len(targets))
		for to, sim := range targets {
			list = append(list, Suggestion{IdentityID: to, Similarity: sim})
		}
		sort.Slice(list, func(x, y int) bool {
			if list[x].Similarity != list[y].Similarity {
				return list[x].Similarity > list[y].Similarity
			}
			return list[x].IdentityID.String() < list[y].IdentityID.String()
		})
		if len(list) > perIdentity {
			list = list[:perIdentity]
		}
		out[from] = list
	}
	return out
}
