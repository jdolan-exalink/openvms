//go:build integration

package clipwatermark

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFFmpegEndToEnd covers PDW-4/PDW-6's own explicit instruction: real ffmpeg end-to-end
// tests, skipped (not failed) when ffmpeg is not on PATH — this dev host does not have it
// installed (confirmed via `which ffmpeg` before writing PDW-4) and the task instructions
// forbid installing packages on the host, so these are expected to skip here; run against the
// worker image's real ffmpeg via `docker exec` (see the PDW-6 ODD doc for the exact commands),
// they prove the real binary accepts buildFFmpegArgs's output, including the PDW-6 textfile
// fix (owner text can never break the filtergraph) and the audio-mapping fix.
func TestFFmpegEndToEnd(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH; skipping (see comment above)")
	}

	t.Run("plain watermark burns in and produces valid output", func(t *testing.T) {
		dir := t.TempDir()
		inputPath := genTestSource(t, ffmpegPath, dir, false, false)
		outputPath := filepath.Join(dir, "out.mp4")

		textPath := genText(t, "2026-09-28 13:05:30 -03:00 · Municipalidad de Helvecia")
		args, err := buildFFmpegArgs(inputPath, outputPath, textPath, "")
		if err != nil {
			t.Fatalf("buildFFmpegArgs: %v", err)
		}
		runFFmpeg(t, ffmpegPath, args)
		requireNonEmpty(t, outputPath)
	})

	// PDW-6 finding: owner names containing ffmpeg filtergraph metacharacters must render
	// without altering the filter graph — previously a `'` in the owner name broke out of the
	// inline text='...' value and could inject arbitrary filtergraph syntax; the textfile fix
	// removes the owner text from the filtergraph string entirely, so this must succeed for
	// every one of these characters instead of corrupting the graph or failing to parse.
	dangerousNames := []string{
		`O'Brien's Towing`,
		`Arrival: 13:05`,
		`100% Secure`,
		`Comma, separated`,
		`Semicolon; here`,
		`[bracketed] name`,
	}
	for _, name := range dangerousNames {
		t.Run("owner name with special characters: "+name, func(t *testing.T) {
			dir := t.TempDir()
			inputPath := genTestSource(t, ffmpegPath, dir, false, false)
			outputPath := filepath.Join(dir, "out.mp4")

			textPath := genText(t, "2026-09-28 13:05:30 -03:00 · "+name)
			args, err := buildFFmpegArgs(inputPath, outputPath, textPath, "")
			if err != nil {
				t.Fatalf("buildFFmpegArgs: %v", err)
			}
			runFFmpeg(t, ffmpegPath, args)
			requireNonEmpty(t, outputPath)
		})
	}

	t.Run("source with audio and a logo keeps both an audio and a video stream", func(t *testing.T) {
		dir := t.TempDir()
		inputPath := genTestSource(t, ffmpegPath, dir, true, false)
		logoPath := genLogo(t, dir)
		outputPath := filepath.Join(dir, "out.mp4")

		textPath := genText(t, "2026-09-28 13:05:30 -03:00 · Municipalidad de Helvecia")
		args, err := buildFFmpegArgs(inputPath, outputPath, textPath, logoPath)
		if err != nil {
			t.Fatalf("buildFFmpegArgs: %v", err)
		}
		runFFmpeg(t, ffmpegPath, args)
		requireNonEmpty(t, outputPath)

		streams := probeStreamTypes(t, ffmpegPath, outputPath)
		if !streams["video"] {
			t.Errorf("output streams = %v, want a video stream", streams)
		}
		if !streams["audio"] {
			t.Errorf("output streams = %v, want an audio stream (PDW-6 fix: -filter_complex must -map 0:a?)", streams)
		}
	})

	t.Run("HEVC source", func(t *testing.T) {
		dir := t.TempDir()
		inputPath, ok := genHEVCSource(t, ffmpegPath, dir)
		if !ok {
			t.Skip("this ffmpeg build has no libx265 encoder; skipping HEVC source subcase")
		}
		outputPath := filepath.Join(dir, "out.mp4")

		textPath := genText(t, "2026-09-28 13:05:30 -03:00 · Municipalidad de Helvecia")
		args, err := buildFFmpegArgs(inputPath, outputPath, textPath, "")
		if err != nil {
			t.Fatalf("buildFFmpegArgs: %v", err)
		}
		runFFmpeg(t, ffmpegPath, args)
		requireNonEmpty(t, outputPath)

		streams := probeStreamTypes(t, ffmpegPath, outputPath)
		if !streams["video"] {
			t.Errorf("output streams = %v, want a video stream (re-encoded from HEVC to H.264)", streams)
		}
	})
}

