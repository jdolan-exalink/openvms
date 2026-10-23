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

func yunetOutputs(fill func(stride int, i int) (cls, obj float32)) (cls, obj [3][]float32) {
	for s, stride := range yunetStrides {
		n := yunetAnchors(stride)
		cls[s], obj[s] = make([]float32, n), make([]float32, n)
		for i := 0; i < n; i++ {
			cls[s][i], obj[s][i] = fill(stride, i)
		}
	}
	return cls, obj
}

func TestBestYuNetScoreTakesTheStrongestAnchorOfAnyStride(t *testing.T) {
	cls, obj := yunetOutputs(func(stride, i int) (float32, float32) {
		if stride == 16 && i == 7 {
			return 0.81, 0.81
		}
		return 0.1, 0.1
	})
	got, err := bestYuNetScore(cls, obj)
	if err != nil || got < 0.80 || got > 0.82 {
		t.Fatalf("score = %v, %v; want about 0.81", got, err)
	}
}

func TestBestYuNetScoreClampsOutOfRangeValues(t *testing.T) {
	cls, obj := yunetOutputs(func(stride, i int) (float32, float32) {
		if stride == 8 && i == 0 {
			return 5, -3 // an unbounded logit must not turn into a face
		}
		return 0, 0
	})
	if got, err := bestYuNetScore(cls, obj); err != nil || got != 0 {
		t.Fatalf("score = %v, %v; want 0", got, err)
	}
}

func TestBestYuNetScoreRejectsUnexpectedOutputSizes(t *testing.T) {
	cls, obj := yunetOutputs(func(int, int) (float32, float32) { return 1, 1 })
	cls[1] = cls[1][:10]
	if _, err := bestYuNetScore(cls, obj); err == nil {
		t.Fatal("short output was accepted")
	}
}
