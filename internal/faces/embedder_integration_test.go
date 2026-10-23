//go:build integration

package faces_test

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/faces"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

type fakeEmbedder struct {
	profile faces.EmbeddingProfile
	calls   int
}

func (f *fakeEmbedder) Profile() faces.EmbeddingProfile { return f.profile }
func (f *fakeEmbedder) Smoke(context.Context) error     { return nil }
func (f *fakeEmbedder) Close() error                    { return nil }
func (f *fakeEmbedder) Embed(context.Context, image.Image) ([]float32, error) {
	f.calls++
	v := make([]float32, 512)
	v[0] = 1
	return v, nil
}

func TestEmbedderStoresVectorsOnceAndRecomputesOnModelChange(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.svc.Adapters = inventory.NewAdapters(sf.env.Svc)
	sf.h01.AddFaceTrainImage("1700000000.11-aa-1700000100-unknown-0.41.webp", pngOf(t, 61))
	sf.h01.AddFaceTrainImage("1700000000.12-bb-1700000200-unknown-0.42.webp", pngOf(t, 62))
	ctx := context.Background()
	if err := (&faces.ReviewIngestor{Store: sf.env.Store, Blobs: sf.blobs, Adapters: sf.svc.Adapters, Log: sf.svc.Log}).RunOnce(ctx, 25); err != nil {
		t.Fatal(err)
	}
	rt := &fakeEmbedder{profile: faces.EmbeddingProfileCPU}
	emb := &faces.Embedder{Store: sf.env.Store, Blobs: sf.blobs, Runtime: rt, ModelID: "arcface-r100-v1", Log: sf.svc.Log}

	if n, err := emb.RunOnce(ctx, 10); err != nil || n != 2 {
		t.Fatalf("first pass embedded %d, %v; want 2", n, err)
	}
	if n, err := emb.RunOnce(ctx, 10); err != nil || n != 0 || rt.calls != 2 {
		t.Fatalf("second pass embedded %d (calls %d), %v; want 0 and no new inference", n, rt.calls, err)
	}
	if got := sf.scanInt(`SELECT count(*) FROM face_sightings WHERE tenant_id = $1 AND embedding_profile = 'cpu' AND embedding_model = 'arcface-r100-v1+in2' AND cardinality(embedding) = 512`, sf.tenant); got != 2 {
		t.Fatalf("stored embeddings = %d, want 2", got)
	}

	// A vector stored under the bare model id predates the versioned input and must be recomputed.
	sf.exec(`UPDATE face_sightings SET embedding_model = 'arcface-r100-v1' WHERE tenant_id = $1`, sf.tenant)
	if n, err := emb.RunOnce(ctx, 10); err != nil || n != 2 {
		t.Fatalf("pass over legacy model ids embedded %d, %v; want 2", n, err)
	}

	emb.ModelID = "arcface-r100-v2"
	if n, err := emb.RunOnce(ctx, 10); err != nil || n != 2 {
		t.Fatalf("pass after model change embedded %d, %v; want 2", n, err)
	}
	if got := sf.scanInt(`SELECT count(*) FROM face_sightings WHERE tenant_id = $1 AND embedding_model = 'arcface-r100-v2+in2'`, sf.tenant); got != 2 {
		t.Fatalf("recomputed embeddings = %d, want 2", got)
	}
}

