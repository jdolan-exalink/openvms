//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/branding"
	"github.com/jdolan-exalink/openvms/internal/clipwatermark"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// TestClipWatermarkJobAPI covers PDW-4's HTTP surface: create (202, queued), poll (200),
// download before the worker has run (409 not_ready), and — after the worker (with a faked
// ffmpeg run, see internal/clipwatermark's own tests for why) processes it — download
// succeeds (200, video/mp4) and is audited as CLIP_DOWNLOADED.
func TestClipWatermarkJobAPI(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	adapters := inventory.NewAdapters(env.Svc)
	syncer := &events.Syncer{
		Store: env.Store, Adapters: adapters, Blobs: noopBlobs{}, Log: pgtest.Discard(),
		Interval: time.Second, Backfill: 24 * time.Hour, Concurrency: 2,
	}
	syncer.SyncAll(ctx)
	evSvc := &events.Service{Store: env.Store, Blobs: noopBlobs{}, Adapters: adapters, Log: pgtest.Discard()}
	mediaSvc := &media.Service{Store: env.Store, Adapters: adapters, Log: pgtest.Discard()}
	blobs := &memBlobs{}
	brandingSvc := &branding.Service{Store: env.Store, Blobs: blobs, Log: pgtest.Discard()}
	cwSvc := &clipwatermark.Service{Store: env.Store, Media: mediaSvc, Branding: brandingSvc, Blobs: blobs, Log: pgtest.Discard()}

	h := &api.Handlers{Inv: env.Svc, Events: evSvc, Media: mediaSvc, Branding: brandingSvc, ClipWatermark: cwSvc, Log: pgtest.Discard()}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{
		Queries: db.New(env.Pool),
		Media:   (&media.Gateway{Svc: mediaSvc, Actor: api.ActorFrom, Branding: brandingSvc}).Routes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()

	camA := env.Cameras["frigate-h01/acceso_norte"]
	page, err := evSvc.ListPlates(ctx, env.Admin, events.PlateFilter{CameraIDs: []uuid.UUID{camA.ID}, Limit: 1})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("seed plate read: items=%d err=%v", len(page.Items), err)
	}
	readID := page.Items[0].ID

	do := func(t *testing.T, method, path string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, method, ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+env.AdminToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if resp.Header.Get("Content-Type") == "application/json" {
			_ = json.NewDecoder(resp.Body).Decode(&out)
		}
		return resp.StatusCode, out
	}

	base := "/api/v1/lpr/reads/" + readID.String() + "/clip-watermark-jobs"

	code, created := do(t, "POST", base)
	if code != http.StatusAccepted {
		t.Fatalf("create job: %d %v", code, created)
	}
	jobID, _ := created["id"].(string)
	if jobID == "" || created["status"] != "queued" {
		t.Fatalf("create job response = %v", created)
	}

	code, status := do(t, "GET", base+"/"+jobID)
	if code != http.StatusOK || status["status"] != "queued" {
		t.Fatalf("get job: %d %v", code, status)
	}

	code, _ = do(t, "GET", base+"/"+jobID+"/download")
	if code != http.StatusConflict {
		t.Fatalf("download before ready: status = %d, want 409", code)
	}

	// Fakes ffmpeg by copying the input clip straight to the output path: this dev host has
	// no ffmpeg binary (confirmed with `which ffmpeg` before writing PDW-4; see
	// internal/clipwatermark's own skip-guarded end-to-end test for the real-ffmpeg proof),
	// so this HTTP-layer test only needs to prove the job reaches "done" and the file
	// downloads, not that ffmpeg itself succeeds — that argument construction and escaping
	// is unit-tested in internal/clipwatermark/ffmpeg_test.go.
	w := &clipwatermark.Worker{Store: env.Store, Adapters: adapters, Blobs: blobs, Log: pgtest.Discard(), RunFFmpeg: func(_ context.Context, _ string, args []string) error {
		input, output := args[4], args[len(args)-1]
		data, err := os.ReadFile(input)
		if err != nil {
			return err
		}
		return os.WriteFile(output, data, 0o600)
	}}
	w.Once(ctx)

	code, status = do(t, "GET", base+"/"+jobID)
	if code != http.StatusOK || status["status"] != "done" {
		t.Fatalf("get job after processing: %d %v", code, status)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+base+"/"+jobID+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+env.AdminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download: status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/mp4" {
		t.Errorf("Content-Type = %q, want video/mp4", ct)
	}

	var n int
	if err := env.Pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE action = 'CLIP_DOWNLOADED' AND target_type = 'clip_watermark_job' AND target_id = $1`,
		jobID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("CLIP_DOWNLOADED audit rows = %d, want 1", n)
	}
}
