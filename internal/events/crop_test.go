package events

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func createTestJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Draw background
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 50, G: 50, B: 50, A: 255})
		}
	}
	// Draw a simulated bright plate rectangle at (100, 200) to (180, 240)
	for y := 200; y < 240; y++ {
		for x := 100; x < 180; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}

func TestCropLicensePlateValidBox(t *testing.T) {
	orig := createTestJPEG(1000, 1000)
	// Box: top=0.20 (200px), left=0.10 (100px), height=0.04 (40px), width=0.08 (80px)
	box := []float64{0.20, 0.10, 0.04, 0.08}
	cropped, err := CropLicensePlate(orig, box)
	if err != nil {
		t.Fatalf("CropLicensePlate: %v", err)
	}
	if len(cropped) == 0 {
		t.Fatal("expected non-empty cropped JPEG")
	}

	img, err := jpeg.Decode(bytes.NewReader(cropped))
	if err != nil {
		t.Fatalf("decode cropped: %v", err)
	}
	b := img.Bounds()
	// With 20% margin:
	// height was 40px -> + 2*8px = ~56px
	// width was 80px -> + 2*16px = ~112px
	if b.Dx() < 70 || b.Dx() > 140 {
		t.Errorf("cropped width = %d, want ~112", b.Dx())
	}
	if b.Dy() < 35 || b.Dy() > 75 {
		t.Errorf("cropped height = %d, want ~56", b.Dy())
	}
}

func TestCropLicensePlateInvalidBoxPassesThrough(t *testing.T) {
	orig := createTestJPEG(100, 100)
	cropped, err := CropLicensePlate(orig, []float64{0.1, 0.2})
	if err != nil {
		t.Fatalf("unexpected error on short box: %v", err)
	}
	if len(cropped) != len(orig) {
		t.Errorf("expected original bytes passed through on invalid box")
	}
}
