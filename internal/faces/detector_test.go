package faces

import (
	"image"
	"image/color"
	"testing"
)

func TestPreprocessYuNetCentersTheCropAsRawBGR(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 10, A: 255})
		}
	}
	got, err := preprocessYuNet(img)
	if err != nil {
		t.Fatal(err)
	}
	plane := yunetInputSize * yunetInputSize
	if len(got) != 3*plane {
		t.Fatalf("input length = %d, want %d", len(got), 3*plane)
	}
	if got[0] != 0 || got[plane] != 0 || got[2*plane] != 0 {
		t.Fatalf("canvas corner = %v,%v,%v, want black padding", got[0], got[plane], got[2*plane])
	}
	centre := (yunetInputSize/2)*yunetInputSize + yunetInputSize/2
	if got[centre] != 10 || got[plane+centre] != 100 || got[2*plane+centre] != 200 {
		t.Fatalf("centre B,G,R = %v,%v,%v, want raw 10,100,200", got[centre], got[plane+centre], got[2*plane+centre])
	}
}

func TestPreprocessYuNetKeepsAspectRatio(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	got, err := preprocessYuNet(img)
	if err != nil {
		t.Fatal(err)
	}
	lit := 0
	rowLit := map[int]bool{}
	for i := 0; i < yunetInputSize*yunetInputSize; i++ {
		if got[i] > 0 {
			lit++
			rowLit[i/yunetInputSize] = true
		}
	}
	if want := yunetFaceSize * (yunetFaceSize / 4); lit != want || len(rowLit) != yunetFaceSize/4 {
		t.Fatalf("lit pixels = %d over %d rows, want %d over %d (4:1 box)", lit, len(rowLit), want, yunetFaceSize/4)
	}
}

func TestPreprocessYuNetRejectsEmptyCrops(t *testing.T) {
	if _, err := preprocessYuNet(nil); err == nil {
		t.Fatal("nil crop was accepted")
	}
	if _, err := preprocessYuNet(image.NewRGBA(image.Rect(0, 0, 0, 0))); err == nil {
		t.Fatal("empty crop was accepted")
	}
}

// yunetOutputs builds fake detector outputs; landmarks gives the 10 kps offsets of one anchor.
func yunetOutputs(fill func(stride int, i int) (cls, obj float32)) (cls, obj, kps [3][]float32) {
	for s, stride := range yunetStrides {
		n := yunetAnchors(stride)
		cls[s], obj[s], kps[s] = make([]float32, n), make([]float32, n), make([]float32, n*10)
		for i := 0; i < n; i++ {
			cls[s][i], obj[s][i] = fill(stride, i)
		}
	}
	return cls, obj, kps
}

func TestBestYuNetDetectionTakesTheStrongestAnchorOfAnyStride(t *testing.T) {
	cls, obj, kps := yunetOutputs(func(stride, i int) (float32, float32) {
		if stride == 16 && i == 7 {
			return 0.81, 0.81
		}
		return 0.1, 0.1
	})
	got, _, err := bestYuNetDetection(cls, obj, kps)
	if err != nil || got < 0.80 || got > 0.82 {
		t.Fatalf("score = %v, %v; want about 0.81", got, err)
	}
}

func TestBestYuNetDetectionDecodesLandmarksFromTheWinningAnchor(t *testing.T) {
	// Anchor 45 of stride 16 sits on the 40-wide grid at row 1, column 5. Landmark n is offset by
	// (n, 2n) cells from it, in units of the stride.
	const anchor = 45
	cls, obj, kps := yunetOutputs(func(stride, i int) (float32, float32) {
		if stride == 16 && i == anchor {
			return 0.9, 0.9
		}
		return 0, 0
	})
	for n := 0; n < 5; n++ {
		kps[1][anchor*10+2*n], kps[1][anchor*10+2*n+1] = float32(n), float32(2*n)
	}
	score, canvas, err := bestYuNetDetection(cls, obj, kps)
	if err != nil || score < 0.89 {
		t.Fatalf("score = %v, %v", score, err)
	}
	for n := 0; n < 5; n++ {
		want := [2]float64{(5 + float64(n)) * 16, (1 + float64(2*n)) * 16}
		if canvas[n] != want {
			t.Fatalf("landmark %d = %v, want %v", n, canvas[n], want)
		}
	}
}

func TestBestYuNetDetectionClampsOutOfRangeValues(t *testing.T) {
	cls, obj, kps := yunetOutputs(func(stride, i int) (float32, float32) {
		if stride == 8 && i == 0 {
			return 5, -3 // an unbounded logit must not turn into a face
		}
		return 0, 0
	})
	if got, _, err := bestYuNetDetection(cls, obj, kps); err != nil || got != 0 {
		t.Fatalf("score = %v, %v; want 0", got, err)
	}
}

func TestBestYuNetDetectionRejectsUnexpectedOutputSizes(t *testing.T) {
	cls, obj, kps := yunetOutputs(func(int, int) (float32, float32) { return 1, 1 })
	kps[2] = kps[2][:10]
	if _, _, err := bestYuNetDetection(cls, obj, kps); err == nil {
		t.Fatal("short landmark output was accepted")
	}
}

func TestToCropCoordsUndoesThePlacementOnTheCanvas(t *testing.T) {
	b := image.Rect(10, 20, 10+80, 20+40) // 2:1 crop at an offset
	x0, y0, w, h := yunetPlacement(b)
	if w != yunetFaceSize || h != yunetFaceSize/2 {
		t.Fatalf("placement size = %dx%d", w, h)
	}
	var canvas [5][2]float64
	for i := range canvas {
		canvas[i] = [2]float64{float64(x0 + w/2), float64(y0 + h/2)} // the middle of the placed crop
	}
	got := toCropCoords(b, canvas)
	if got[0][0] != 50 || got[0][1] != 40 {
		t.Fatalf("centre maps to %v, want the crop centre (50, 40)", got[0])
	}
}
