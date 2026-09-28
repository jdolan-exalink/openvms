package clipwatermark

import (
	"slices"
	"strings"
	"testing"
)

// TestBuildFFmpegArgsNoLogo covers PDW-4: without a logo, the filter is a plain -vf
// drawtext, H.264/AAC output, veryfast preset, a fixed low thread count.
func TestBuildFFmpegArgsNoLogo(t *testing.T) {
	args := buildFFmpegArgs("/tmp/in.mp4", "/tmp/out.mp4", "2026-09-28 13:05:30 UTC+00:00", "")

	mustContainSeq(t, args, []string{"-i", "/tmp/in.mp4"})
	mustContainSeq(t, args, []string{"-vf", "drawtext=text='2026-09-28 13\\:05\\:30 UTC+00\\:00':fontcolor=white:fontsize=24:box=1:boxcolor=black@0.6:boxborderw=8:x=10:y=h-th-10"})
	mustContainSeq(t, args, []string{"-c:v", "libx264"})
	mustContainSeq(t, args, []string{"-preset", "veryfast"})
	mustContainSeq(t, args, []string{"-threads", "2"})
	mustContainSeq(t, args, []string{"-c:a", "aac"})
	if args[len(args)-1] != "/tmp/out.mp4" {
		t.Errorf("last arg = %q, want output path", args[len(args)-1])
	}
	if slices.Contains(args, "-filter_complex") {
		t.Errorf("no logo: must not use -filter_complex, got %v", args)
	}
}

// TestBuildFFmpegArgsWithLogo covers PDW-4: a logo adds a second input and switches to
// -filter_complex compositing it with overlay before drawtext.
func TestBuildFFmpegArgsWithLogo(t *testing.T) {
	args := buildFFmpegArgs("/tmp/in.mp4", "/tmp/out.mp4", "text", "/tmp/logo.png")

	mustContainSeq(t, args, []string{"-i", "/tmp/in.mp4"})
	mustContainSeq(t, args, []string{"-i", "/tmp/logo.png"})
	idx := indexOf(args, "-filter_complex")
	if idx == -1 || idx+1 >= len(args) {
		t.Fatalf("missing -filter_complex, got %v", args)
	}
	filter := args[idx+1]
	if !strings.Contains(filter, "overlay=") {
		t.Errorf("filter_complex = %q, want an overlay stage", filter)
	}
	if !strings.Contains(filter, "drawtext=") {
		t.Errorf("filter_complex = %q, want the drawtext stage too", filter)
	}
	if slices.Contains(args, "-vf") {
		t.Errorf("with logo: must not also use -vf, got %v", args)
	}
}

// TestEscapeDrawtext covers PDW-4's explicit requirement: drawtext escaping of the
// timestamp (colons) and owner name (which may contain quotes, backslashes or percent
// signs) must not let user-configured branding text break out of the filter value or be
// misinterpreted as strftime expansion.
func TestEscapeDrawtext(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"colon", "13:05:30", `13\:05\:30`},
		{"single quote", "O'Brien's Garage", `O\'Brien\'s Garage`},
		{"backslash", `C:\Users`, `C\:\\Users`},
		{"percent", "100% Towing", `100\% Towing`},
		{"plain text has no escapes", "Municipalidad de Helvecia", "Municipalidad de Helvecia"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeDrawtext(tt.input); got != tt.want {
				t.Errorf("escapeDrawtext(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestDrawtextFilterIsWellFormed guards against a regression where an unescaped owner name
// containing a single quote would prematurely close the text='...' value, corrupting the
// rest of the filter graph (e.g. turning boxcolor into literal drawtext output).
func TestDrawtextFilterIsWellFormed(t *testing.T) {
	filter := drawtextFilter("2026-09-28 13:05:30 UTC+00:00 · O'Brien's Towing")
	// Exactly two unescaped single quotes should remain: the ones opening/closing the text
	// value. Count quotes not preceded by a backslash.
	unescaped := 0
	for i, r := range filter {
		if r == '\'' && (i == 0 || filter[i-1] != '\\') {
			unescaped++
		}
	}
	if unescaped != 2 {
		t.Errorf("filter has %d unescaped single quotes, want exactly 2 (open/close of text='...'): %s", unescaped, filter)
	}
}

func mustContainSeq(t *testing.T, args, seq []string) {
	t.Helper()
	if indexOfSeq(args, seq) == -1 {
		t.Fatalf("args %v does not contain sequence %v", args, seq)
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func indexOfSeq(s, seq []string) int {
	for i := 0; i+len(seq) <= len(s); i++ {
		match := true
		for j, v := range seq {
			if s[i+j] != v {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
