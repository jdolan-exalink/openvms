package faces

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestNormalizeFrigateName(t *testing.T) {
	tests := map[string]string{
		"Juan Pérez":      "juan_perez",
		"  María-José  ":  "maria_jose",
		"Núñez, Ana":      "nunez_ana",
		"李雷":              "person",
		"A/B\\C..":        "a_b_c",
		"UPPER_lower 123": "upper_lower_123",
	}
	for in, want := range tests {
		if got := NormalizeFrigateName(in); got != want {
			t.Errorf("NormalizeFrigateName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := NormalizeFrigateName(strings.Repeat("a", 100)); len(got) > maxFrigateSlug {
		t.Errorf("slug too long: %d", len(got))
	}
}

func encode(t *testing.T, format string, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buf, img)
	default:
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestValidateImage(t *testing.T) {
	png8, jpg8 := encode(t, "png", 8, 8), encode(t, "jpeg", 8, 8)
	// Smallest valid lossy WebP (1x1).
	webp, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		data    []byte
		ct      string
		wantErr string
	}{
		{"png ok", png8, "image/png", ""},
		{"jpeg ok", jpg8, "image/jpeg", ""},
		{"jpeg with params and case", jpg8, " Image/JPEG ", ""},
		{"webp ok", webp, "image/webp", ""},
		{"empty", nil, "image/png", "empty"},
		{"too large", bytes.Repeat([]byte{1}, MaxImageBytes+1), "image/png", "exceeds"},
		{"gif rejected", []byte("GIF89a"), "image/gif", "JPEG, PNG or WebP"},
		{"mismatch", jpg8, "image/png", "does not match"},
		{"garbage", []byte("not an image"), "image/png", "not a valid image"},
		{"bomb", encode(t, "png", 5000, 5000), "image/png", "dimensions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := validateImage(tt.data, tt.ct)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if meta.SHA256 == "" || meta.Size != len(tt.data) {
					t.Errorf("bad meta %+v", meta)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("want *ValidationError, got %T", err)
			}
		})
	}
}

func TestUnreachableKeepsCallerErrorsAndSanitizesTheRest(t *testing.T) {
	var ve *ValidationError
	if err := unreachable(invalid("bad")); !errors.As(err, &ve) {
		t.Fatalf("validation error was rewritten to %T", err)
	}
	var re *RemoteError
	err := unreachable(errors.New("dial tcp 10.0.0.5:5000: connection refused"))
	if !errors.As(err, &re) || strings.Contains(re.Reason, "10.0.0.5") {
		t.Fatalf("remote failure = %v, want a sanitized RemoteError", err)
	}
}
