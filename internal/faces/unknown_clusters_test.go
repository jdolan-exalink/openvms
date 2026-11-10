package faces

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStoredClusterIDsNamesEachClusterByItsOldestMember(t *testing.T) {
	now := time.Now()
	oldest, middle, newest, alone := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seen := map[uuid.UUID]time.Time{oldest: now.Add(-3 * time.Hour), middle: now.Add(-2 * time.Hour), newest: now, alone: now.Add(-time.Hour)}
	// The cluster lists its members newest first, as the clustering pass receives them.
	sightings, names := storedClusterIDs([]Cluster{{SightingIDs: []uuid.UUID{newest, middle, oldest}}, {SightingIDs: []uuid.UUID{alone}}}, seen)
	if len(sightings) != 4 || len(names) != 4 {
		t.Fatalf("sightings = %d, names = %d, want 4 each", len(sightings), len(names))
	}
	want := map[uuid.UUID]uuid.UUID{newest: oldest, middle: oldest, oldest: oldest, alone: alone}
	for i, id := range sightings {
		if names[i] != want[id] {
			t.Fatalf("sighting %s named %s, want %s", id, names[i], want[id])
		}
	}
}

func TestStoredClusterIDsBreaksTiesBySightingID(t *testing.T) {
	at := time.Now()
	a, b := uuid.MustParse("00000000-0000-0000-0000-000000000001"), uuid.MustParse("00000000-0000-0000-0000-000000000002")
	for _, order := range [][]uuid.UUID{{a, b}, {b, a}} {
		_, names := storedClusterIDs([]Cluster{{SightingIDs: order}}, map[uuid.UUID]time.Time{a: at, b: at})
		if names[0] != a || names[1] != a {
			t.Fatalf("order %v named %v, want %s for both", order, names, a)
		}
	}
}
