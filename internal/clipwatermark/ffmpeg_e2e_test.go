//go:build integration

package clipwatermark

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestFFmpegEndToEnd covers PDW-4's own explicit instruction: an ffmpeg end-to-end test,
// skipped (not failed) when ffmpeg is not on PATH — this dev host does not have it
// installed (confirmed via `which ffmpeg` before writing this feature) and the task
// instructions forbid installing packages on the host, so this test is expected to skip
// here; it is written to actually prove the real binary accepts buildFFmpegArgs's output
// wherever ffmpeg is available (e.g. CI, or the built worker image, which does have it).
func TestFFmpegEndToEnd(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH; skipping (see comment above)")
	}

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "in.mp4")
	outputPath := filepath.Join(dir, "out.mp4")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Generate a tiny synthetic source clip with ffmpeg's own lavfi test source, so this
	// test needs no fixture file and no network access.
	gen := exec.CommandContext(ctx, ffmpegPath, "-y", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=64x64:rate=5:duration=1",
		"-c:v", "libx264", "-preset", "veryfast", inputPath)
	var stderr bytes.Buffer
	gen.Stderr = &stderr
	if err := gen.Run(); err != nil {
		t.Fatalf("generate synthetic input clip: %v: %s", err, stderr.String())
	}

	args := buildFFmpegArgs(inputPath, outputPath, "2026-09-28 13:05:30 UTC+00:00 · Municipalidad de Helvecia", "")
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	stderr.Reset()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("ffmpeg with buildFFmpegArgs output: %v: %s", err, stderr.String())
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() == 0 {
		t.Error("watermarked output is empty")
	}
}
