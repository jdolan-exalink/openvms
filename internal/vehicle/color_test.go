package vehicle

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func solid(c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestClassifyColorSolidPaints(t *testing.T) {
	cases := []struct {
		name string
		c    color.RGBA
		want string
	}{
		{"red", color.RGBA{180, 20, 20, 255}, "red"},
		{"blue", color.RGBA{20, 40, 180, 255}, "blue"},
		{"white", color.RGBA{245, 245, 245, 255}, "white"},
		{"black", color.RGBA{30, 30, 30, 255}, "black"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyColor(solid(tc.c))
			if got.Name != tc.want {
				t.Fatalf("color %s, want %s (conf %.2f quality %s)", got.Name, tc.want, got.Confidence, got.Quality)
			}
			if got.Confidence < colorMinConfidence {
				t.Fatalf("confidence %.2f below the display floor", got.Confidence)
			}
		})
	}
}

func TestClassifyColorKeepsBlackBodyOverBlueWindow(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 90, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 90; x++ {
			img.SetRGBA(x, y, color.RGBA{30, 30, 30, 255})
		}
	}
	for y := 10; y < 80; y++ {
		img.SetRGBA(10, y, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(11, y, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(78, y, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(79, y, color.RGBA{0, 255, 0, 255})
	}
	for x := 10; x < 80; x++ {
		img.SetRGBA(x, 10, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(x, 11, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(x, 78, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(x, 79, color.RGBA{0, 255, 0, 255})
	}
	for y := 30; y < 55; y++ {
		for x := 30; x < 60; x++ {
			img.SetRGBA(x, y, color.RGBA{20, 40, 180, 255})
		}
	}
	got := ClassifyColor(img)
	if got.Name != "black" {
		t.Fatalf("black body with a blue window = %+v, want black", got)
	}
}

func TestClassifyClothingSplitsUpperAndLower(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 80, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 80; x++ {
			img.SetRGBA(x, y, color.RGBA{140, 140, 140, 255})
		}
	}
	for y := 20; y < 100; y++ {
		img.SetRGBA(15, y, color.RGBA{230, 210, 20, 255})
		img.SetRGBA(16, y, color.RGBA{230, 210, 20, 255})
		img.SetRGBA(63, y, color.RGBA{230, 210, 20, 255})
		img.SetRGBA(64, y, color.RGBA{230, 210, 20, 255})
	}
	for x := 15; x < 65; x++ {
		img.SetRGBA(x, 20, color.RGBA{230, 210, 20, 255})
		img.SetRGBA(x, 21, color.RGBA{230, 210, 20, 255})
		img.SetRGBA(x, 98, color.RGBA{230, 210, 20, 255})
		img.SetRGBA(x, 99, color.RGBA{230, 210, 20, 255})
	}
	for y := 28; y < 60; y++ {
		for x := 20; x < 60; x++ {
			img.SetRGBA(x, y, color.RGBA{20, 40, 180, 255})
		}
	}
	for y := 60; y < 95; y++ {
		for x := 20; x < 60; x++ {
			img.SetRGBA(x, y, color.RGBA{180, 20, 20, 255})
		}
	}
	got := ClassifyClothing(img)
	if got.Upper.Name != "blue" || got.Lower.Name != "red" {
		t.Fatalf("clothing = upper %+v lower %+v, want blue / red", got.Upper, got.Lower)
	}
}

func TestGreenDetectionBoxIsNotThePaint(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 300; x++ {
			img.SetRGBA(x, y, color.RGBA{150, 150, 148, 255})
		}
	}
	for y := 90; y < 210; y++ {
		for x := 70; x < 230; x++ {
			img.SetRGBA(x, y, color.RGBA{186, 188, 190, 255})
		}
	}
	for y := 80; y < 220; y++ {
		for _, x := range []int{60, 61, 62, 238, 239, 240} {
			img.SetRGBA(x, y, color.RGBA{30, 210, 40, 255})
		}
	}
	for x := 60; x < 241; x++ {
		for _, y := range []int{80, 81, 82, 218, 219, 220} {
			img.SetRGBA(x, y, color.RGBA{30, 210, 40, 255})
		}
	}
	for y := 58; y < 78; y++ {
		for x := 60; x < 190; x++ {
			img.SetRGBA(x, y, color.RGBA{20, 200, 30, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 75}); err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got := ClassifyColor(decoded)
	if got.Name == "green" || got.Name == "unknown" {
		t.Fatalf("silver body inside a green box = %+v", got)
	}
}

func TestLimeBodyStaysGreen(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 300; x++ {
			img.SetRGBA(x, y, color.RGBA{120, 220, 40, 255})
		}
	}
	for y := 40; y < 260; y++ {
		img.SetRGBA(30, y, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(270, y, color.RGBA{0, 255, 0, 255})
	}
	for x := 30; x < 271; x++ {
		img.SetRGBA(x, 40, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(x, 260, color.RGBA{0, 255, 0, 255})
	}
	got := ClassifyColor(img)
	if got.Name != "green" {
		t.Fatalf("lime body = %+v, want green", got)
	}
}

func TestNeutralShadeNames(t *testing.T) {
	cases := []struct {
		name string
		c    color.RGBA
		want string
	}{
		{"white", color.RGBA{245, 245, 245, 255}, "white"},
		{"off white", color.RGBA{230, 230, 230, 255}, "white"},
		{"light gray", color.RGBA{190, 190, 190, 255}, "silver"},
		{"medium gray", color.RGBA{140, 140, 140, 255}, "gray"},
		{"dark gray", color.RGBA{90, 90, 90, 255}, "gray"},
		{"black", color.RGBA{30, 30, 30, 255}, "black"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			img := solid(tc.c)
			got := voteColor(img, img.Bounds(), true)
			if got.Name != tc.want {
				t.Fatalf("%s = %+v, want %s", tc.name, got, tc.want)
			}
		})
	}
}

func TestBluePersonBoxReadsTheClothesNotTheStreet(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 220, 220))
	for y := 0; y < 220; y++ {
		for x := 0; x < 220; x++ {
			img.SetRGBA(x, y, color.RGBA{190, 190, 190, 255})
		}
	}
	for y := 40; y < 190; y++ {
		for x := 70; x < 150; x++ {
			img.SetRGBA(x, y, color.RGBA{12, 12, 14, 255})
		}
	}
	slate := color.RGBA{70, 90, 140, 255}
	for y := 30; y < 200; y++ {
		for _, x := range []int{40, 41, 42, 178, 179, 180} {
			img.SetRGBA(x, y, slate)
		}
	}
	for x := 40; x < 181; x++ {
		for _, y := range []int{30, 31, 32, 197, 198, 199} {
			img.SetRGBA(x, y, slate)
		}
	}
	got := ClassifyClothing(img)
	if got.Upper.Name != "black" || got.Lower.Name != "black" {
		t.Fatalf("black clothes in a blue box = upper %+v lower %+v, want black / black", got.Upper, got.Lower)
	}
}

func TestClothingWithoutABoxDoesNotReadTheStreet(t *testing.T) {
	got := ClassifyClothing(solid(color.RGBA{190, 190, 190, 255}))
	if got.Upper.Name != "unknown" || got.Lower.Name != "unknown" {
		t.Fatalf("street without a person box = upper %+v lower %+v, want unknown", got.Upper, got.Lower)
	}
}

func TestLoosePersonBoxKeepsTheBlackJacket(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 300; x++ {
			img.SetRGBA(x, y, color.RGBA{186, 186, 184, 255})
		}
	}
	for y := 70; y < 250; y++ {
		for x := 120; x < 180; x++ {
			img.SetRGBA(x, y, color.RGBA{8, 8, 10, 255})
		}
	}
	yellow := color.RGBA{230, 210, 20, 255}
	for y := 20; y < 280; y++ {
		for _, x := range []int{15, 16, 17, 282, 283, 284} {
			img.SetRGBA(x, y, yellow)
		}
	}
	for x := 15; x < 285; x++ {
		for _, y := range []int{20, 21, 22, 277, 278, 279} {
			img.SetRGBA(x, y, yellow)
		}
	}
	got := ClassifyClothing(img)
	if got.Upper.Name != "black" || got.Lower.Name != "black" {
		t.Fatalf("black jacket inside a wide box = upper %+v lower %+v, want black / black", got.Upper, got.Lower)
	}
}

func TestWhiteBodyOnGrayPavement(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 140))
	for y := 0; y < 140; y++ {
		for x := 0; x < 200; x++ {
			img.SetRGBA(x, y, color.RGBA{140, 140, 140, 255})
		}
	}
	for y := 40; y < 110; y++ {
		for x := 50; x < 150; x++ {
			img.SetRGBA(x, y, color.RGBA{232, 232, 232, 255})
		}
	}
	for y := 55; y < 80; y++ {
		for x := 70; x < 120; x++ {
			img.SetRGBA(x, y, color.RGBA{30, 32, 36, 255})
		}
	}
	got := ClassifyColor(img)
	if got.Name != "white" {
		t.Fatalf("white body on gray pavement = %+v, want white", got)
	}
}

