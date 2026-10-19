package faces

import (
	"testing"
	"time"
)

func TestParseTrainFile(t *testing.T) {
	cases := []struct {
		file  string
		seen  int64
		label string
		score float64
	}{
		// Frigate 0.17: <event id>-<unix seconds>-<label>-<score>.webp
		{"1700000000.123456-abc123-1700000005-juan-0.91.webp", 1700000005, "juan", 0.91},
		// Frigate 0.18: the attempt time carries a fraction (live Frigate 0.18.0-77a66e7).
		{"1792014454.926961-ctk7qb-1792014467.366228-unknown-0.webp", 1792014467, "unknown", 0},
		{"1791409133.951967-yqmpa7-1791409142.237417-unknown-0.04.webp", 1791409142, "unknown", 0.04},
	}
	for _, c := range cases {
		seen, label, score := parseTrainFile(c.file)
		if seen == nil || score == nil {
			t.Fatalf("%s: not parsed", c.file)
		}
		if !seen.Equal(time.Unix(c.seen, 0).UTC()) || label != c.label || *score != c.score {
			t.Errorf("%s: got %v %q %v, want %d %q %v", c.file, seen, label, *score, c.seen, c.label, c.score)
		}
	}
}

func TestParseTrainFileRejectsUnknownShapes(t *testing.T) {
	for _, file := range []string{"face.webp", "a-b-c.webp", "x-y-notatime-label-0.5.webp", "x-y-0-label-0.5.webp", "x-y-NaN-label-0.5.webp", "x-y-Inf-label-0.5.webp", "x-y-+Inf-label-0.5.webp", "x-y-1e30-label-0.5.webp"} {
		if seen, _, _ := parseTrainFile(file); seen != nil {
			t.Errorf("%s: parsed a time from an unknown shape", file)
		}
	}
}
