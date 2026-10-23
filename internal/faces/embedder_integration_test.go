//go:build integration

package faces_test

import (
	"context"
	"image"
	"testing"

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
	if got := sf.scanInt(`SELECT count(*) FROM face_sightings WHERE tenant_id = $1 AND embedding_profile = 'cpu' AND embedding_model = 'arcface-r100-v1' AND cardinality(embedding) = 512`, sf.tenant); got != 2 {
		t.Fatalf("stored embeddings = %d, want 2", got)
	}

	emb.ModelID = "arcface-r100-v2"
	if n, err := emb.RunOnce(ctx, 10); err != nil || n != 2 {
		t.Fatalf("pass after model change embedded %d, %v; want 2", n, err)
	}
	if got := sf.scanInt(`SELECT count(*) FROM face_sightings WHERE tenant_id = $1 AND embedding_model = 'arcface-r100-v2'`, sf.tenant); got != 2 {
		t.Fatalf("recomputed embeddings = %d, want 2", got)
	}
}
