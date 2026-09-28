package branding

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// hugeDimensionPNGBytes generates a PDW-6 decompression-bomb-shaped PNG: a solid color (so it
// compresses to a small byte count, well under MaxLogoBytes) but with declared pixel
// dimensions far beyond any sane logo — the shape of an attack where the bytes on disk look
// innocuous but decoding (later, by BurnPhoto/ffmpeg's overlay compositing) would allocate
// gigabytes.
func hugeDimensionPNGBytes(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if buf.Len() >= MaxLogoBytes {
		t.Fatalf("test fixture itself is too large (%d bytes): not a useful decompression-bomb shape", buf.Len())
	}
	return buf.Bytes()
}

// TestDecodeAndValidateLogo covers PDW-1: the logo upload must be validated server-side
// (PNG/JPEG, size-limited) before it is ever written to the object store or burned into a
// watermark. A previously broken guard here would let an oversized or forged file through.
func TestDecodeAndValidateLogo(t *testing.T) {
	png4x4, jpeg4x4 := pngBytes(t), jpegBytes(t)
	// side chosen so side*side > MaxLogoPixels while the encoded solid-color PNG still stays
	// comfortably under MaxLogoBytes (asserted by hugeDimensionPNGBytes itself).
	bombPNG := hugeDimensionPNGBytes(t, 2000)

	tests := []struct {
		name        string
		data        []byte
		contentType string
		wantErr     bool
		errContains string
	}{
		{"valid png", png4x4, "image/png", false, ""},
		{"valid jpeg", jpeg4x4, "image/jpeg", false, ""},
		{"empty", nil, "image/png", true, "empty"},
		{"too large", bytes.Repeat([]byte{0}, MaxLogoBytes+1), "image/png", true, "exceeds"},
		{"wrong declared content type", png4x4, "image/gif", true, "PNG or JPEG"},
		{"content type mismatches actual bytes", jpeg4x4, "image/png", true, "not a valid image"},
		{"not an image at all", []byte("not an image"), "image/png", true, "not a valid image"},
		{"huge declared dimensions despite small byte size (decompression bomb)", bombPNG, "image/png", true, "dimensions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := decodeAndValidateLogo(tt.data, tt.contentType)
			if tt.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("want no error, got %v", err)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

// TestUpdateRejectsInvalidTimezone covers PDW-7: an invalid or empty time zone must be
// rejected as a ValidationError before ever reaching the database — validation runs
// synchronously ahead of s.tx, so a zero-value *Service (no Store) can exercise it directly,
// exactly like the existing OwnerName-length and RemoveLogo+Logo checks above it.
func TestUpdateRejectsInvalidTimezone(t *testing.T) {
	tenantID := uuid.New()
	actor := authz.Actor{UserID: uuid.New(), TenantID: &tenantID}
	s := &Service{}

	tests := []struct {
		name string
		tz   string
	}{
		{"not a real IANA name", "Not/AZone"},
		{"empty", ""},
		{"whitespace only", "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tz := tt.tz
			_, err := s.Update(context.Background(), actor, tenantID, Input{Timezone: &tz})
			if err == nil {
				t.Fatalf("Update with timezone %q: want error, got nil", tt.tz)
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("err = %v (%T), want a *ValidationError", err, err)
			}
		})
	}
}

// TestResolveLocation covers PDW-7: an empty timezone (a tenant with no branding row at all)
// resolves to DefaultTimezone, an invalid one degrades to UTC rather than failing outright
// (should be unreachable in practice once Update's own validation is in place, but a watermark
// download must never fail over a time zone lookup), and a valid IANA name resolves to itself.
func TestResolveLocation(t *testing.T) {
	if got := ResolveLocation(""); got.String() != DefaultTimezone {
		t.Errorf(`ResolveLocation("") = %v, want %s`, got, DefaultTimezone)
	}
	if got := ResolveLocation("not/a-real-zone"); got != time.UTC {
		t.Errorf("ResolveLocation(invalid) = %v, want time.UTC", got)
	}
	if got := ResolveLocation("Europe/Madrid"); got.String() != "Europe/Madrid" {
		t.Errorf("ResolveLocation(Europe/Madrid) = %v, want Europe/Madrid", got)
	}
}
