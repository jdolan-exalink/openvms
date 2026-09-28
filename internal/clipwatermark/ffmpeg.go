package clipwatermark

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// buildFFmpegArgs returns the ffmpeg argument list that burns the watermark text stored at
// textFilePath into the video at inputPath, compositing logoPath in the top-right corner when
// it is non-empty, writing outputPath. Output is always H.264 video (compatibility: Frigate
// clips may be HEVC) with an AAC audio track, keeping CPU/RAM modest per the task's own
// constraint: -preset veryfast and a fixed, low thread count rather than "as many cores as
// available".
//
// PDW-6 fix: the watermark text (owner-configured branding, so untrusted) is NEVER embedded
// inline in the filtergraph string anymore. The previous implementation built
// `drawtext=text='<escaped text>'` and "escaped" a literal single quote in text as `\'`, but
// ffmpeg's filtergraph quoting does not work that way: content between single quotes is copied
// literally with NO backslash-escaping applied inside it, and there is no way to represent a
// literal single quote inside a single-quoted value at all (ffmpeg's own docs: close the
// quote, insert an escaped quote *outside* it, reopen the quote). That meant an owner name
// containing a `'` closed the value early and injected arbitrary filtergraph syntax into the
// shared worker process, and — independently — the colon-escaping applied to the timestamp
// rendered as a literal backslash (`13\:05\:30`) in every clip, since colons inside single
// quotes are not an escape sequence either.
//
// The fix sidesteps filtergraph quoting entirely for the text: the caller (worker.go) writes
// it verbatim to a private temp file and passes that file's path here; drawtext reads the
// file's content as-is via textfile=, with expansion=none so ffmpeg never interprets %{...}
// sequences in that content either (drawtext's default "normal" expansion mode would still
// treat % specially even when reading from a file). Only the file path itself — server
// generated, not attacker-controlled — is still embedded inline in the filtergraph string,
// quoted via quoteFilterValue, which refuses (rather than silently mis-escapes) a path
// containing a single quote.
//
// args never includes a shell: exec.CommandContext(ctx, ffmpegPath, args...) runs ffmpeg
// directly, so nothing here needs shell quoting — only ffmpeg's own filtergraph syntax matters.
func buildFFmpegArgs(inputPath, outputPath, textFilePath, logoPath string) ([]string, error) {
	args := []string{"-y", "-loglevel", "error", "-i", inputPath}

	dt, err := drawtextFilter(textFilePath)
	if err != nil {
		return nil, err
	}

	if logoPath != "" {
		args = append(args, "-i", logoPath)
		args = append(args, "-filter_complex",
			"[1:v]scale=120:-1[logo];[0:v][logo]overlay=W-w-10:10:shortest=1,"+dt+"[vout]")
		// PDW-6 fix: -filter_complex drops the implicit "map everything from input 0" default
		// that a plain -vf keeps, so without an explicit -map ffmpeg selects only the filtered
		// video stream ([vout]) and silently discards the source's audio track. Map the
		// labeled video output plus the source's audio stream if it has one; "?" makes the
		// audio map optional so a clip with no audio track still succeeds.
		args = append(args, "-map", "[vout]", "-map", "0:a?")
	} else {
		args = append(args, "-vf", dt)
	}

	return append(args,
		"-c:v", "libx264", "-preset", "veryfast", "-threads", "2",
		"-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart",
		"-t", maxClipSeconds,
		outputPath,
	), nil
}

// maxClipSeconds caps the encoded output duration as a defensive limit against an
// unexpectedly long source clip driving up CPU time on a modest host; Frigate clips are
// normally well under this (PRD/export limits elsewhere in this codebase cap at 2 hours,
// far above a single tracked-object clip).
const maxClipSeconds = "120"

func drawtextFilter(textFilePath string) (string, error) {
	q, err := quoteFilterValue(textFilePath)
	if err != nil {
		return "", fmt.Errorf("clipwatermark: watermark text file path: %w", err)
	}
	return "drawtext=textfile=" + q +
		":expansion=none:fontcolor=white:fontsize=24:box=1:boxcolor=black@0.6:boxborderw=8:x=10:y=h-th-10", nil
}

// quoteFilterValue wraps s in ffmpeg filtergraph single quotes ('...'). Content inside single
// quotes is copied by ffmpeg literally — no backslash escaping applies there at all — and a
// literal single quote cannot be represented inside a single-quoted value by any means (see
// buildFFmpegArgs's doc comment for the two bugs that resulted from getting this wrong). s here
// is always a server-generated temp file path (see worker.go's writeTempText), never owner-
// configured text, but this still refuses rather than silently mis-quoting if it ever did
// contain a single quote.
func quoteFilterValue(s string) (string, error) {
	if strings.ContainsRune(s, '\'') {
		return "", fmt.Errorf("value contains a single quote, which cannot be safely quoted for ffmpeg's filtergraph: %q", s)
	}
	return "'" + s + "'", nil
}

func outputKey(jobID uuid.UUID) string {
	return "clip-jobs/" + jobID.String() + "/watermarked.mp4"
}

func jobLogoKey(jobID uuid.UUID) string {
	return "clip-jobs/" + jobID.String() + "/logo"
}
