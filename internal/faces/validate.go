package faces

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg" // format sniffing and header decoding only
	_ "image/png"  // format sniffing and header decoding only
	"strings"
	"unicode"

	_ "golang.org/x/image/webp" // format sniffing and header decoding only
)

const (
	// MaxImageBytes limits one enrollment image.
	MaxImageBytes = 5 << 20
	// MaxImagePixels bounds the declared pixel count (width*height) independent of the encoded
	// size, so a tiny but highly compressible file cannot later decode into gigabytes.
	MaxImagePixels = 16 << 20
	// MaxImagesPerPerson caps the enrollment set of one person.
	MaxImagesPerPerson = 20
	// MaxDisplayNameLen and MaxNotesLen mirror the table CHECK constraints.
	MaxDisplayNameLen = 120
	MaxNotesLen       = 2000

	// maxFrigateSlug leaves room for the "_xxxxxx" collision suffix inside Frigate's 50 char limit.
	maxFrigateSlug = 40
	fallbackSlug   = "person"
)

// ValidationError is returned for a well-formed but unacceptable request.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

var accentFold = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ñ", "n", "ç", "c",
)

// NormalizeFrigateName turns a display name into a Frigate-safe identifier: lowercase ASCII
// letters, digits and underscores only, at most maxFrigateSlug characters. A name with no
// usable characters maps to "person". It is computed once at creation and then kept: renaming
// the display name never changes the Frigate identifier.
func NormalizeFrigateName(display string) string {
	var b strings.Builder
	lastUnderscore := true // suppresses leading underscores
	for _, r := range accentFold.Replace(strings.ToLower(display)) {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		// Everything else (spaces, punctuation, unfolded non-ASCII) is a single separator.
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	s := strings.Trim(b.String(), "_")
	if len(s) > maxFrigateSlug {
		s = strings.Trim(s[:maxFrigateSlug], "_")
	}
	if s == "" {
		return fallbackSlug
	}
	return s
}

type imageMeta struct {
	ContentType   string
	Size          int
	SHA256        string
	Width, Height int
}

// validateImage checks an enrollment image server-side: allowed type, size, a body that
// really is that type, and bounded declared dimensions.
func validateImage(data []byte, contentType string) (imageMeta, error) {
	if len(data) == 0 {
		return imageMeta{}, invalid("image is empty")
	}
	if len(data) > MaxImageBytes {
		return imageMeta{}, invalid("image exceeds %d MB", MaxImageBytes>>20)
	}
	ct := strings.ToLower(strings.TrimSpace(contentType))
	want := map[string]string{"image/jpeg": "jpeg", "image/png": "png", "image/webp": "webp"}[ct]
	if want == "" {
		return imageMeta{}, invalid("image must be JPEG, PNG or WebP")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return imageMeta{}, invalid("image is not a valid image: %s", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return imageMeta{}, invalid("image is not a valid image")
	}
	if format != want {
		return imageMeta{}, invalid("declared content type %s does not match the file", ct)
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxImagePixels {
		return imageMeta{}, invalid("image dimensions (%dx%d) are too large (max %d pixels)", cfg.Width, cfg.Height, MaxImagePixels)
	}
	sum := sha256.Sum256(data)
	return imageMeta{ContentType: ct, Size: len(data), SHA256: hex.EncodeToString(sum[:]), Width: cfg.Width, Height: cfg.Height}, nil
}
