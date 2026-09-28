package watermark

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"
)

func solidJPEG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("time.LoadLocation(%q): %v (tzdata missing? see PDW-7's time/tzdata import)", name, err)
	}
	return loc
}

// TestText covers PDW-7: seenAt is rendered in the given *time.Location with its real numeric
// UTC offset, not fixed UTC — including a nil location (defaults to UTC, e.g. a tenant with
// no branding row at all).
func TestText(t *testing.T) {
	seenAt := time.Date(2026, 9, 28, 16, 5, 30, 0, time.UTC) // 2026-09-28T16:05:30Z

	tests := []struct {
		name      string
		ownerName string
		loc       *time.Location
		want      string
	}{
		{"UTC (nil location)", "Municipalidad de Helvecia", nil, "2026-09-28 16:05:30 +00:00 · Municipalidad de Helvecia"},
		{"empty owner", "", mustLoadLocation(t, "UTC"), "2026-09-28 16:05:30 +00:00"},
		{"whitespace-only owner", "   ", mustLoadLocation(t, "UTC"), "2026-09-28 16:05:30 +00:00"},
		// America/Argentina/Buenos_Aires has been a fixed -03:00 with no DST since 2009: this
		// is PDW-7's own documented default zone.
		{"Buenos Aires (no DST)", "Municipalidad de Helvecia", mustLoadLocation(t, "America/Argentina/Buenos_Aires"), "2026-09-28 13:05:30 -03:00 · Municipalidad de Helvecia"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Text(seenAt, tt.ownerName, tt.loc); got != tt.want {
				t.Errorf("Text() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTextComputesDSTOffsetFromRealTZData covers PDW-7's own explicit "prove the offset is
// computed, not hardcoded" requirement: Europe/Madrid is +01:00 (CET) in winter and +02:00
// (CEST) in summer. Both instants below are the *same* UTC instant class rendered through the
// same *time.Location — if the offset were ever hardcoded (e.g. copy-pasted from the zone's
// "standard" offset) this would fail for one of the two seasons.
func TestTextComputesDSTOffsetFromRealTZData(t *testing.T) {
	madrid := mustLoadLocation(t, "Europe/Madrid")

	winter := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if got, want := Text(winter, "", madrid), "2026-01-15 13:00:00 +01:00"; got != want {
		t.Errorf("winter (CET) Text() = %q, want %q", got, want)
	}

	summer := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	if got, want := Text(summer, "", madrid), "2026-07-15 14:00:00 +02:00"; got != want {
		t.Errorf("summer (CEST) Text() = %q, want %q", got, want)
	}
}

// TestBurnPhoto covers PDW-3: the watermark bar must appear in the bottom region of the
// image, dimensions must be preserved exactly (the file is evidence; cropping or resizing
// would misrepresent it), and it must round-trip as a valid, decodable JPEG.
func TestBurnPhoto(t *testing.T) {
	const w, h = 320, 180
	bg := color.RGBA{R: 40, G: 40, B: 40, A: 255}
	src := solidJPEG(t, w, h, bg)

	out, err := BurnPhoto(src, "2026-09-28 13:05:30 UTC+00:00 · Test Owner", nil)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("output is not a valid JPEG: %v", err)
	}
	if b := decoded.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("dimensions changed: got %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
	}

	// The watermark bar sits in the bottom ~1/7 of the image (barHeight = h/14, clamped to
	// [24,64]; for h=180 that is max(180/14, 24) = 24px): its pixels must differ noticeably
	// from the original uniform background, proving something was actually drawn there.
	bottomY := h - 5
	r, g, b, _ := decoded.At(10, bottomY).RGBA()
	diff := absInt(int(r>>8)-int(bg.R)) + absInt(int(g>>8)-int(bg.G)) + absInt(int(b>>8)-int(bg.B))
	if diff < 30 {
		t.Errorf("bottom-bar pixel barely changed from background (diff=%d): watermark bar was not drawn", diff)
	}

	// The top of the image, well above the bar, must be unchanged (within JPEG re-encode
	// noise), proving the watermark did not bleed outside its bar.
	r, g, b, _ = decoded.At(10, 5).RGBA()
	diff = absInt(int(r>>8)-int(bg.R)) + absInt(int(g>>8)-int(bg.G)) + absInt(int(b>>8)-int(bg.B))
	if diff > 15 {
		t.Errorf("top-of-image pixel changed (diff=%d): watermark bar leaked outside its region", diff)
	}
}

// TestPhotoJPEGQualityIsMaximum covers the PDW-6 nit: the user asked for maximum JPEG
// quality on watermarked photo downloads, since the file is evidence.
func TestPhotoJPEGQualityIsMaximum(t *testing.T) {
	if photoJPEGQuality != 100 {
		t.Errorf("photoJPEGQuality = %d, want 100 (maximum)", photoJPEGQuality)
	}
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
