package faces

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// transformPoints applies a similarity (rotation theta, scale s, translation) to the template.
func transformPoints(theta, s, tx, ty float64) [5][2]float64 {
	var out [5][2]float64
	c, sn := math.Cos(theta), math.Sin(theta)
	for i, p := range arcFaceTemplate {
		out[i] = [2]float64{s*(c*p[0]-sn*p[1]) + tx, s*(sn*p[0]+c*p[1]) + ty}
	}
	return out
}

func TestEstimateSimilarityRecoversRotationScaleAndTranslation(t *testing.T) {
	src := transformPoints(0.3, 1.7, 20, -9)
	m, err := estimateSimilarity(src, arcFaceTemplate)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range src {
		x := m[0]*p[0] + m[1]*p[1] + m[2]
		y := m[3]*p[0] + m[4]*p[1] + m[5]
		if math.Abs(x-arcFaceTemplate[i][0]) > 1e-6 || math.Abs(y-arcFaceTemplate[i][1]) > 1e-6 {
			t.Fatalf("landmark %d maps to (%.4f, %.4f), want %v", i, x, y, arcFaceTemplate[i])
		}
	}
	if got := m.scale(); math.Abs(got-1/1.7) > 1e-6 {
		t.Fatalf("scale = %.6f, want %.6f", got, 1/1.7)
	}
}

func TestEstimateSimilarityRejectsDegenerateLandmarks(t *testing.T) {
	var same [5][2]float64
	for i := range same {
		same[i] = [2]float64{10, 10}
	}
	if _, err := estimateSimilarity(same, arcFaceTemplate); err == nil {
		t.Fatal("landmarks collapsed onto one point were accepted")
	}
}

func gradient(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / (w - 1)), G: uint8(y * 255 / (h - 1)), B: 100, A: 255}) //nolint:gosec // bounded 0-255.
		}
	}
	return img
}

func TestAlignFace112IsTheIdentityWhenLandmarksAlreadyMatchTheTemplate(t *testing.T) {
	src := gradient(faceModelInputSize, faceModelInputSize)
	got, err := alignFace112(src, arcFaceTemplate)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range [][2]int{{10, 10}, {56, 56}, {100, 90}} {
		wr, wg, wb, _ := src.At(p[0], p[1]).RGBA()
		gr, gg, gb, _ := got.At(p[0], p[1]).RGBA()
		if abs16(wr, gr) > 2<<8 || abs16(wg, gg) > 2<<8 || abs16(wb, gb) > 2<<8 {
			t.Fatalf("pixel %v changed: got %v,%v,%v want %v,%v,%v", p, gr>>8, gg>>8, gb>>8, wr>>8, wg>>8, wb>>8)
		}
	}
}

func abs16(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

func TestAlignFace112UndoesAShiftedAndScaledFace(t *testing.T) {
	// The face sits twice as large and 30 px to the right in a 300x300 crop: the template landmark
	// positions, scaled by 2 and shifted, must come back at the template positions.
	const w = 300
	src := image.NewRGBA(image.Rect(0, 0, w, w))
	lm := transformPoints(0, 2, 30, 20)
	for _, p := range lm {
		for dy := -3; dy <= 3; dy++ {
			for dx := -3; dx <= 3; dx++ {
				src.Set(int(p[0])+dx, int(p[1])+dy, color.RGBA{R: 255, A: 255})
			}
		}
	}
	got, err := alignFace112(src, lm)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range arcFaceTemplate {
		r, _, _, _ := got.At(int(math.Round(p[0])), int(math.Round(p[1]))).RGBA()
		if r>>8 < 200 {
			t.Fatalf("landmark %d is not at the template position %v after aligning (red=%d)", i, p, r>>8)
		}
	}
}

func TestAlignFace112RejectsBadInput(t *testing.T) {
	if _, err := alignFace112(nil, arcFaceTemplate); err == nil {
		t.Fatal("nil crop was accepted")
	}
	tiny := transformPoints(0, 0.001, 0, 0) // landmarks squeezed to a speck would need a huge magnification
	if _, err := alignFace112(gradient(40, 40), tiny); err == nil {
		t.Fatal("implausible landmark scale was accepted")
	}
}

func TestAlignFace112HonoursCropsThatDoNotStartAtTheOrigin(t *testing.T) {
	// The same shifted-and-scaled face as above, inside an image whose bounds start at (50, 40): the
	// landmarks are given in the image's own coordinates, as the detector reports them.
	src := image.NewRGBA(image.Rect(50, 40, 350, 340))
	lm := transformPoints(0, 2, 80, 60)
	for _, p := range lm {
		for dy := -3; dy <= 3; dy++ {
			for dx := -3; dx <= 3; dx++ {
				src.Set(int(p[0])+dx, int(p[1])+dy, color.RGBA{R: 255, A: 255})
			}
		}
	}
	got, err := alignFace112(src, lm)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range arcFaceTemplate {
		r, _, _, _ := got.At(int(math.Round(p[0])), int(math.Round(p[1]))).RGBA()
		if r>>8 < 200 {
			t.Fatalf("landmark %d is not at the template position %v after aligning (red=%d)", i, p, r>>8)
		}
	}
}
