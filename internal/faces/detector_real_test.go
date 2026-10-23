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

// TestYuNetRealModelRejectsImagesWithoutFaces runs the configured detector for real. It is skipped
// unless FACE_DETECTOR_MODEL_PATH and ONNXRUNTIME_SHARED_LIBRARY point at deployment artifacts.
// The repository carries no face photo, so the positive case is covered by the calibration notes in
// docs/operations/face-embeddings.md rather than by a fixture.
func TestYuNetRealModelRejectsImagesWithoutFaces(t *testing.T) {
	modelPath, libraryPath := os.Getenv("FACE_DETECTOR_MODEL_PATH"), os.Getenv("ONNXRUNTIME_SHARED_LIBRARY")
	if modelPath == "" || libraryPath == "" {
		t.Skip("FACE_DETECTOR_MODEL_PATH and ONNXRUNTIME_SHARED_LIBRARY are not set")
	}
	det, err := OpenYuNet(YuNetConfig{ModelPath: modelPath, LibraryPath: libraryPath})
	if err != nil {
		t.Fatalf("open detector: %v", err)
	}
	defer func() { _ = det.Close() }()

	fill := func(c func(x, y int) color.RGBA) image.Image {
		img := image.NewRGBA(image.Rect(0, 0, 120, 120))
		for y := 0; y < 120; y++ {
			for x := 0; x < 120; x++ {
				img.Set(x, y, c(x, y))
			}
		}
		return img
	}
	cases := map[string]image.Image{
		"black frame": fill(func(int, int) color.RGBA { return color.RGBA{A: 255} }),
		"flat gray":   fill(func(int, int) color.RGBA { return color.RGBA{R: 150, G: 160, B: 165, A: 255} }),
		"gradient": fill(func(x, y int) color.RGBA {
			return color.RGBA{R: uint8((x * 2) & 0xff), G: uint8((y * 2) & 0xff), B: 90, A: 255}
		}),
	}
	start := time.Now()
	for name, img := range cases {
		has, err := det.HasFace(context.Background(), img)
		if err != nil || has {
			t.Fatalf("%s: HasFace = %v, %v; want false", name, has, err)
		}
	}
	t.Logf("latency=%s per crop", time.Since(start)/time.Duration(len(cases)))
}