// genTestSource generates a tiny synthetic source clip with ffmpeg's own lavfi test source, so
// tests need no fixture file and no network access. withAudio adds a sine-wave audio track.
func genTestSource(t *testing.T, ffmpegPath, dir string, withAudio, hevc bool) string {
	t.Helper()
	path := filepath.Join(dir, "in.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"-y", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=64x64:rate=5:duration=1"}
	if withAudio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "aac")
	}
	codec := "libx264"
	if hevc {
		codec = "libx265"
	}
	args = append(args, "-c:v", codec, "-preset", "veryfast", path)
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("generate synthetic input clip: %v: %s", err, stderr.String())
	}
	return path
}

// genHEVCSource generates a source clip encoded with libx265; ok is false (and the test should
// skip, not fail) when this ffmpeg build has no libx265 encoder available.
func genHEVCSource(t *testing.T, ffmpegPath, dir string) (string, bool) {
	t.Helper()
	path := filepath.Join(dir, "in-hevc.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpegPath, "-y", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=64x64:rate=5:duration=1",
		"-c:v", "libx265", "-preset", "veryfast", "-tag:v", "hvc1", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "Unknown encoder") || strings.Contains(stderr.String(), "libx265") {
			return "", false
		}
		t.Fatalf("generate HEVC input clip: %v: %s", err, stderr.String())
	}
	return path, true
}

// genLogo writes a tiny valid PNG logo file, using the Go standard library rather than ffmpeg
// so this stays independent of any lavfi source-generation quirk.
func genLogo(t *testing.T, dir string) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	path := filepath.Join(dir, "logo.png")
	f, err := os.Create(path) //nolint:gosec // path is this test's own t.TempDir()-derived path
	if err != nil {
		t.Fatalf("create logo file: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode logo png: %v", err)
	}
	return path
}

// genText writes text to a temp file via the package's own writeTempText, the same helper
// worker.go uses, so this test exercises the real production path end to end.
func genText(t *testing.T, text string) string {
	t.Helper()
	path, cleanup, err := writeTempText(text)
	if err != nil {
		t.Fatalf("writeTempText: %v", err)
	}
	t.Cleanup(cleanup)
	return path
}

func runFFmpeg(t *testing.T, ffmpegPath string, args []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("ffmpeg with buildFFmpegArgs output: %v: %s\nargs: %v", err, stderr.String(), args)
	}
}

func requireNonEmpty(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() == 0 {
		t.Error("watermarked output is empty")
	}
}

// probeStreamTypes reports which stream types (e.g. "video", "audio") path contains, using
// ffprobe when it is on PATH (the worker image only ships ffmpeg, so this also falls back to
// parsing `ffmpeg -i`'s own stderr stream listing, which is always available wherever ffmpeg
// is).
func probeStreamTypes(t *testing.T, ffmpegPath, path string) map[string]bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	types := map[string]bool{}
	if ffprobePath, err := exec.LookPath("ffprobe"); err == nil {
		out, err := exec.CommandContext(ctx, ffprobePath, "-v", "error",
			"-show_entries", "stream=codec_type", "-of", "csv=p=0", path).Output()
		if err != nil {
			t.Fatalf("ffprobe: %v", err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				types[line] = true
			}
		}
		return types
	}

	// No ffprobe: `ffmpeg -i <file>` with no output always exits non-zero, but it still prints
	// the input's stream listing to stderr first ("Stream #0:0: Video: ...", "Stream #0:1:
	// Audio: ...") — parse that instead.
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, ffmpegPath, "-i", path)
	cmd.Stderr = &stderr
	_ = cmd.Run()
	out := stderr.String()
	if strings.Contains(out, ": Video:") {
		types["video"] = true
	}
	if strings.Contains(out, ": Audio:") {
		types["audio"] = true
	}
	return types
}
