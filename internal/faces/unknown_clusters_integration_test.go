//go:build integration

package faces_test

import (
	"context"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/faces"
)

// groupedSighting inserts a pending sighting with a 512-dimension vector. Vectors of shape "a" and "b" are
// orthogonal, so the two shapes never join the same group; shape "" stores no vector at all.
func (sf *syncFixture) groupedSighting(name string, age time.Duration, shape string) {
	sf.t.Helper()
	vector := "NULL, NULL, NULL, NULL"
	switch shape {
	case "a":
		vector = "ARRAY(SELECT 0.1::real FROM generate_series(1, 512)), 'cpu', 'arcface', now()"
	case "b":
		vector = "ARRAY(SELECT (CASE WHEN g % 2 = 0 THEN 0.1 ELSE -0.1 END)::real FROM generate_series(1, 512) g), 'cpu', 'arcface', now()"
	}
	sf.exec(`INSERT INTO face_sightings (tenant_id, server_id, source_file, source_seen_at, first_seen_at, crop_key, crop_content_type,
	           crop_size_bytes, crop_sha256, review_status, source_score, embedding, embedding_profile, embedding_model, embedded_at)
	         VALUES ($1, $2, $3, now() - $4::interval, now() - $4::interval, $5, 'image/jpeg', 1000, repeat('a', 64), 'pending', 0.5, `+vector+`)`,
		sf.tenant, sf.h01ID, name+".jpg", age.String(), "faces/crops/"+name)
}

func TestStoredClustersGroupTheWorkersWayAndPageOnTheServer(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	rule := faces.ClusterRule{MinAppearances: 3, WindowDays: 7}
	page := faces.ClusterPage{Limit: 10, Sort: faces.ClusterSortRecurrence}

	for i, name := range []string{"a1", "a2", "a3"} {
		sf.groupedSighting(name, time.Duration(i+1)*time.Hour, "a")
	}
	for i, name := range []string{"b1", "b2"} {
		sf.groupedSighting(name, time.Duration(i+1)*time.Hour, "b")
	}
	sf.groupedSighting("plain", time.Hour, "")

	// Before the worker has grouped anything every face is a group of one, so nothing is hidden.
	got, err := sf.svc.ListUnknownClusters(ctx, sf.env.Admin, faces.TrainFilter{}, rule, page)
	if err != nil || got.Total != 6 || len(got.Items) != 6 {
		t.Fatalf("before grouping: total %d, items %d, err %v; want 6 groups of one", got.Total, len(got.Items), err)
	}

	if n, err := sf.svc.RefreshUnknownClusters(ctx); err != nil || n != 1 {
		t.Fatalf("first pass grouped %d tenants, err %v; want 1", n, err)
	}
	got, err = sf.svc.ListUnknownClusters(ctx, sf.env.Admin, faces.TrainFilter{}, rule, page)
	if err != nil || got.Total != 3 || len(got.Items) != 3 {
		t.Fatalf("after grouping: total %d, items %d, err %v; want 3 groups", got.Total, len(got.Items), err)
	}
	first := got.Items[0]
	if first.SightingCount != 3 || !first.Recurrent || first.DistinctDays != 1 || len(first.ServerIDs) != 1 || first.ServerNames[0] == "" || first.BestScore == nil {
		t.Fatalf("largest group = %+v, want 3 recurrent sightings from one server with a score", first)
	}
	if got.Items[1].SightingCount != 2 || got.Items[1].Recurrent || got.Items[2].SightingCount != 1 {
		t.Fatalf("other groups = %+v / %+v, want sizes 2 and 1", got.Items[1], got.Items[2])
	}

	// Paging: the total stays the number of groups, each page holds its slice, and a limit is honoured.
	p1, err := sf.svc.ListUnknownClusters(ctx, sf.env.Admin, faces.TrainFilter{}, rule, faces.ClusterPage{Limit: 2, Sort: faces.ClusterSortRecurrence})
	if err != nil || p1.Total != 3 || len(p1.Items) != 2 || p1.Items[0].ID != first.ID {
		t.Fatalf("first page = total %d, items %d, err %v", p1.Total, len(p1.Items), err)
	}
	p2, err := sf.svc.ListUnknownClusters(ctx, sf.env.Admin, faces.TrainFilter{}, rule, faces.ClusterPage{Limit: 2, Offset: 2, Sort: faces.ClusterSortRecurrence})
	if err != nil || p2.Total != 3 || len(p2.Items) != 1 || p2.Items[0].ID != got.Items[2].ID {
		t.Fatalf("second page = total %d, items %d, err %v", p2.Total, len(p2.Items), err)
	}

	// A pass with nothing new does no work, and a new face joins its group on the next pass.
	if n, err := sf.svc.RefreshUnknownClusters(ctx); err != nil || n != 0 {
		t.Fatalf("idle pass grouped %d tenants, err %v; want 0", n, err)
	}
	sf.groupedSighting("a4", 30*time.Minute, "a")
	if n, err := sf.svc.RefreshUnknownClusters(ctx); err != nil || n != 1 {
		t.Fatalf("pass after a new face grouped %d tenants, err %v; want 1", n, err)
	}
	got, err = sf.svc.ListUnknownClusters(ctx, sf.env.Admin, faces.TrainFilter{}, rule, page)
	if err != nil || got.Total != 3 || got.Items[0].SightingCount != 4 || got.Items[0].ID != first.ID {
		t.Fatalf("after the new face: total %d, largest %+v, err %v; want the same group id with 4 sightings", got.Total, got.Items[0], err)
	}
}

func TestStoredClustersApplyTheSortAndTheRuleWhenReading(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	// Three old faces of one person and one fresh face of another.
	for i, name := range []string{"old1", "old2", "old3"} {
		sf.groupedSighting(name, time.Duration(20+i)*24*time.Hour, "a")
	}
	sf.groupedSighting("fresh", time.Minute, "b")
	if _, err := sf.svc.RefreshUnknownClusters(ctx); err != nil {
		t.Fatal(err)
	}

	byRecent, err := sf.svc.ListUnknownClusters(ctx, sf.env.Admin, faces.TrainFilter{}, faces.ClusterRule{MinAppearances: 3, WindowDays: 30}, faces.ClusterPage{Limit: 10, Sort: faces.ClusterSortRecent})
	if err != nil || byRecent.Total != 2 || byRecent.Items[0].SightingCount != 1 || !byRecent.Items[1].Recurrent {
		t.Fatalf("most recent first = %+v, err %v; want the fresh single face first and the old trio recurrent in a 30-day window", byRecent.Items, err)
	}
	// The same data is not recurrent under a 7-day window: the rule is applied when reading, never stored.
	narrow, err := sf.svc.ListUnknownClusters(ctx, sf.env.Admin, faces.TrainFilter{}, faces.ClusterRule{MinAppearances: 3, WindowDays: 7}, faces.ClusterPage{Limit: 10, Sort: faces.ClusterSortRecurrence})
	if err != nil || narrow.Items[0].Recurrent || narrow.Items[0].SightingCount != 3 || narrow.Items[1].SightingCount != 1 {
		t.Fatalf("7-day window = %+v, err %v; want the trio first by size and not recurrent", narrow.Items, err)
	}
}
