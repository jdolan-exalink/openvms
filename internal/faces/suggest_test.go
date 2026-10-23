package faces

import (
	"math"
	"testing"

	"github.com/google/uuid"
)

func vec(x, y float32) []float32 {
	v := make([]float32, embeddingVectorSize)
	v[0], v[1] = x, y
	return v
}

func TestSuggestIdentitiesRanksByBestMemberPairAndRespectsThreshold(t *testing.T) {
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	groupID := uuid.New()
	items := []embeddedSighting{
		{ID: a, IdentityID: a, Profile: "cpu", Model: "m1", Vector: vec(1, 0)},
		{ID: b, IdentityID: b, Profile: "cpu", Model: "m1", Vector: vec(0.9, 0.1)},
		{ID: c, IdentityID: groupID, Profile: "cpu", Model: "m1", Vector: vec(0, 1)},
		{ID: d, IdentityID: groupID, Profile: "cpu", Model: "m1", Vector: vec(0.8, 0.2)},
	}
	got := suggestIdentities(items, 0.8, 3, nil)
	if len(got[a]) != 2 || got[a][0].IdentityID != b || got[a][1].IdentityID != groupID {
		t.Fatalf("suggestions for a = %+v, want b then the group (best member d)", got[a])
	}
	if got[a][0].Similarity < got[a][1].Similarity {
		t.Fatalf("suggestions are not sorted by similarity: %+v", got[a])
	}
	if len(got[groupID]) != 2 {
		t.Fatalf("group suggestions = %+v, want a and b", got[groupID])
	}
	for id, list := range got {
		for _, s := range list {
			if s.IdentityID == id {
				t.Fatalf("identity %s suggested to itself", id)
			}
		}
	}
}

func TestSuggestIdentitiesNeverComparesAcrossProfileOrModel(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	items := []embeddedSighting{
		{ID: a, IdentityID: a, Profile: "cpu", Model: "m1", Vector: vec(1, 0)},
		{ID: b, IdentityID: b, Profile: "hailo8", Model: "m1", Vector: vec(1, 0)},
		{ID: c, IdentityID: c, Profile: "cpu", Model: "m2", Vector: vec(1, 0)},
	}
	if got := suggestIdentities(items, 0.1, 3, nil); len(got) != 0 {
		t.Fatalf("suggestions across profile/model = %+v, want none", got)
	}
}

func TestSuggestIdentitiesLimitsPerIdentityAndIgnoresZeroVectors(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	var items []embeddedSighting
	for _, id := range ids {
		items = append(items, embeddedSighting{ID: id, IdentityID: id, Profile: "cpu", Model: "m1", Vector: vec(1, 0)})
	}
	zero := uuid.New()
	items = append(items, embeddedSighting{ID: zero, IdentityID: zero, Profile: "cpu", Model: "m1", Vector: vec(0, 0)})
	got := suggestIdentities(items, 0.5, 2, nil)
	if len(got[ids[0]]) != 2 {
		t.Fatalf("suggestions = %d, want capped at 2", len(got[ids[0]]))
	}
	if len(got[zero]) != 0 {
		t.Fatalf("zero vector received suggestions: %+v", got[zero])
	}
}

// The default threshold comes from reviewing real crop pairs with aligned embeddings: same-person
// pairs held down to about 0.46, and the first different people appeared at 0.44.
func TestDefaultSuggestionThresholdDropsPairsBelow050(t *testing.T) {
	at := func(cos float64) []float32 { return vec(float32(cos), float32(math.Sqrt(1-cos*cos))) }
	base, below, above := uuid.New(), uuid.New(), uuid.New()
	items := []embeddedSighting{
		{ID: base, IdentityID: base, Profile: "cpu", Model: "m1", Vector: vec(1, 0)},
		{ID: below, IdentityID: below, Profile: "cpu", Model: "m1", Vector: at(0.47)},
		{ID: above, IdentityID: above, Profile: "cpu", Model: "m1", Vector: at(0.53)},
	}
	got := suggestIdentities(items, DefaultSuggestionThreshold, maxSuggestionPerIdentity, nil)
	if len(got[base]) != 1 || got[base][0].IdentityID != above {
		t.Fatalf("suggestions for base = %+v, want only the 0.53 match", got[base])
	}
}

func TestSuggestIdentitiesSkipsRuledOutPairsBeforeTheCap(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	items := []embeddedSighting{
		{ID: a, IdentityID: a, Profile: "cpu", Model: "m1", Vector: vec(1, 0)},
		{ID: b, IdentityID: b, Profile: "cpu", Model: "m1", Vector: vec(0.95, 0.31)},
		{ID: c, IdentityID: c, Profile: "cpu", Model: "m1", Vector: vec(0.8, 0.6)},
	}
	ruledOut := pairSet{orderedUUIDPair(b, a): {}} // the order the pair is named in does not matter
	got := suggestIdentities(items, 0.5, 1, ruledOut)
	if len(got[a]) != 1 || got[a][0].IdentityID != c {
		t.Fatalf("suggestions for a = %+v, want c once b is ruled out (the cap must not be spent on b)", got[a])
	}
	if len(got[b]) != 1 || got[b][0].IdentityID != c {
		t.Fatalf("suggestions for b = %+v, want c", got[b])
	}
}

// A card shows up to five candidates: three hid lower-scoring same-person matches when one person
// had several close shots, and more would bury the card in suggestion blocks.
func TestDefaultSuggestionCapPerIdentityIsFive(t *testing.T) {
	base := uuid.New()
	items := []embeddedSighting{{ID: base, IdentityID: base, Profile: "cpu", Model: "m1", Vector: vec(1, 0)}}
	for i := 0; i < 7; i++ {
		id := uuid.New()
		cos := 0.95 - 0.02*float64(i) // 0.95, 0.93, ... 0.83
		items = append(items, embeddedSighting{ID: id, IdentityID: id, Profile: "cpu", Model: "m1", Vector: vec(float32(cos), float32(math.Sqrt(1-cos*cos)))})
	}
	got := suggestIdentities(items, DefaultSuggestionThreshold, maxSuggestionPerIdentity, nil)
	if len(got[base]) != 5 {
		t.Fatalf("suggestions for base = %d, want 5 of the 7 matches", len(got[base]))
	}
	for i := 1; i < len(got[base]); i++ {
		if got[base][i].Similarity > got[base][i-1].Similarity {
			t.Fatalf("suggestions are not best-first: %+v", got[base])
		}
	}
}
