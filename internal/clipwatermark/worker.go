package clipwatermark

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"time"

	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Worker processes queued clip watermark jobs one at a time (concurrency 1, per the task's
// own CPU/RAM budget for a modest host: -preset veryfast, a fixed low thread count in
// buildFFmpegArgs, and a per-job timeout here), fetching the source clip from the camera's
// Frigate, running ffmpeg, and uploading the result to the object store.
type Worker struct {
	Store    *store.Store
	Adapters *inventory.Adapters
	Blobs    Blobs
	Log      *slog.Logger

	// FFmpegPath defaults to "ffmpeg" (resolved via PATH).
	FFmpegPath string
	// Interval between poll cycles when no job is queued. Defaults to 3s.
	Interval time.Duration
	// Timeout bounds one job's ffmpeg run (and the clip fetch). Defaults to 5 minutes —
	// generous for a single tracked-object clip at veryfast, but bounded so a stuck job
	// cannot hold the single-worker slot forever.
	Timeout time.Duration

	// RunFFmpeg executes ffmpeg; overridable (exported so other packages' tests can set it
	// too, e.g. internal/api's HTTP-layer test) so the job state machine can be exercised
	// without a real ffmpeg binary (which this dev host does not have). nil uses the real
	// exec.CommandContext-based implementation.
	RunFFmpeg func(ctx context.Context, ffmpegPath string, args []string) error
}

func (w *Worker) ffmpegPath() string {
	if w.FFmpegPath != "" {
		return w.FFmpegPath
	}
	return "ffmpeg"
}

func (w *Worker) interval() time.Duration {
	if w.Interval > 0 {
		return w.Interval
	}
	return 3 * time.Second
}

func (w *Worker) timeout() time.Duration {
	if w.Timeout > 0 {
		return w.Timeout
	}
	return 5 * time.Minute
}

func (w *Worker) run(ctx context.Context, ffmpegPath string, args []string) error {
	if w.RunFFmpeg != nil {
		return w.RunFFmpeg(ctx, ffmpegPath, args)
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, args...) //nolint:gosec // ffmpegPath is fixed config ("ffmpeg" on PATH); args come from buildFFmpegArgs, not user input
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

// Run polls forever until ctx is cancelled, processing at most one job per Once call before
// checking again — a queue with several jobs drains one at a time on this same goroutine.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(w.interval())
	defer t.Stop()
	for {
		w.Once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Once claims and processes every currently queued job, one at a time, then returns.
func (w *Worker) Once(ctx context.Context) {
	for {
		job, ok, err := w.claim(ctx)
		if err != nil {
			w.Log.WarnContext(ctx, "claim clip watermark job", "error", err)
			return
		}
		if !ok {
			return
		}
		w.process(ctx, job)
	}
}

func (w *Worker) claim(ctx context.Context) (db.ClipWatermarkJob, bool, error) {
	var job db.ClipWatermarkJob
	found := false
	err := w.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		j, err := q.ClaimNextClipWatermarkJob(ctx)
		if err != nil {
			if errors.Is(store.Classify(err), store.ErrNotFound) {
				return nil
			}
			return err
		}
		job, found = j, true
		return nil
	})
	return job, found, err
}

func (w *Worker) process(ctx context.Context, job db.ClipWatermarkJob) {
	cctx, cancel := context.WithTimeout(ctx, w.timeout())
	defer cancel()
	if err := w.runJob(cctx, job); err != nil {
		w.Log.WarnContext(ctx, "clip watermark job failed", "job", job.ID, "error", err)
		msg := err.Error()
		if len(msg) > 2000 {
			msg = msg[:2000]
		}
		if markErr := w.Store.Tx(context.WithoutCancel(ctx), store.AllTenants, func(q *db.Queries) error {
			return q.MarkClipWatermarkJobFailed(ctx, db.MarkClipWatermarkJobFailedParams{ID: job.ID, Error: msg})
		}); markErr != nil {
			w.Log.ErrorContext(ctx, "mark clip watermark job failed", "job", job.ID, "error", markErr)
		}
	}
}

// runJob does the actual work: fetch the source clip, optionally the logo, run ffmpeg,
// upload the result, and mark the job done. Every intermediate file is a temp file cleaned
// up before returning; nothing is held in memory beyond one clip/logo/output at a time.
func (w *Worker) runJob(ctx context.Context, job db.ClipWatermarkJob) error {
	srv, err := w.server(ctx, job)
	if err != nil {
		return fmt.Errorf("load server: %w", err)
	}
	ad, err := w.Adapters.Get(ctx, srv)
	if err != nil {
		return fmt.Errorf("connect to frigate: %w", err)
	}
	resp, err := ad.Media().Open(ctx, "/api/events/"+url.PathEscape(job.RemoteEventID)+"/clip.mp4", nil, nil)
	if err != nil {
		return fmt.Errorf("fetch clip: %w", err)
	}
	defer resp.Body.Close()

	inputPath, cleanupInput, err := writeTemp("clip-in-*.mp4", resp.Body)
	if err != nil {
		return fmt.Errorf("stage input clip: %w", err)
	}
	defer cleanupInput()

	var logoPath string
	if job.LogoKey != "" {
		data, _, err := w.Blobs.Get(ctx, job.LogoKey)
		if err != nil {
			w.Log.WarnContext(ctx, "load job logo, continuing without it", "job", job.ID, "error", err)
		} else {
			p, cleanupLogo, err := writeTemp("clip-logo-*", bytes.NewReader(data))
			if err != nil {
				w.Log.WarnContext(ctx, "stage job logo, continuing without it", "job", job.ID, "error", err)
			} else {
				logoPath = p
				defer cleanupLogo()
			}
		}
	}

	outputPath, cleanupOutput, err := writeTemp("clip-out-*.mp4", nil)
	if err != nil {
		return fmt.Errorf("stage output path: %w", err)
	}
	defer cleanupOutput()

	args := buildFFmpegArgs(inputPath, outputPath, job.WatermarkText, logoPath)
	if err := w.run(ctx, w.ffmpegPath(), args); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}

	out, err := os.ReadFile(outputPath) //nolint:gosec // outputPath is our own os.CreateTemp result, not user input
	if err != nil {
		return fmt.Errorf("read ffmpeg output: %w", err)
	}
	key := outputKey(job.ID)
	if err := w.Blobs.Put(ctx, key, out, "video/mp4"); err != nil {
		return fmt.Errorf("upload watermarked clip: %w", err)
	}
	return w.Store.Tx(context.WithoutCancel(ctx), store.AllTenants, func(q *db.Queries) error {
		return q.MarkClipWatermarkJobDone(ctx, db.MarkClipWatermarkJobDoneParams{ID: job.ID, OutputKey: key})
	})
}

func (w *Worker) server(ctx context.Context, job db.ClipWatermarkJob) (db.FrigateServer, error) {
	var srv db.FrigateServer
	err := w.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		srv, err = q.GetServerRow(ctx, job.ServerID)
		return err
	})
	return srv, err
}

// writeTemp creates a temp file matching pattern, copying r into it if r is not nil
// (nil reserves an empty path for ffmpeg to write into), and returns its path and a cleanup
// func that removes it.
func writeTemp(pattern string, r io.Reader) (string, func(), error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", func() {}, err
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if r != nil {
		if _, err := io.Copy(f, r); err != nil {
			_ = f.Close()
			cleanup()
			return "", func() {}, err
		}
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}
