//go:build cgo

package faces

import (
	"context"
	"image"
	"image/color"
	"os"
	"testing"
	"time"
)

// TestCPUEmbeddingRealModel runs the configured ArcFace model for real. It is skipped unless
// FACE_EMBEDDING_MODEL_PATH and ONNXRUNTIME_SHARED_LIBRARY point at deployment artifacts.
func TestCPUEmbeddingRealModel(t *testing.T) {
	modelPath, libraryPath := os.Getenv("FACE_EMBEDDING_MODEL_PATH"), os.Getenv("ONNXRUNTIME_SHARED_LIBRARY")
	if modelPath == "" || libraryPath == "" {
		t.Skip("FACE_EMBEDDING_MODEL_PATH and ONNXRUNTIME_SHARED_LIBRARY are not set")
	}
	ctx := context.Background()
	sel, err := SelectEmbeddingRuntime(ctx, []EmbeddingRuntimeFactory{
		CPUEmbeddingFactory{Config: CPUEmbeddingConfig{ModelPath: modelPath, LibraryPath: libraryPath}},
	})
	if err != nil {
		t.Fatalf("select runtime: %v", err)
	}
	defer func() { _ = sel.Runtime.Close() }()

	img := image.NewRGBA(image.Rect(0, 0, 160, 160))
	for y := 0; y < 160; y++ {
		for x := 0; x < 160; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8((x ^ y) & 0xff), A: 255})
		}
	}
	start := time.Now()
	const runs = 5
	var first []float32
	for i := 0; i < runs; i++ {
		v, err := sel.Runtime.Embed(ctx, img)
		if err != nil || len(v) != embeddingVectorSize {
			t.Fatalf("embed = %d values, %v", len(v), err)
		}
		if first == nil {
			first = v
		}
	}
	t.Logf("profile=%s latency=%s per crop", sel.Profile, time.Since(start)/runs)
	again, _ := sel.Runtime.Embed(ctx, img)
	var dot float64
	for i := range first {
		dot += float64(first[i]) * float64(again[i])
	}
	if dot < 0.999 {
		t.Fatalf("same crop is not deterministic: cosine %.4f", dot)
	}
}
