package clipwatermark

import (
	"os"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/watermark"
)

// TestWriteTempTextContentMatchesWatermarkTextExactly covers PDW-6 finding #2: the text
// burned into the clip (via drawtext's textfile=) must equal watermark.Text's output exactly,
// byte for byte — no escaping, no truncation, no re-encoding — since that function is the one
// source of truth shared with the photo burn-in and the web CSS overlay ("what you see is
// what gets burned in").
func TestWriteTempTextContentMatchesWatermarkTextExactly(t *testing.T) {
	seenAt := time.Date(2026, 9, 28, 13, 5, 30, 0, time.UTC)
	want := watermark.Text(seenAt, "O'Brien's 100% Towing · Étoile", nil)

	path, cleanup, err := writeTempText(want)
	if err != nil {
		t.Fatalf("writeTempText: %v", err)
	}
	defer cleanup()

	got, err := os.ReadFile(path) //nolint:gosec // path is writeTempText's own return value in this test
	if err != nil {
		t.Fatalf("read temp text file: %v", err)
	}
	if string(got) != want {
		t.Errorf("temp text file content = %q, want exactly %q", got, want)
	}
}

// TestWriteTempTextCleanup covers PDW-6: cleanup must actually remove the file (and its
// private directory), so a burst of clip jobs does not leak temp files.
func TestWriteTempTextCleanup(t *testing.T) {
	path, cleanup, err := writeTempText("2026-09-28 13:05:30 -03:00")
	if err != nil {
		t.Fatalf("writeTempText: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("temp text file does not exist before cleanup: %v", err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temp text file still exists after cleanup: err = %v", err)
	}
}
