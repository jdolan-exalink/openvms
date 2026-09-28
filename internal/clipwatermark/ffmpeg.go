package clipwatermark

import (
	"strings"

	"github.com/google/uuid"
)

// buildFFmpegArgs returns the ffmpeg argument list that burns text (already
// watermark.Text-formatted) into the video at inputPath, compositing logoPath in the
// top-right corner when it is non-empty, writing outputPath. Output is always H.264 video
// (compatibility: Frigate clips may be HEVC) with a copied/AAC audio track, keeping CPU/RAM
// modest per the task's own constraint: -preset veryfast and a fixed, low thread count
// rather than "as many cores as available".
//
// args never includes a shell: exec.CommandContext(ctx, ffmpegPath, args...) runs ffmpeg
// directly, so nothing here needs shell quoting — only ffmpeg's own filtergraph syntax
// (escapeDrawtext) matters.
func buildFFmpegArgs(inputPath, outputPath, text, logoPath string) []string {
	args := []string{"-y", "-loglevel", "error", "-i", inputPath}

	dt := drawtextFilter(text)
	if logoPath != "" {
		args = append(args, "-i", logoPath)
		args = append(args, "-filter_complex",
			"[1:v]scale=120:-1[logo];[0:v][logo]overlay=W-w-10:10:shortest=1,"+dt)
	} else {
		args = append(args, "-vf", dt)
	}

	return append(args,
		"-c:v", "libx264", "-preset", "veryfast", "-threads", "2",
		"-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart",
		"-t", maxClipSeconds,
		outputPath,
	)
}

// maxClipSeconds caps the encoded output duration as a defensive limit against an
// unexpectedly long source clip driving up CPU time on a modest host; Frigate clips are
// normally well under this (PRD/export limits elsewhere in this codebase cap at 2 hours,
// far above a single tracked-object clip).
const maxClipSeconds = "120"

func drawtextFilter(text string) string {
	return "drawtext=text='" + escapeDrawtext(text) +
		"':fontcolor=white:fontsize=24:box=1:boxcolor=black@0.6:boxborderw=8:x=10:y=h-th-10"
}

// drawtextReplacer escapes ffmpeg drawtext filter metacharacters in order: backslash first
// (so escaping the other characters does not double-escape their own backslashes), then
// single quote (the text value itself is wrapped in single quotes), then colon (filter
// option separator) and percent (strftime-style expansion trigger under drawtext's default
// "normal" expansion mode). This mirrors ffmpeg's own documented drawtext escaping rules.
var drawtextReplacer = strings.NewReplacer(
	`\`, `\\`,
	`'`, `\'`,
	`:`, `\:`,
	`%`, `\%`,
)

func escapeDrawtext(s string) string {
	return drawtextReplacer.Replace(s)
}

func outputKey(jobID uuid.UUID) string {
	return "clip-jobs/" + jobID.String() + "/watermarked.mp4"
}

func jobLogoKey(jobID uuid.UUID) string {
	return "clip-jobs/" + jobID.String() + "/logo"
}