func TestSmallGlareDoesNotWhitenADarkVehicle(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 140))
	for y := 0; y < 140; y++ {
		for x := 0; x < 200; x++ {
			img.SetRGBA(x, y, color.RGBA{150, 150, 150, 255})
		}
	}
	for y := 50; y < 110; y++ {
		for x := 60; x < 140; x++ {
			img.SetRGBA(x, y, color.RGBA{36, 36, 38, 255})
		}
	}
	for y := 58; y < 64; y++ {
		for x := 120; x < 128; x++ {
			img.SetRGBA(x, y, color.RGBA{250, 250, 250, 255})
		}
	}
	got := voteColor(img, img.Bounds(), true)
	if got.Name == "white" {
		t.Fatalf("dark vehicle with a small glare = %+v, want a dark neutral", got)
	}
}

func TestClassifyColorRefusesGrayInfrared(t *testing.T) {
	got := ClassifyColor(solid(color.RGBA{128, 128, 128, 255}))
	if got.Name != "unknown" || got.Quality != qualityLow {
		t.Fatalf("gray frame = %+v, want unknown/low", got)
	}
}

func TestLongBoxIsATruckAndTrailer(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 360, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 360; x++ {
			img.SetRGBA(x, y, color.RGBA{120, 120, 118, 255})
		}
	}
	for y := 30; y < 95; y++ {
		for x := 20; x < 110; x++ {
			img.SetRGBA(x, y, color.RGBA{236, 236, 236, 255})
		}
	}
	for y := 40; y < 70; y++ {
		for x := 40; x < 80; x++ {
			img.SetRGBA(x, y, color.RGBA{20, 24, 28, 255})
		}
	}
	for y := 30; y < 95; y++ {
		for x := 110; x < 330; x++ {
			img.SetRGBA(x, y, color.RGBA{210, 40, 90, 255})
		}
	}
	for y := 24; y < 100; y++ {
		img.SetRGBA(14, y, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(15, y, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(336, y, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(337, y, color.RGBA{0, 255, 0, 255})
	}
	for x := 14; x < 338; x++ {
		img.SetRGBA(x, 24, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(x, 25, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(x, 98, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(x, 99, color.RGBA{0, 255, 0, 255})
	}
	cab, trailer, ok := ClassifyRig(img)
	if !ok || cab.Name != "white" || trailer.Name != "red" {
		t.Fatalf("rig = ok %v cab %+v trailer %+v, want white / red", ok, cab, trailer)
	}
}

func TestSquareBoxIsNotATrailer(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 160, 140))
	for y := 0; y < 140; y++ {
		for x := 0; x < 160; x++ {
			img.SetRGBA(x, y, color.RGBA{30, 30, 30, 255})
		}
	}
	for y := 30; y < 110; y++ {
		img.SetRGBA(30, y, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(31, y, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(120, y, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(121, y, color.RGBA{0, 255, 0, 255})
	}
	for x := 30; x < 122; x++ {
		img.SetRGBA(x, 30, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(x, 31, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(x, 108, color.RGBA{0, 255, 0, 255})
		img.SetRGBA(x, 109, color.RGBA{0, 255, 0, 255})
	}
	if _, _, ok := ClassifyRig(img); ok {
		t.Fatal("a short box was called a trailer")
	}
}

func TestTypeFromLabelsDoesNotInventSUV(t *testing.T) {
	typ, conf := TypeFromLabels([]string{"car"})
	if typ != "car" || conf < TypeMinConfidence {
		t.Fatalf("car label = %s %.2f", typ, conf)
	}
	typ, _ = TypeFromLabels([]string{"person", "dog"})
	if typ != "unknown" {
		t.Fatalf("person = %s, want unknown", typ)
	}
	if !IsVehicle([]string{"person", "truck"}) || IsVehicle([]string{"person"}) {
		t.Fatal("vehicle gate mismatch")
	}
	typ, _ = TypeFromLabels([]string{"motorcycle", "person", "car"})
	if typ != "motorcycle" {
		t.Fatalf("motorcycle first = %s, want motorcycle", typ)
	}
	typ, _ = TypeFromLabels([]string{"car", "motorcycle"})
	if typ != "car" {
		t.Fatalf("car first = %s, want car", typ)
	}
	typ, _ = TypeFromLabels([]string{"bicycle", "motorcycle"})
	if typ != "motorcycle" {
		t.Fatalf("bicycle+motorcycle = %s, want motorcycle", typ)
	}
}
