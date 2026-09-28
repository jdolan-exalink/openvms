package clipwatermark

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestBuildFFmpegArgsNoLogo covers PDW-4/PDW-6: without a logo, the filter is a plain -vf
// drawtext reading the watermark text from a file (textfile=), never inlined into the
// filtergraph, with expansion disabled; H.264/AAC output, veryfast preset, a fixed low thread
// count.
func TestBuildFFmpegArgsNoLogo(t *testing.T) {
	textPath := filepath.Join(t.TempDir(), "watermark.txt")
	args, err := buildFFmpegArgs("/tmp/in.mp4", "/tmp/out.mp4", textPath, "")
	if err != nil {
		t.Fatalf("buildFFmpegArgs: %v", err)
	}

	mustContainSeq(t, args, []string{"-i", "/tmp/in.mp4"})
	idx := indexOf(args, "-vf")
	if idx == -1 || idx+1 >= len(args) {
		t.Fatalf("missing -vf, got %v", args)
	}
	filter := args[idx+1]
	if !strings.Contains(filter, "drawtext=textfile='"+textPath+"'") {
		t.Errorf("filter = %q, want drawtext=textfile='%s'", filter, textPath)
	}
	if !strings.Contains(filter, "expansion=none") {
		t.Errorf("filter = %q, want expansion=none so drawtext never interprets %%{...} in the text file", filter)
	}
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

// TestBuildFFmpegArgsWithLogo covers PDW-4/PDW-6: a logo adds a second input and switches to
// -filter_complex compositing it with overlay before drawtext, and explicitly maps the
// filtered video plus the source's (optional) audio stream, since -filter_complex drops
// ffmpeg's implicit "map everything" default that -vf keeps (PDW-6 finding: silent audio loss).
func TestBuildFFmpegArgsWithLogo(t *testing.T) {
	textPath := filepath.Join(t.TempDir(), "watermark.txt")
	args, err := buildFFmpegArgs("/tmp/in.mp4", "/tmp/out.mp4", textPath, "/tmp/logo.png")
	if err != nil {
		t.Fatalf("buildFFmpegArgs: %v", err)
	}

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
	if !strings.Contains(filter, "drawtext=textfile='"+textPath+"'") {
		t.Errorf("filter_complex = %q, want the drawtext-from-file stage too", filter)
	}
	if !strings.Contains(filter, "[vout]") {
		t.Errorf("filter_complex = %q, want a labeled video output ([vout]) to -map explicitly", filter)
	}
	if slices.Contains(args, "-vf") {
		t.Errorf("with logo: must not also use -vf, got %v", args)
	}
	mustContainSeq(t, args, []string{"-map", "[vout]"})
	mustContainSeq(t, args, []string{"-map", "0:a?"})
}

// TestBuildFFmpegArgsNeverInlinesRawText is PDW-6's core regression guard: previously the
// owner-configured watermark text (untrusted) was formatted directly into the filtergraph
// string passed as an ffmpeg argument, which is exactly the injection vector PDW-6 fixes. Now
// buildFFmpegArgs never receives the raw text at all (only a file path), so no argument can
// ever contain it — this test proves that structurally, by construction, rather than by
// re-deriving ffmpeg's escaping rules.
func TestBuildFFmpegArgsNeverInlinesRawText(t *testing.T) {
	dangerous := "O'Brien's; rm -rf / #% :: [evil]"
	dir := t.TempDir()
	textPath := filepath.Join(dir, "watermark.txt")
	if err := os.WriteFile(textPath, []byte(dangerous), 0o600); err != nil {
		t.Fatal(err)
	}
	args, err := buildFFmpegArgs("/tmp/in.mp4", "/tmp/out.mp4", textPath, "/tmp/logo.png")
	if err != nil {
		t.Fatalf("buildFFmpegArgs: %v", err)
	}
	for _, a := range args {
		if strings.Contains(a, dangerous) {
			t.Fatalf("arg %q contains the raw watermark text; it must only ever appear inside the text file, never inline in an ffmpeg argument", a)
		}
	}
}

// TestQuoteFilterValueRejectsSingleQuote covers PDW-6: since ffmpeg's filtergraph quoting has
// no way to represent a literal single quote inside a single-quoted value, quoteFilterValue
// must refuse such a value outright instead of emitting a broken or (as the original bug did)
// exploitable filtergraph.
func TestQuoteFilterValueRejectsSingleQuote(t *testing.T) {
	if _, err := quoteFilterValue("/tmp/it's-a-trap"); err == nil {
		t.Fatal("quoteFilterValue accepted a value containing a single quote, want an error")
	}
	got, err := quoteFilterValue("/tmp/clip-wm-text-123/watermark.txt")
	if err != nil {
		t.Fatalf("quoteFilterValue: %v", err)
	}
	if want := "'/tmp/clip-wm-text-123/watermark.txt'"; got != want {
		t.Errorf("quoteFilterValue = %q, want %q", got, want)
	}
}

// TestBuildFFmpegArgsRejectsUnquotableTextPath covers the error path end to end: a text file
// path containing a single quote (should never happen in practice — see writeTempText — but
// defensively checked) must fail buildFFmpegArgs rather than build a broken filtergraph.
func TestBuildFFmpegArgsRejectsUnquotableTextPath(t *testing.T) {
	if _, err := buildFFmpegArgs("/tmp/in.mp4", "/tmp/out.mp4", "/tmp/it's-a-trap/watermark.txt", ""); err == nil {
		t.Fatal("buildFFmpegArgs accepted a text file path containing a single quote, want an error")
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
