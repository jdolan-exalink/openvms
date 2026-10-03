package vehicle

import (
	"image"
	"image/color"
	"testing"
)

func TestDecideBodyNamesAVan(t *testing.T) {
	logits := []float32{0.2, 4.5, 0, 0, 0.1, 1.0, 0.4, 0.3}
	typ, conf, ok := decideBody(logits)
	if !ok || typ != "van" {
		t.Fatalf("got %s %.2f ok=%v", typ, conf, ok)
	}
	if conf < bodyMinConfidence {
		t.Fatalf("confidence %.2f", conf)
	}
}

func TestDecideBodyDropsARacingCar(t *testing.T) {
	logits := []float32{1, 0.4, 0, 0, 6, 1, 0.2, 0.2}
	if _, _, ok := decideBody(logits); ok {
		t.Fatal("open wheel was accepted")
	}
}

func TestDecideBodyNamesASedan(t *testing.T) {
	logits := []float32{0, 0, 0, 0, 0, 6, 1, 0}
	typ, _, ok := decideBody(logits)
	if !ok || typ != "sedan" {
		t.Fatalf("got %s ok=%v", typ, ok)
	}
}

func TestDecideBodyDropsATie(t *testing.T) {
	logits := []float32{0.1, 0.1, 0.1, 0.1, 0, 0.2, 0.1, 0.1}
	if _, _, ok := decideBody(logits); ok {
		t.Fatal("a flat distribution was accepted")
	}
}

func TestApplyBodyKeepsMotorcycle(t *testing.T) {
	typ, conf, fromModel := applyBody("motorcycle", 0.8, "van", 0.95, true, true, false)
	if typ != "motorcycle" || conf != 0.8 || fromModel {
		t.Fatalf("got %s %.2f model=%v", typ, conf, fromModel)
	}
}

func TestApplyBodyUsesTheCropOnACar(t *testing.T) {
	typ, conf, fromModel := applyBody("car", 0.75, "pickup", 0.91, true, true, false)
	if typ != "pickup" || conf != 0.91 || !fromModel {
		t.Fatalf("got %s %.2f model=%v", typ, conf, fromModel)
	}
}

func TestApplyBodyLeavesAutoWithoutAPercentWhenUnsure(t *testing.T) {
	typ, conf, fromModel := applyBody("car", 0.75, "", 0, true, false, false)
	if typ != "car" || conf != 0 || fromModel {
		t.Fatalf("got %s %.2f model=%v", typ, conf, fromModel)
	}
}

func TestApplyBodyKeepsALongVan(t *testing.T) {
	typ, _, fromModel := applyBody("car", 0.75, "van", 0.91, true, true, true)
	if typ != "van" || !fromModel {
		t.Fatalf("got %s model=%v", typ, fromModel)
	}
}

func TestApplyBodyKeepsAModestSedanOnALongBox(t *testing.T) {
	typ, conf, fromModel := applyBody("car", 0.75, "sedan", 0.50, true, true, true)
	if typ != "sedan" || conf != 0.50 || !fromModel {
		t.Fatalf("got %s %.2f model=%v", typ, conf, fromModel)
	}
}

func TestApplyBodyStillSplitsAnUnsureLongBox(t *testing.T) {
	typ, conf, fromModel := applyBody("car", 0.75, "", 0, true, false, true)
	if typ != "truck_trailer" || conf != 0.72 || fromModel {
		t.Fatalf("got %s %.2f model=%v", typ, conf, fromModel)
	}
}

func TestBodyViewUsesTheGreenBox(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 140))
	for y := 0; y < 140; y++ {
		for x := 0; x < 200; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 140, G: 140, B: 140, A: 255})
		}
	}
	green := color.RGBA{R: 20, G: 220, B: 20, A: 255}
	for x := 30; x <= 160; x++ {
		img.SetRGBA(x, 24, green)
		img.SetRGBA(x, 110, green)
	}
	for y := 24; y <= 110; y++ {
		img.SetRGBA(30, y, green)
		img.SetRGBA(160, y, green)
	}
	view, ok := bodyView(img)
	if !ok {
		t.Fatal("green box was not found")
	}
	if view.Bounds().Dx() >= 200 || view.Bounds().Dy() >= 140 {
		t.Fatalf("crop %v is the whole frame", view.Bounds())
	}
}

func TestBodyViewWithoutABoxDoesNotReadTheStreet(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 80, 60))
	if _, ok := bodyView(img); ok {
		t.Fatal("a frame without a vehicle box was classified")
	}
}

func TestPrepareBodyIsNCHW224(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	data := prepareBody(img)
	if len(data) != 3*224*224 {
		t.Fatalf("len %d", len(data))
	}
}
