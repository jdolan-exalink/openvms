package faces

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func rowAt(id, identity, server uuid.UUID, name string, at time.Time, score float64) db.ListFaceReviewSightingsRow {
	return db.ListFaceReviewSightingsRow{
		ID: id, IdentityID: identity, ServerID: server, ServerName: name,
		SourceSeenAt: &at, FirstSeenAt: at, SourceScore: &score,
	}
}

func TestSummarizeClustersCountsDaysServersAndFlagsRecurrence(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	srvA, srvB := uuid.New(), uuid.New()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	rows := []db.ListFaceReviewSightingsRow{
		rowAt(ids[0], ids[0], srvA, "Acceso", now.Add(-1*time.Hour), 0.7),
		rowAt(ids[1], ids[1], srvA, "Acceso", now.Add(-26*time.Hour), 0.9),
		rowAt(ids[2], ids[2], srvB, "Cochera", now.Add(-50*time.Hour), 0.5),
		rowAt(ids[3], ids[3], srvB, "Cochera", now.Add(-3*time.Hour), 0.6),
	}
	clusters := []Cluster{
		{SightingIDs: []uuid.UUID{ids[0], ids[1], ids[2]}, IdentityIDs: ids[:3]},
		{SightingIDs: []uuid.UUID{ids[3]}, IdentityIDs: ids[3:]},
	}
	got := SummarizeClusters(clusters, rows, ClusterRule{MinAppearances: 3, WindowDays: 7}, now)
	if len(got) != 2 {
		t.Fatalf("summaries = %d, want 2", len(got))
	}
	big := got[0]
	if big.SightingCount != 3 || big.DistinctDays != 3 || !big.Recurrent {
		t.Fatalf("big = %+v, want 3 sightings on 3 days, recurrent", big)
	}
	if big.ID != ids[2] {
		t.Fatalf("ID = %s, want the oldest sighting %s so it stays stable as new faces join", big.ID, ids[2])
	}
	if big.BestSightingID != ids[1] || len(big.ServerNames) != 2 {
		t.Fatalf("best = %s servers = %v, want the 0.9 score and two servers", big.BestSightingID, big.ServerNames)
	}
	if !big.LastSeenAt.Equal(now.Add(-1*time.Hour)) || !big.FirstSeenAt.Equal(now.Add(-50*time.Hour)) {
		t.Fatalf("first/last = %v/%v", big.FirstSeenAt, big.LastSeenAt)
	}
	if got[1].Recurrent {
		t.Fatalf("a single sighting must not be recurrent: %+v", got[1])
	}
}

func TestSummarizeClustersOnlyCountsAppearancesInsideTheWindow(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	srv := uuid.New()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	rows := []db.ListFaceReviewSightingsRow{
		rowAt(ids[0], ids[0], srv, "A", now.Add(-1*time.Hour), 0.5),
		rowAt(ids[1], ids[1], srv, "A", now.Add(-2*time.Hour), 0.5),
		rowAt(ids[2], ids[2], srv, "A", now.Add(-20*24*time.Hour), 0.5),
	}
	c := []Cluster{{SightingIDs: ids, IdentityIDs: ids}}
	if got := SummarizeClusters(c, rows, ClusterRule{MinAppearances: 3, WindowDays: 7}, now); got[0].Recurrent {
		t.Fatalf("two recent appearances and one old one must not reach 3 in a 7-day window: %+v", got[0])
	}
	if got := SummarizeClusters(c, rows, ClusterRule{MinAppearances: 3, WindowDays: 30}, now); !got[0].Recurrent {
		t.Fatalf("a 30-day window holds all three: %+v", got[0])
	}
}

func TestSummarizeClustersKeepsSightingsWithoutAnEmbeddingAsSingletons(t *testing.T) {
	now := time.Now()
	id := uuid.New()
	rows := []db.ListFaceReviewSightingsRow{rowAt(id, id, uuid.New(), "A", now, 0.4)}
	got := SummarizeClusters(nil, rows, ClusterRule{MinAppearances: 3, WindowDays: 7}, now)
	if len(got) != 1 || got[0].SightingCount != 1 || got[0].ID != id {
		t.Fatalf("a sighting with no embedding must still be reviewable: %+v", got)
	}
}
