//go:build integration

package api_test

import (
	"bytes"
	"context"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/branding"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// TestLPRReadSnapshotDownload covers PDW-3: GET .../snapshot.jpg?download=1 needs
// snapshots.download in addition to snapshots.view+lpr.view, returns a watermarked
// attachment, and is audited as SNAPSHOT_DOWNLOADED against the lpr_read.
func TestLPRReadSnapshotDownload(t *testing.T) {
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

	h := &api.Handlers{Inv: env.Svc, Events: evSvc, Media: mediaSvc, Branding: brandingSvc, Log: pgtest.Discard()}
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
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) == 0 {
		t.Fatalf("camera %s has no synced plate reads", camA.RemoteName)
	}
	readID := page.Items[0].ID
	downloadPath := "/media/v1/lpr/reads/" + readID.String() + "/snapshot.jpg?download=1"

	// Configure owner branding so the watermark text is deterministic to assert against
	// indirectly (the JPEG bytes differ from a plain view).
	if _, err := brandingSvc.Update(ctx, env.Admin, env.Demo.TenantID, branding.Input{OwnerName: ptr("Municipalidad de Helvecia")}); err != nil {
		t.Fatal(err)
	}

	get := func(t *testing.T, token string) *http.Response {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+downloadPath, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	t.Run("actor with snapshots.download gets a watermarked attachment", func(t *testing.T) {
		plainResp := get2(t, ts, readID.String(), env.AdminToken) // plain view, for comparison
		defer plainResp.Body.Close()
		plainBody, _ := io.ReadAll(plainResp.Body)

		resp := get(t, env.AdminToken)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if cd := resp.Header.Get("Content-Disposition"); cd == "" {
			t.Errorf("missing Content-Disposition header")
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		img, err := jpeg.Decode(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("downloaded body is not a valid JPEG: %v", err)
		}
		plainImg, err := jpeg.Decode(bytes.NewReader(plainBody))
		if err != nil {
			t.Fatalf("plain view body is not a valid JPEG: %v", err)
		}
		if img.Bounds() != plainImg.Bounds() {
			t.Errorf("watermarked dims = %v, want %v (same as plain view)", img.Bounds(), plainImg.Bounds())
		}
		if len(body) == len(plainBody) {
			t.Errorf("watermarked download is byte-identical to the plain view: watermark was not burned in")
		}

		var n int
		if err := env.Pool.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE action = 'SNAPSHOT_DOWNLOADED' AND target_type = 'lpr_read' AND target_id = $1`,
			readID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("SNAPSHOT_DOWNLOADED audit rows = %d, want 1", n)
		}
	})

	t.Run("actor with snapshots.view but not snapshots.download is forbidden", func(t *testing.T) {
		u, token, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "view-not-download")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []authz.Permission{authz.SnapshotsView, authz.LPRView} {
			if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
				SubjectType: "user", SubjectID: u.UserID, Permission: p,
				Effect: authz.Allow, ScopeType: authz.ScopeCamera, ScopeID: &camA.ID,
			}); err != nil {
				t.Fatal(err)
			}
		}
		resp := get(t, token)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
	})
}

func ptr[T any](v T) *T { return &v }

func get2(t *testing.T, ts *httptest.Server, readID, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), "GET", ts.URL+"/media/v1/lpr/reads/"+readID+"/snapshot.jpg", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
