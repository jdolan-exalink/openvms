//go:build cgo

package faces

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestPreprocessArcFace112ConvertsRGBToNormalizedNCHW(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 255, G: 127, B: 0, A: 255})
	got, err := preprocessArcFace112(img)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3*faceModelInputSize*faceModelInputSize {
		t.Fatalf("input length = %d", len(got))
	}
	plane := faceModelInputSize * faceModelInputSize
	if got[0] != 1 || got[plane] < -0.01 || got[plane] > 0.01 || got[2*plane] != -1 {
		t.Fatalf("first pixel channels = %v, want normalized R,G,B = [1, 0, -1]", []float32{got[0], got[plane], got[2*plane]})
	}
}

func TestNormalizeEmbeddingReturnsUnitVector(t *testing.T) {
	values := make([]float32, faceModelVectorSize)
	values[0], values[1] = 3, 4
	got, err := normalizeEmbedding(values)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != 0.6 || got[1] != 0.8 {
		t.Fatalf("normalized first values = %v, want [0.6, 0.8]", got[:2])
	}
}

func TestNormalizeEmbeddingRejectsInvalidVectors(t *testing.T) {
	tests := []struct {
		name   string
		values []float32
	}{
		{name: "wrong dimension", values: []float32{1}},
		{name: "zero norm", values: make([]float32, faceModelVectorSize)},
		{name: "non-finite", values: func() []float32 { v := make([]float32, faceModelVectorSize); v[0] = float32(math.NaN()); return v }()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := normalizeEmbedding(tt.values); err == nil {
				t.Fatal("expected invalid embedding error")
			}
		})
	}
}
