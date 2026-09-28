//go:build integration

package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// TestLPRReadClipEndpoint covers PDW-2: GET /media/v1/lpr/reads/{id}/clip.mp4 proxies the
// mock Frigate's tracked-object clip, needs recordings.view + lpr.view on the read's
// camera, another tenant's read 404s, and a Range request is honoured end to end (the
// browser <video> element depends on this to seek without downloading the whole clip).
func TestLPRReadClipEndpoint(t *testing.T) {
	env := demofix.Setup(t)
	adapters := inventory.NewAdapters(env.Svc)
	syncer := &events.Syncer{
		Store: env.Store, Adapters: adapters, Blobs: noopBlobs{}, Log: pgtest.Discard(),
		Interval: time.Second, Backfill: 24 * time.Hour, Concurrency: 2,
	}
	syncer.SyncAll(context.Background())
	evSvc := &events.Service{Store: env.Store, Blobs: noopBlobs{}, Adapters: adapters, Log: pgtest.Discard()}
	mediaSvc := &media.Service{Store: env.Store, Adapters: adapters, Log: pgtest.Discard()}

	h := &api.Handlers{Inv: env.Svc, Events: evSvc, Media: mediaSvc, Log: pgtest.Discard()}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{
		Queries: db.New(env.Pool),
		Media:   (&media.Gateway{Svc: mediaSvc, Actor: api.ActorFrom}).Routes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()

	get := func(t *testing.T, token, path, rangeHeader string) *http.Response {
		t.Helper()
		req, _ := http.NewRequestWithContext(context.Background(), "GET", ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	camA := env.Cameras["frigate-h01/acceso_norte"] // LPR-enabled
	ctx := context.Background()
	page, err := evSvc.ListPlates(ctx, env.Admin, events.PlateFilter{CameraIDs: []uuid.UUID{camA.ID}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) == 0 {
		t.Fatalf("camera %s has no synced plate reads", camA.RemoteName)
	}
	readID := page.Items[0].ID
	clipPath := "/media/v1/lpr/reads/" + readID.String() + "/clip.mp4"

	t.Run("actor with recordings.view and lpr.view gets the clip", func(t *testing.T) {
		resp := get(t, env.AdminToken, clipPath, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "video/mp4" {
			t.Errorf("Content-Type = %q, want video/mp4", ct)
		}
		if ar := resp.Header.Get("Accept-Ranges"); ar != "bytes" {
			t.Errorf("Accept-Ranges = %q, want bytes", ar)
		}
	})

	t.Run("a Range request gets 206 with the requested byte window", func(t *testing.T) {
		resp := get(t, env.AdminToken, clipPath, "bytes=0-99")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusPartialContent {
			t.Fatalf("status = %d, want 206", resp.StatusCode)
		}
		if cl := resp.Header.Get("Content-Length"); cl != "100" {
			t.Errorf("Content-Length = %q, want 100", cl)
		}
		if cr := resp.Header.Get("Content-Range"); cr == "" {
			t.Errorf("Content-Range header missing on a ranged response")
		}
	})

	t.Run("actor without recordings.view is forbidden", func(t *testing.T) {
		u, token, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "clip-lpr-only")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: u.UserID, Permission: authz.LPRView,
			Effect: authz.Allow, ScopeType: authz.ScopeCamera, ScopeID: &camA.ID,
		}); err != nil {
			t.Fatal(err)
		}
		resp := get(t, token, clipPath, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("another tenant's read id 404s", func(t *testing.T) {
		u, token, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "clip-cross-tenant")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []authz.Permission{authz.RecordingsView, authz.LPRView} {
			if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
				SubjectType: "user", SubjectID: u.UserID, Permission: p,
				Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &env.Demo.TenantID,
			}); err != nil {
				t.Fatal(err)
			}
		}
		tenantB, err := env.Svc.CreateTenant(ctx, env.Admin, "lpr-clip-cross-tenant", "Cross Tenant Clip")
		if err != nil {
			t.Fatal(err)
		}
		foreignID := seedForeignLPRRead(t, env, tenantB.ID)

		resp := get(t, token, "/media/v1/lpr/reads/"+foreignID.String()+"/clip.mp4", "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})
}
