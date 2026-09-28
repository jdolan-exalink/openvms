package branding

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
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

// TestDecodeAndValidateLogo covers PDW-1: the logo upload must be validated server-side
// (PNG/JPEG, size-limited) before it is ever written to the object store or burned into a
// watermark. A previously broken guard here would let an oversized or forged file through.
func TestDecodeAndValidateLogo(t *testing.T) {
	png4x4, jpeg4x4 := pngBytes(t), jpegBytes(t)

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