func TestReviewSightingsSuggestSimilarIdentitiesWithoutGrouping(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.svc.Adapters = inventory.NewAdapters(sf.env.Svc)
	for i, f := range []string{
		"1700000000.11-aa-1700000100-unknown-0.41.webp",
		"1700000000.12-bb-1700000200-unknown-0.42.webp",
		"1700000000.13-cc-1700000300-unknown-0.43.webp",
	} {
		sf.h01.AddFaceTrainImage(f, pngOf(t, byte(70+i)))
	}
	ctx := context.Background()
	if err := (&faces.ReviewIngestor{Store: sf.env.Store, Blobs: sf.blobs, Adapters: sf.svc.Adapters, Log: sf.svc.Log}).RunOnce(ctx, 25); err != nil {
		t.Fatal(err)
	}
	page, err := sf.svc.ListReviewSightings(ctx, sf.manager, faces.TrainFilter{TenantID: &sf.tenant})
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("pending sightings = %d, %v", len(page.Items), err)
	}
	for _, item := range page.Items {
		if len(item.Suggestions) != 0 {
			t.Fatalf("sighting %s has suggestions before any embedding", item.ID)
		}
	}
	// Two sightings share a vector; the third points elsewhere, so it must stay without suggestions.
	emb := &faces.Embedder{Store: sf.env.Store, Blobs: sf.blobs, Runtime: &fakeEmbedder{profile: faces.EmbeddingProfileCPU}, ModelID: "m1", Log: sf.svc.Log}
	if n, err := emb.RunOnce(ctx, 10); err != nil || n != 3 {
		t.Fatalf("embedded %d, %v", n, err)
	}
	sf.exec(`UPDATE face_sightings SET embedding = (SELECT array_agg(CASE WHEN i = 2 THEN 1.0 ELSE 0.0 END::real ORDER BY i) FROM generate_series(1, 512) i) WHERE id = $1`, page.Items[2].ID)

	page, err = sf.svc.ListReviewSightings(ctx, sf.manager, faces.TrainFilter{TenantID: &sf.tenant})
	if err != nil {
		t.Fatal(err)
	}
	withSuggestions := 0
	for _, item := range page.Items {
		withSuggestions += len(item.Suggestions)
		if item.GroupID != nil {
			t.Fatalf("sighting %s was grouped automatically", item.ID)
		}
	}
	if withSuggestions != 2 {
		t.Fatalf("suggestions = %d, want 2 (the matching pair, one each way)", withSuggestions)
	}
}

// fakeDetector judges a crop by its first gray pixel so tests control the verdict through pngOf.
type fakeDetector struct {
	noFace, broken map[uint8]bool
	calls          int
}

func (f *fakeDetector) Close() error { return nil }
func (f *fakeDetector) HasFace(_ context.Context, img image.Image) (bool, error) {
	f.calls++
	seed := img.(*image.Gray).Pix[0]
	if f.broken[seed] {
		return false, errors.New("detector failed")
	}
	return !f.noFace[seed], nil
}

func seedFaceSightings(t *testing.T, sf *syncFixture, seeds ...uint8) {
	t.Helper()
	sf.h01.AddFaceTrainImage("1700000000.11-aa-1700000100-unknown-0.41.webp", pngOf(t, seeds[0]))
	sf.h01.AddFaceTrainImage("1700000000.12-bb-1700000200-unknown-0.42.webp", pngOf(t, seeds[1]))
	sf.h01.AddFaceTrainImage("1700000000.13-cc-1700000300-unknown-0.43.webp", pngOf(t, seeds[2]))
	if err := (&faces.ReviewIngestor{Store: sf.env.Store, Blobs: sf.blobs, Adapters: sf.svc.Adapters, Log: sf.svc.Log}).RunOnce(context.Background(), 25); err != nil {
		t.Fatal(err)
	}
}

func TestEmbedderDropsCropsTheDetectorRejectsButKeepsThemInTheInbox(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.svc.Adapters = inventory.NewAdapters(sf.env.Svc)
	seedFaceSightings(t, sf, 61, 62, 63)
	ctx := context.Background()
	rt := &fakeEmbedder{profile: faces.EmbeddingProfileCPU}
	emb := &faces.Embedder{Store: sf.env.Store, Blobs: sf.blobs, Runtime: rt, ModelID: "m1", Log: sf.svc.Log}
	if n, err := emb.RunOnce(ctx, 10); err != nil || n != 3 {
		t.Fatalf("embedding before the gate = %d, %v; want 3", n, err)
	}

	// Turning the gate on judges the already embedded crops: the rejected one loses its vector, the
	// other two keep theirs without a second inference.
	det := &fakeDetector{noFace: map[uint8]bool{62: true}}
	emb.Detector = det
	if n, err := emb.RunOnce(ctx, 10); err != nil || n != 0 {
		t.Fatalf("gated pass stored %d, %v; want 0 new vectors", n, err)
	}
	if rt.calls != 3 || det.calls != 3 {
		t.Fatalf("inference calls = %d embed / %d detect, want 3 / 3", rt.calls, det.calls)
	}
	if got := sf.scanInt(`SELECT count(*) FROM face_sightings WHERE tenant_id = $1 AND face_detected = false AND embedding IS NULL AND embedding_model IS NULL AND embedded_at IS NULL`, sf.tenant); got != 1 {
		t.Fatalf("rejected sightings without a vector = %d, want 1", got)
	}
	if got := sf.scanInt(`SELECT count(*) FROM face_sightings WHERE tenant_id = $1 AND face_detected = true AND cardinality(embedding) = 512`, sf.tenant); got != 2 {
		t.Fatalf("accepted sightings with a vector = %d, want 2", got)
	}
	if n, err := emb.RunOnce(ctx, 10); err != nil || n != 0 || det.calls != 3 {
		t.Fatalf("second gated pass = %d, %v (detector calls %d); want nothing left to judge", n, err, det.calls)
	}

	page, err := sf.svc.ListReviewSightings(ctx, sf.manager, faces.TrainFilter{TenantID: &sf.tenant})
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("inbox sightings = %d, %v; a rejected crop must stay reviewable", len(page.Items), err)
	}

	// A model change recomputes accepted crops only; the rejected one is never listed again.
	emb.ModelID = "m2"
	if n, err := emb.RunOnce(ctx, 10); err != nil || n != 2 || det.calls != 3 {
		t.Fatalf("pass after model change = %d, %v (detector calls %d); want 2 re-embedded without re-detection", n, err, det.calls)
	}
}

