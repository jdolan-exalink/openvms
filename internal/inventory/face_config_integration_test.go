//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func faceActor(t *testing.T, env *demofix.Env, perms ...authz.Permission) authz.Actor {
	t.Helper()
	tenant := env.Demo.TenantID
	actor, _, err := bootstrap.TenantUser(context.Background(), env.Store, tenant, "face-"+uuid.NewString()[:6])
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range perms {
		if _, err := env.Svc.CreateGrant(context.Background(), env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: actor.UserID, Permission: p, Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenant,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return actor
}

func fp[T any](v T) *T { return &v }

func TestFaceConfigListReadsRunningSettingsAndGatesEditing(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	tenant := env.Demo.TenantID
	viewer := faceActor(t, env, authz.FacesView, authz.ServersView)
	manager := faceActor(t, env, authz.FacesView, authz.FacesManage, authz.ServersView, authz.ServersConfig)

	list, err := env.Svc.ListFaceConfig(ctx, viewer, inventory.FaceConfigFilter{TenantID: &tenant})
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %d items, %v; want the two demo servers", len(list), err)
	}
	for _, it := range list {
		if it.CanEdit || it.Settings == nil || it.Error != "" || it.SiteName == "" || len(it.Cameras) == 0 {
			t.Fatalf("viewer item = %+v", it)
		}
	}
	list, _ = env.Svc.ListFaceConfig(ctx, manager, inventory.FaceConfigFilter{TenantID: &tenant})
	if !list[0].CanEdit || !list[1].CanEdit {
		t.Fatal("a manager with servers.config cannot edit")
	}
	// Without faces.view nothing is listed; without servers.view no server shows up.
	if _, err := env.Svc.ListFaceConfig(ctx, faceActor(t, env, authz.ServersView), inventory.FaceConfigFilter{TenantID: &tenant}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("no faces.view = %v, want forbidden", err)
	}
	if got, err := env.Svc.ListFaceConfig(ctx, faceActor(t, env, authz.FacesView), inventory.FaceConfigFilter{TenantID: &tenant}); err != nil || len(got) != 0 {
		t.Fatalf("no servers.view = %d items, %v", len(got), err)
	}
}

