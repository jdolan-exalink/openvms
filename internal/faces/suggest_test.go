package faces

import (
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
	got := suggestIdentities(items, 0.8, 3)
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
	if got := suggestIdentities(items, 0.1, 3); len(got) != 0 {
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
	got := suggestIdentities(items, 0.5, 2)
	if len(got[ids[0]]) != 2 {
		t.Fatalf("suggestions = %d, want capped at 2", len(got[ids[0]]))
	}
	if len(got[zero]) != 0 {
		t.Fatalf("zero vector received suggestions: %+v", got[zero])
	}
}
