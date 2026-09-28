//go:build integration

// package clipwatermark_test (not clipwatermark): a white-box test here would create an
// import cycle, since internal/testutil/demofix -> internal/bootstrap -> internal/api ->
// internal/clipwatermark (api.Handlers.ClipWatermark). The external test package avoids it
// — a standard Go pattern for exactly this situation — using only clipwatermark's exported
// surface (Service, Worker.RunFFmpeg, ErrNotReady, Blobs).
package clipwatermark_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/branding"
	"github.com/jdolan-exalink/openvms/internal/clipwatermark"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// memBlobs is a minimal in-memory object store fake (Put/Get), enough to prove the worker's
// upload/download roundtrip without a real S3-compatible backend.
type memBlobs struct {
	mu   sync.Mutex
	data map[string][]byte
	ct   map[string]string
}

func (b *memBlobs) Put(_ context.Context, key string, body []byte, contentType string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.data == nil {
		b.data, b.ct = map[string][]byte{}, map[string]string{}
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	b.data[key], b.ct[key] = cp, contentType
	return nil
}

func (b *memBlobs) Get(_ context.Context, key string) ([]byte, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.data[key]
	if !ok {
		return nil, "", errors.New("object not found")
	}
	return d, b.ct[key], nil
}

type noopBlobs struct{}

func (noopBlobs) Put(context.Context, string, []byte, string) error   { return nil }
func (noopBlobs) Get(context.Context, string) ([]byte, string, error) { return nil, "", nil }

// fakeRunFFmpeg simulates ffmpeg by copying the input file to the output path, so the job
// state machine (queued -> running -> done, or -> failed) can be exercised without the real
// ffmpeg binary, which this dev host does not have (see the ODD doc for the skip-guarded
// real-ffmpeg end-to-end test in ffmpeg_e2e_test.go).
func fakeRunFFmpeg(t *testing.T, fail bool) func(ctx context.Context, ffmpegPath string, args []string) error {
	return func(_ context.Context, _ string, args []string) error {
		if fail {
			return errors.New("simulated ffmpeg failure")
		}
		idx := -1
		for i, a := range args {
			if a == "-i" {
				idx = i
				break
			}
		}
		if idx == -1 || idx+1 >= len(args) {
			t.Fatalf("no -i in ffmpeg args: %v", args)
		}
		input := args[idx+1]
		output := args[len(args)-1]
		data, err := os.ReadFile(input)
		if err != nil {
			return err
		}
		return os.WriteFile(output, data, 0o600)
	}
}

func setupService(env *demofix.Env, blobs clipwatermark.Blobs) (*clipwatermark.Service, *inventory.Adapters) {
	adapters := inventory.NewAdapters(env.Svc)
	mediaSvc := &media.Service{Store: env.Store, Adapters: adapters, Log: pgtest.Discard()}
	brandingSvc := &branding.Service{Store: env.Store, Blobs: &memBlobs{}, Log: pgtest.Discard()}
	return &clipwatermark.Service{Store: env.Store, Media: mediaSvc, Branding: brandingSvc, Blobs: blobs, Log: pgtest.Discard()}, adapters
}

func firstPlateRead(t *testing.T, env *demofix.Env, adapters *inventory.Adapters, cameraID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	sy := &events.Syncer{Store: env.Store, Adapters: adapters, Blobs: noopBlobs{}, Log: pgtest.Discard(), Interval: time.Second, Backfill: 24 * time.Hour, Concurrency: 2}
	sy.SyncAll(ctx)
	evSvc := &events.Service{Store: env.Store, Blobs: noopBlobs{}, Adapters: adapters, Log: pgtest.Discard()}
	page, err := evSvc.ListPlates(ctx, env.Admin, events.PlateFilter{CameraIDs: []uuid.UUID{cameraID}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) == 0 {
		t.Fatalf("camera %s has no synced plate reads", cameraID)
	}
	return page.Items[0].ID
}

// TestClipWatermarkJobLifecycle covers PDW-4's job state machine end to end (minus the real
// ffmpeg invocation, faked here — see the ODD doc): CreateJob (authorized) inserts a queued
// row; the worker claims and processes it to done, uploading watermarked bytes retrievable
// through Download; a second job that fails ffmpeg ends up failed with an error message,
// and Download on it answers ErrNotReady.
func TestClipWatermarkJobLifecycle(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	blobs := &memBlobs{}
	svc, adapters := setupService(env, blobs)

	camA := env.Cameras["frigate-h01/acceso_norte"]
	readID := firstPlateRead(t, env, adapters, camA.ID)

	job, err := svc.CreateJob(ctx, env.Admin, readID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if job.Status != "queued" {
		t.Fatalf("new job status = %q, want queued", job.Status)
	}

	w := &clipwatermark.Worker{Store: env.Store, Adapters: adapters, Blobs: blobs, Log: pgtest.Discard(), RunFFmpeg: fakeRunFFmpeg(t, false)}
	w.Once(ctx)

	done, err := svc.GetJob(ctx, env.Admin, readID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "done" {
		t.Fatalf("job status after processing = %q (error=%q), want done", done.Status, done.Error)
	}

	data, err := svc.Download(ctx, env.Admin, readID, job.ID)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if len(data) == 0 {
		t.Error("downloaded clip is empty")
	}

	t.Run("a failing ffmpeg run marks the job failed", func(t *testing.T) {
		job2, err := svc.CreateJob(ctx, env.Admin, readID)
		if err != nil {
			t.Fatal(err)
		}
		w2 := &clipwatermark.Worker{Store: env.Store, Adapters: adapters, Blobs: blobs, Log: pgtest.Discard(), RunFFmpeg: fakeRunFFmpeg(t, true)}
		w2.Once(ctx)

		failed, err := svc.GetJob(ctx, env.Admin, readID, job2.ID)
		if err != nil {
			t.Fatal(err)
		}
		if failed.Status != "failed" {
			t.Fatalf("job status = %q, want failed", failed.Status)
		}
		if failed.Error == "" {
			t.Error("failed job has no error message")
		}

		if _, err := svc.Download(ctx, env.Admin, readID, job2.ID); !errors.Is(err, clipwatermark.ErrNotReady) {
			t.Errorf("Download on a failed job: err = %v, want ErrNotReady", err)
		}
	})
}

// TestClipWatermarkJobAuthorization covers PDW-4's permission rules: creating a job needs
// exports.create (in addition to the clip-view permissions) and downloading needs
// exports.download.
func TestClipWatermarkJobAuthorization(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	blobs := &memBlobs{}
	svc, adapters := setupService(env, blobs)

	camA := env.Cameras["frigate-h01/acceso_norte"]
	readID := firstPlateRead(t, env, adapters, camA.ID)

	operator := env.Actor(t, "operador")
	if _, err := svc.CreateJob(ctx, operator, readID); err == nil {
		t.Fatal("operador without exports.create was able to CreateJob")
	}

	// operatorPerms (internal/bootstrap/demo.go) already gives "operador" recordings.view on
	// camA but not lpr.view or exports.create; both are needed for CreateJob.
	for _, p := range []authz.Permission{authz.LPRView, authz.ExportsCreate} {
		if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: operator.UserID, Permission: p,
			Effect: authz.Allow, ScopeType: authz.ScopeCamera, ScopeID: &camA.ID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	job, err := svc.CreateJob(ctx, operator, readID)
	if err != nil {
		t.Fatalf("CreateJob after granting exports.create: %v", err)
	}

	w := &clipwatermark.Worker{Store: env.Store, Adapters: adapters, Blobs: blobs, Log: pgtest.Discard(), RunFFmpeg: fakeRunFFmpeg(t, false)}
	w.Once(ctx)

	if _, err := svc.Download(ctx, operator, readID, job.ID); err == nil {
		t.Fatal("operador without exports.download was able to Download")
	}
}

// TestClipWatermarkJobRecoversFromPanic covers PDW-6: a panic anywhere inside a job's
// processing (simulated here via a RunFFmpeg fake that panics, standing in for e.g. a bug in
// a future ffmpeg-args builder or a malformed clip triggering an out-of-bounds access) must
// mark that one job failed instead of crashing the whole worker process. Without
// runJobRecovered's recover(), w.Once(ctx) below would itself panic and crash this entire test
// binary rather than fail this one assertion — a strong, hard-to-miss RED signal that was
// observed before recover() was added (see the PDW-6 ODD doc).
func TestClipWatermarkJobRecoversFromPanic(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	blobs := &memBlobs{}
	svc, adapters := setupService(env, blobs)

	camA := env.Cameras["frigate-h01/acceso_norte"]
	readID := firstPlateRead(t, env, adapters, camA.ID)

	job, err := svc.CreateJob(ctx, env.Admin, readID)
	if err != nil {
		t.Fatal(err)
	}

	w := &clipwatermark.Worker{
		Store: env.Store, Adapters: adapters, Blobs: blobs, Log: pgtest.Discard(),
		RunFFmpeg: func(context.Context, string, []string) error {
			panic("simulated panic inside job processing")
		},
	}
	w.Once(ctx)

	failed, err := svc.GetJob(ctx, env.Admin, readID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" {
		t.Fatalf("job status after a panicking run = %q, want failed (recover() should have caught the panic)", failed.Status)
	}
	if !strings.Contains(failed.Error, "panic") {
		t.Errorf("failed job error = %q, want it to mention the panic", failed.Error)
	}
}

// TestSweepStuckClipWatermarkJobs covers PDW-6: a job whose worker process was killed after
// claiming it (status -> "running") but before it could finish — so recover() itself never
// ran, since the process is gone, not merely panicking — must eventually be failed by the
// periodic sweep rather than staying "running" forever and leaving the requester polling
// indefinitely.
func TestSweepStuckClipWatermarkJobs(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	blobs := &memBlobs{}
	svc, adapters := setupService(env, blobs)

	camA := env.Cameras["frigate-h01/acceso_norte"]
	readID := firstPlateRead(t, env, adapters, camA.ID)

	stuckJob, err := svc.CreateJob(ctx, env.Admin, readID)
	if err != nil {
		t.Fatal(err)
	}
	recentJob, err := svc.CreateJob(ctx, env.Admin, readID)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the worker having claimed both jobs (status -> running) at different times:
	// stuckJob well past the 15-minute stuckTimeout, recentJob just now — only the former
	// should be swept.
	setRunningSince := func(id uuid.UUID, since time.Duration) {
		t.Helper()
		if err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE clip_watermark_jobs SET status = 'running', updated_at = $2 WHERE id = $1`, id, time.Now().Add(-since))
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	setRunningSince(stuckJob.ID, 20*time.Minute)
	setRunningSince(recentJob.ID, 10*time.Second)

	w := &clipwatermark.Worker{Store: env.Store, Adapters: adapters, Blobs: blobs, Log: pgtest.Discard()}
	w.SweepStuck(ctx)

	stuck, err := svc.GetJob(ctx, env.Admin, readID, stuckJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stuck.Status != "failed" {
		t.Errorf("swept job status = %q, want failed", stuck.Status)
	}
	if stuck.Error == "" {
		t.Error("swept job has no error message")
	}

	recent, err := svc.GetJob(ctx, env.Admin, readID, recentJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recent.Status != "running" {
		t.Errorf("recently-claimed job status = %q, want it left alone as running", recent.Status)
	}
}