func TestPatchFaceConfigWritesThroughTheEditorWithRevisionsAndRefreshesCapabilities(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	tenant := env.Demo.TenantID
	manager := faceActor(t, env, authz.FacesView, authz.FacesManage, authz.ServersView, authz.ServersConfig)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	mock := env.Mocks["frigate-h01"].Server

	res, err := env.Svc.PatchFaceConfig(ctx, manager, tenant, cam.ServerID, inventory.FaceConfigPatch{
		Enabled: fp(true), ModelSize: fp("large"), RecognitionThreshold: fp(0.85), MinFaces: fp(2),
		Cameras: []inventory.FaceCameraPatch{{CameraID: cam.ID, Enabled: fp(false), MinArea: fp(900)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.RestartRequired || len(res.RevisionIDs) != 2 {
		t.Fatalf("result = %+v, want a server and a camera revision", res)
	}
	calls := mock.SetCalls()
	if len(calls) != 2 {
		t.Fatalf("frigate set calls = %d, want 2", len(calls))
	}
	global, _ := calls[0].ConfigData["face_recognition"].(map[string]any)
	if global["enabled"] != true || global["model_size"] != "large" || global["recognition_threshold"] != 0.85 || global["min_faces"] != float64(2) {
		t.Fatalf("global config_data = %+v", calls[0].ConfigData)
	}
	if calls[0].RequiresRestart == nil || *calls[0].RequiresRestart != 1 || calls[0].UpdateTopic != "" {
		t.Fatalf("global write is not flagged for restart: %+v", calls[0])
	}
	if calls[1].UpdateTopic != "" || calls[1].RequiresRestart == nil || *calls[1].RequiresRestart != 1 {
		t.Fatalf("camera face_recognition must not claim a live update: %+v", calls[1])
	}

	var kinds []string
	var audits int
	err = env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT kind FROM frigate_config_revisions WHERE server_id = $1 ORDER BY created_at`, cam.ServerID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				return err
			}
			kinds = append(kinds, k)
		}
		rows.Close()
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'FRIGATE_CONFIG_PATCHED' AND details->>'server_id' = $1`, cam.ServerID.String()).Scan(&audits)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 2 || kinds[0] != "server_patch" || kinds[1] != "camera_patch" {
		t.Fatalf("revision kinds = %v", kinds)
	}
	if audits != 2 {
		t.Fatalf("audit rows = %d, want 2", audits)
	}

	// The settings read back, and the capability flag followed so the registry sync can start.
	list, _ := env.Svc.ListFaceConfig(ctx, manager, inventory.FaceConfigFilter{TenantID: &tenant})
	var h01 inventory.FaceServerConfig
	for _, it := range list {
		if it.ServerID == cam.ServerID {
			h01 = it
		}
	}
	if h01.Settings == nil || !h01.Settings.Enabled || h01.Settings.ModelSize != "large" {
		t.Fatalf("read back = %+v", h01.Settings)
	}
	for _, c := range h01.Cameras {
		if c.CameraID == cam.ID && (c.Enabled || c.EnabledOverride == nil || *c.EnabledOverride || c.MinArea == nil || *c.MinArea != 900) {
			t.Fatalf("camera override = %+v", c)
		}
	}
	var flag bool
	if err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT (capabilities->>'face_recognition')::bool FROM frigate_servers WHERE id = $1`, cam.ServerID).Scan(&flag)
	}); err != nil || !flag {
		t.Fatalf("capabilities.face_recognition = %v, %v", flag, err)
	}
}

func TestPatchFaceConfigAuthorizationAndValidation(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	tenant := env.Demo.TenantID
	cam := env.Cameras["frigate-h01/acceso_norte"]
	other := env.Cameras["frigate-c01/muelle"]
	mock := env.Mocks["frigate-h01"].Server
	manager := faceActor(t, env, authz.FacesView, authz.FacesManage, authz.ServersView, authz.ServersConfig)

	// faces.manage and servers.config are both required.
	for name, actor := range map[string]authz.Actor{
		"no servers.config": faceActor(t, env, authz.FacesView, authz.FacesManage, authz.ServersView),
		"no faces.manage":   faceActor(t, env, authz.FacesView, authz.ServersView, authz.ServersConfig),
	} {
		if _, err := env.Svc.PatchFaceConfig(ctx, actor, tenant, cam.ServerID, inventory.FaceConfigPatch{Enabled: fp(true)}); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("%s = %v, want forbidden", name, err)
		}
	}
	bad := map[string]inventory.FaceConfigPatch{
		"empty":                 {},
		"threshold above one":   {DetectionThreshold: fp(1.5)},
		"negative unknown":      {UnknownScore: fp(-0.1)},
		"bad model":             {ModelSize: fp("huge")},
		"min_faces zero":        {MinFaces: fp(0)},
		"min_faces too many":    {MinFaces: fp(7)},
		"negative min_area":     {MinArea: fp(-1)},
		"save_attempts huge":    {SaveAttempts: fp(1_000_000)},
		"camera without change": {Cameras: []inventory.FaceCameraPatch{{CameraID: cam.ID}}},
		"camera twice":          {Cameras: []inventory.FaceCameraPatch{{CameraID: cam.ID, Enabled: fp(true)}, {CameraID: cam.ID, Enabled: fp(false)}}},
	}
	for name, p := range bad {
		var ve *inventory.ValidationError
		if _, err := env.Svc.PatchFaceConfig(ctx, manager, tenant, cam.ServerID, p); !errors.As(err, &ve) {
			t.Errorf("%s = %v, want validation error", name, err)
		}
	}
	// A camera of another server, and a server of another tenant, are not found.
	if _, err := env.Svc.PatchFaceConfig(ctx, manager, tenant, cam.ServerID, inventory.FaceConfigPatch{Cameras: []inventory.FaceCameraPatch{{CameraID: other.ID, Enabled: fp(true)}}}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("foreign camera = %v, want not found", err)
	}
	if _, err := env.Svc.PatchFaceConfig(ctx, manager, uuid.New(), cam.ServerID, inventory.FaceConfigPatch{Enabled: fp(true)}); err == nil {
		t.Error("wrong tenant accepted")
	}
	if n := len(mock.SetCalls()); n != 0 {
		t.Fatalf("rejected patches reached Frigate (%d writes)", n)
	}
}

func TestFaceConfigListNeedsNoTenantAndFiltersBySite(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()

	// A platform administrator has no tenant of its own and still sees every server.
	all, err := env.Svc.ListFaceConfig(ctx, env.Admin, inventory.FaceConfigFilter{})
	if err != nil || len(all) != 2 {
		t.Fatalf("admin without tenant = %d items, %v; want the two demo servers", len(all), err)
	}
	site := env.Cameras["frigate-h01/acceso_norte"].SiteID
	bySite, err := env.Svc.ListFaceConfig(ctx, env.Admin, inventory.FaceConfigFilter{SiteIDs: []uuid.UUID{site}})
	if err != nil || len(bySite) != 1 {
		t.Fatalf("site filter = %d of %d items, %v", len(bySite), len(all), err)
	}
	for _, it := range bySite {
		if it.SiteName == "" {
			t.Fatalf("filtered item without site: %+v", it)
		}
	}

	// A member without a tenant argument gets its own tenant, never another one.
	viewer := faceActor(t, env, authz.FacesView, authz.ServersView)
	own, err := env.Svc.ListFaceConfig(ctx, viewer, inventory.FaceConfigFilter{})
	if err != nil || len(own) != 2 {
		t.Fatalf("member without tenant = %d items, %v", len(own), err)
	}
	other := uuid.New()
	if _, err := env.Svc.ListFaceConfig(ctx, viewer, inventory.FaceConfigFilter{TenantID: &other}); err == nil {
		t.Fatal("member naming another tenant succeeded")
	}
	if _, err := env.Svc.ListFaceConfig(ctx, faceActor(t, env, authz.ServersView), inventory.FaceConfigFilter{}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("member without faces.view = %v, want forbidden", err)
	}
}
