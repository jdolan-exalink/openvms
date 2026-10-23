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

// TestCPUEmbeddingRealModelSeparatesDifferentImages pins the input range the ArcFace graph expects.
// The OMZ model normalizes pixels itself, so feeding it [-1, 1] collapses every crop onto nearly
// the same vector (cosine > 0.9 between unrelated images) and every suggestion reads as a match.
func TestCPUEmbeddingRealModelSeparatesDifferentImages(t *testing.T) {
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

	embed := func(fill func(x, y int) color.RGBA) []float32 {
		img := image.NewRGBA(image.Rect(0, 0, 112, 112))
		for y := 0; y < 112; y++ {
			for x := 0; x < 112; x++ {
				img.Set(x, y, fill(x, y))
			}
		}
		v, err := sel.Runtime.Embed(ctx, img)
		if err != nil {
			t.Fatalf("embed: %v", err)
		}
		return v
	}
	gradient := embed(func(x, y int) color.RGBA {
		return color.RGBA{R: uint8((x * 2) & 0xff), G: uint8((y * 2) & 0xff), B: 90, A: 255}
	})
	stripes := embed(func(x, y int) color.RGBA {
		if (x/8+y/8)%2 == 0 {
			return color.RGBA{R: 240, G: 30, B: 30, A: 255}
		}
		return color.RGBA{R: 20, G: 20, B: 200, A: 255}
	})
	var dot float64
	for i := range gradient {
		dot += float64(gradient[i]) * float64(stripes[i])
	}
	if dot > 0.8 {
		t.Fatalf("unrelated images embed almost identically: cosine %.3f", dot)
	}
}
