//go:build cgo

package vehicle

import (
	"bytes"
	"image"
	"image/color"
	_ "image/jpeg"
	"os"
	"testing"
)

func TestBodyModelLoadsAndScoresEightClasses(t *testing.T) {
	model := os.Getenv("VEHICLE_BODY_MODEL")
	library := os.Getenv("ONNXRUNTIME_SHARED_LIBRARY")
	if model == "" || library == "" {
		t.Skip("model path not set")
	}
	clf, err := OpenBodyClassifier(model, library)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 180, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 180; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 240, G: 240, B: 240, A: 255})
		}
	}
	for y := 30; y < 80; y++ {
		for x := 20; x < 160; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 30, G: 30, B: 30, A: 255})
		}
	}
	logits, err := clf.logitsForTest(img)
	if err != nil {
		t.Fatal(err)
	}
	if len(logits) != 8 {
		t.Fatalf("logits %v", logits)
	}
	for i, v := range logits {
		if v != v || v > 1e6 || v < -1e6 {
			t.Fatalf("logit %d = %v", i, v)
		}
	}
	typ, conf, ok := decideBody(logits)
	t.Logf("logits %v -> %s %.2f accepted=%v", logits, typ, conf, ok)
}

func TestBodyModelOnSampleCrops(t *testing.T) {
	dir := os.Getenv("VEHICLE_BODY_SAMPLES")
	model := os.Getenv("VEHICLE_BODY_MODEL")
	library := os.Getenv("ONNXRUNTIME_SHARED_LIBRARY")
	if dir == "" || model == "" || library == "" {
		t.Skip("sample dir not set")
	}
	clf, err := OpenBodyClassifier(model, library)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := os.ReadFile(dir + "/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		img, _, err := image.Decode(bytes.NewReader(body))
		if err != nil {
			t.Logf("%s decode: %v", entry.Name(), err)
			continue
		}
		view, boxed := bodyView(img)
		if !boxed {
			t.Logf("%s no-box", entry.Name())
			continue
		}
		logits, err := clf.logitsForTest(view)
		if err != nil {
			t.Fatal(err)
		}
		lead, leadConf, _ := leadingBody(logits)
		typ, conf, ok := decideBody(logits)
		t.Logf("%s lead=%s %.2f accepted=%s %.2f ok=%v", entry.Name(), lead, leadConf, typ, conf, ok)
	}
}