func TestEmbedderRetriesACropWhenTheDetectorFails(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.svc.Adapters = inventory.NewAdapters(sf.env.Svc)
	seedFaceSightings(t, sf, 71, 72, 73)
	ctx := context.Background()
	det := &fakeDetector{broken: map[uint8]bool{72: true}}
	emb := &faces.Embedder{Store: sf.env.Store, Blobs: sf.blobs, Runtime: &fakeEmbedder{profile: faces.EmbeddingProfileCPU}, ModelID: "m1", Detector: det, Log: sf.svc.Log}
	if n, err := emb.RunOnce(ctx, 10); err == nil || n != 2 {
		t.Fatalf("pass = %d, %v; want 2 embedded and an error for the crop the detector could not judge", n, err)
	}
	if got := sf.scanInt(`SELECT count(*) FROM face_sightings WHERE tenant_id = $1 AND face_detected IS NULL AND embedding IS NULL AND embed_failures = 1`, sf.tenant); got != 1 {
		t.Fatalf("crops left unjudged with one failure = %d, want 1", got)
	}
}

func TestEmbedderDoesNotLetAnUnreadableCropStarveTheQueue(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.svc.Adapters = inventory.NewAdapters(sf.env.Svc)
	sf.h01.AddFaceTrainImage("1700000000.11-aa-1700000100-unknown-0.41.webp", pngOf(t, 61))
	sf.h01.AddFaceTrainImage("1700000000.12-bb-1700000200-unknown-0.42.webp", pngOf(t, 62))
	ctx := context.Background()
	if err := (&faces.ReviewIngestor{Store: sf.env.Store, Blobs: sf.blobs, Adapters: sf.svc.Adapters, Log: sf.svc.Log}).RunOnce(ctx, 25); err != nil {
		t.Fatal(err)
	}
	var badID uuid.UUID
	var badKey string
	sf.queryRow(`SELECT id FROM face_sightings WHERE tenant_id = $1 ORDER BY first_seen_at, id LIMIT 1`, &badID, sf.tenant)
	sf.queryRow(`SELECT crop_key FROM face_sightings WHERE id = $1`, &badKey, badID)
	if err := sf.blobs.Put(ctx, badKey, []byte("not an image"), "image/png"); err != nil {
		t.Fatal(err)
	}
	emb := &faces.Embedder{Store: sf.env.Store, Blobs: sf.blobs, Runtime: &fakeEmbedder{profile: faces.EmbeddingProfileCPU}, ModelID: "m1", Log: sf.svc.Log}

	// Two single-slot passes: the unreadable crop is tried once, then the fresh one gets the slot.
	if n, err := emb.RunOnce(ctx, 1); err == nil || n != 0 {
		t.Fatalf("first pass = %d, %v; want the unreadable crop to fail", n, err)
	}
	if n, err := emb.RunOnce(ctx, 1); err != nil || n != 1 {
		t.Fatalf("second pass = %d, %v; want the fresh crop embedded", n, err)
	}
	for i := 0; i < 6; i++ {
		_, _ = emb.RunOnce(ctx, 1)
	}
	if got := sf.scanInt(`SELECT embed_failures FROM face_sightings WHERE id = $1`, badID); got != 5 {
		t.Fatalf("embed_failures = %d, want retries capped at 5", got)
	}
}
