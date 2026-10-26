//go:build integration

package inventory_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// fakeEngine is an in-memory Frigate: it keeps one camera document and can be told to fail.
type fakeEngine struct {
	frigate.Adapter
	mu          sync.Mutex
	doc         map[string]any
	patches     int
	patchErr    error
	readErr     error
	failReadsAt int // reads fail once patches reaches this value (0: never)
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{doc: map[string]any{
		"detect": map[string]any{"enabled": false},
		"record": map[string]any{"enabled": true},
		"lpr":    map[string]any{"enabled": false},
	}}
}

func (f *fakeEngine) Version() string { return "0.17.2" }
func (f *fakeEngine) StreamCodecs(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
func (f *fakeEngine) RawConfig(context.Context) (string, error) { return "cameras: {}\n", nil }
func (f *fakeEngine) CameraConfig(context.Context, string, bool) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil || (f.failReadsAt > 0 && f.patches >= f.failReadsAt) {
		return nil, errors.New("connection refused")
	}
	out := map[string]any{}
	for k, v := range f.doc {
		out[k] = v
	}
	return out, nil
}
func (f *fakeEngine) ApplyCameraPatch(_ context.Context, _ string, patch map[string]any) ([]frigate.SectionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.patchErr != nil {
		return nil, f.patchErr
	}
	f.patches++
	var res []frigate.SectionResult
	for sec, v := range patch {
		cur, _ := f.doc[sec].(map[string]any)
		next := map[string]any{}
		for k, x := range cur {
			next[k] = x
		}
		for k, x := range v.(map[string]any) {
			next[k] = x
		}
		f.doc[sec] = next
		res = append(res, frigate.SectionResult{Section: sec, AppliedLive: frigate.LiveSection(sec)})
	}
	return res, nil
}

func (f *fakeEngine) setExternally(section, key string, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.doc[section] = map[string]any{key: v}
}

func useEngine(env *demofix.Env, e *fakeEngine) {
	env.Svc.Connect = func(context.Context, frigate.ConnInfo) (frigate.Adapter, error) { return e, nil }
}

func bptr(b bool) *bool { return &b }

func detectionOn() camconfig.Patch {
	return camconfig.Patch{Detection: &camconfig.DetectionPatch{Enabled: bptr(true)}}
}

type auditRow struct {
	Action  string
	Details map[string]any
}

func configAudit(t *testing.T, env *demofix.Env, cam uuid.UUID) []auditRow {
	t.Helper()
	var rows []auditRow
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		r, err := tx.Query(context.Background(), `SELECT action, details FROM audit_log WHERE action = $1 AND target_id = $2 ORDER BY id`, inventory.ActionCameraConfigChanged, cam)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var a auditRow
			var raw []byte
			if err := r.Scan(&a.Action, &raw); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &a.Details); err != nil {
				return err
			}
			rows = append(rows, a)
		}
		return r.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func stateRows(t *testing.T, env *demofix.Env, scope store.TenantScope) int {
	t.Helper()
	var n int
	if err := env.Store.TxRaw(context.Background(), scope, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM camera_config_state`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCameraConfigReviewHasNoSideEffects(t *testing.T) {
	env := demofix.Setup(t)
	eng := newFakeEngine()
	useEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]

	rev, err := env.Svc.ReviewCameraConfig(context.Background(), env.Admin, cam.ID, detectionOn())
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Changes) != 1 || rev.Changes[0].Path != "detection.enabled" || rev.Changes[0].From != false || rev.Changes[0].To != true {
		t.Fatalf("review = %+v", rev)
	}
	if eng.patches != 0 || stateRows(t, env, store.AllTenants) != 0 || len(configAudit(t, env, cam.ID)) != 0 {
		t.Fatalf("review wrote something: patches=%d", eng.patches)
	}
}

func TestCameraConfigApplySuccessIsSyncedAndAudited(t *testing.T) {
	env := demofix.Setup(t)
	eng := newFakeEngine()
	useEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()

	res, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn(), Origin: camconfig.OriginProfile})
	if err != nil {
		t.Fatal(err)
	}
	if res.State.SyncState != camconfig.SyncSynced || res.State.DesiredRevision != 1 || res.State.AppliedRevision != 1 || res.Outcome.Result != camconfig.ResultOK {
		t.Fatalf("result = %+v", res)
	}
	if eng.doc["detect"].(map[string]any)["enabled"] != true {
		t.Fatal("engine was not changed")
	}
	rows := configAudit(t, env, cam.ID)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d", len(rows))
	}
	d := rows[0].Details
	prev, _ := d["previous_values"].(map[string]any)
	next, _ := d["new_values"].(map[string]any)
	if d["origin"] != "profile" || d["result"] != "ok" || prev["detection.enabled"] != false || next["detection.enabled"] != true {
		t.Fatalf("audit details = %+v", d)
	}

	// Defaults to the UI origin; the next apply needs the new revision.
	if _, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: camconfig.Patch{Recording: &camconfig.RecordingPatch{Enabled: bptr(false)}}, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if rows := configAudit(t, env, cam.ID); rows[1].Details["origin"] != "ui" {
		t.Fatalf("default origin = %v", rows[1].Details["origin"])
	}
}

func TestCameraConfigEngineFailureNeverLeavesSynced(t *testing.T) {
	env := demofix.Setup(t)
	eng := newFakeEngine()
	eng.patchErr = errors.New("frigate said no at http://admin:hunter2@10.0.0.5:5000/api/config/set?token=abc")
	useEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()

	res, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn()})
	if err != nil {
		t.Fatal(err)
	}
	if res.State.SyncState != camconfig.SyncError || res.State.AppliedRevision != 0 || res.State.DesiredRevision != 1 || res.Outcome.Result != camconfig.ResultError {
		t.Fatalf("result = %+v", res)
	}
	for _, leak := range []string{"hunter2", "abc"} {
		if contains(res.State.LastError, leak) {
			t.Fatalf("last_error leaks %q: %q", leak, res.State.LastError)
		}
	}
	ce, err := env.Svc.OpenCameraEngine(ctx, env.Admin, cam.ID)
	if err != nil || ce.State == nil || ce.State.SyncState != camconfig.SyncError {
		t.Fatalf("stored state = %+v (%v)", ce.State, err)
	}
	if rows := configAudit(t, env, cam.ID); len(rows) != 1 || rows[0].Details["result"] != "error" {
		t.Fatalf("audit = %+v", rows)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestCameraConfigStaleRevisionConflicts(t *testing.T) {
	env := demofix.Setup(t)
	useEngine(env, newFakeEngine())
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()

	if _, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn()}); err != nil {
		t.Fatal(err)
	}
	_, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn(), ExpectedRevision: 0})
	if !errors.Is(err, inventory.ErrRevisionConflict) {
		t.Fatalf("err = %v, want revision conflict", err)
	}
	if rows := configAudit(t, env, cam.ID); len(rows) != 1 {
		t.Fatalf("a conflicting apply must not be audited as a change: %d rows", len(rows))
	}
}

func TestCameraConfigUnreadableEngineAfterApplyIsUnverifiable(t *testing.T) {
	env := demofix.Setup(t)
	eng := newFakeEngine()
	eng.failReadsAt = 1 // reads work until the patch lands, then the engine goes dark
	useEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]

	res, err := env.Svc.ApplyCameraConfig(context.Background(), env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn()})
	if err != nil {
		t.Fatal(err)
	}
	if res.State.SyncState != camconfig.SyncUnverifiable || res.State.AppliedRevision != 0 || res.Outcome.Result != camconfig.ResultUnverified {
		t.Fatalf("result = %+v", res)
	}
}

func TestCameraConfigDetectsExternalDrift(t *testing.T) {
	env := demofix.Setup(t)
	eng := newFakeEngine()
	useEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()
	if _, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn()}); err != nil {
		t.Fatal(err)
	}

	eng.setExternally("detect", "enabled", false) // someone edits Frigate by hand

	ce, err := env.Svc.OpenCameraEngine(ctx, env.Admin, cam.ID)
	if err != nil {
		t.Fatal(err)
	}
	model, err := camconfig.FrigateAdapter{Engine: eng}.Read(ctx, inventory.CameraConfigInventory(ce.Camera, ce.ServerName))
	if err != nil {
		t.Fatal(err)
	}
	st := camconfig.ComputeState(inventory.StoredConfigState(ce.State), model)
	if st.SyncState != camconfig.SyncDrift || len(st.Drift) != 1 || st.Drift[0].Path != "detection.enabled" || st.Drift[0].Desired != true || st.Drift[0].Effective != false {
		t.Fatalf("state = %+v", st)
	}
}

func TestCameraConfigRequiresTheFrigateConfigPermission(t *testing.T) {
	env := demofix.Setup(t)
	eng := newFakeEngine()
	useEngine(env, eng)
	operator := env.Actor(t, "operador")
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()

	if _, err := env.Svc.ReviewCameraConfig(ctx, operator, cam.ID, detectionOn()); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("review err = %v, want forbidden", err)
	}
	if _, err := env.Svc.ApplyCameraConfig(ctx, operator, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn()}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("apply err = %v, want forbidden", err)
	}
	if eng.patches != 0 || stateRows(t, env, store.AllTenants) != 0 {
		t.Fatal("a denied apply must not write anything")
	}
}

func TestCameraConfigStateIsTenantIsolated(t *testing.T) {
	env := demofix.Setup(t)
	useEngine(env, newFakeEngine())
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()
	if _, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn()}); err != nil {
		t.Fatal(err)
	}

	other, err := env.Svc.CreateTenant(ctx, env.Admin, "otro", "Otro municipio")
	if err != nil {
		t.Fatal(err)
	}
	otherAdmin, _, err := bootstrap.TenantUser(ctx, env.Store, other.ID, "admin-otro")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []authz.Permission{authz.CamerasView, authz.ServersConfig} {
		if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: otherAdmin.UserID, Permission: p,
			Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &other.ID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if n := stateRows(t, env, store.TenantScope{TenantID: cam.TenantID}); n != 1 {
		t.Fatalf("owner tenant sees %d rows", n)
	}
	if n := stateRows(t, env, store.TenantScope{TenantID: other.ID}); n != 0 {
		t.Fatalf("other tenant sees %d rows", n)
	}
	if _, err := env.Svc.ApplyCameraConfig(ctx, otherAdmin, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn(), ExpectedRevision: 1}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant apply = %v, want not found", err)
	}
}

// grantAt gives a fresh tenant user the permissions at tenant scope and returns it.
func grantAt(t *testing.T, env *demofix.Env, name string, tenant uuid.UUID, perms ...authz.Permission) authz.Actor {
	t.Helper()
	u, _, err := bootstrap.TenantUser(context.Background(), env.Store, tenant, name)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range perms {
		if _, err := env.Svc.CreateGrant(context.Background(), env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: u.UserID, Permission: p,
			Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenant,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return u
}

func TestCameraConfigSectionPermissions(t *testing.T) {
	env := demofix.Setup(t)
	eng := newFakeEngine()
	useEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()
	multi := camconfig.Patch{
		Detection: &camconfig.DetectionPatch{Enabled: bptr(true)},
		Recording: &camconfig.RecordingPatch{Enabled: bptr(false)},
		Analytics: &camconfig.AnalyticsPatch{LPR: bptr(true)},
	}
	recordingOnly := camconfig.Patch{Recording: &camconfig.RecordingPatch{Enabled: bptr(false)}}

	// Legacy holder (servers.config only): everything keeps working exactly as before.
	legacy := grantAt(t, env, "legacy", cam.TenantID, authz.CamerasView, authz.ServersConfig)
	res, err := env.Svc.ApplyCameraConfig(ctx, legacy, cam.ID, inventory.ConfigApplyInput{Patch: multi})
	if err != nil {
		t.Fatalf("legacy servers.config holder lost access: %v", err)
	}
	rev := res.State.DesiredRevision

	// cameras.manage alone and cameras.view alone never gain config writes.
	for name, perms := range map[string][]authz.Permission{"manage": {authz.CamerasView, authz.CamerasManage}, "view": {authz.CamerasView}} {
		u := grantAt(t, env, "only-"+name, cam.TenantID, perms...)
		if _, err := env.Svc.ApplyCameraConfig(ctx, u, cam.ID, inventory.ConfigApplyInput{Patch: recordingOnly}); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("%s alone: err = %v, want forbidden", name, err)
		}
	}

	// A granular actor needs EVERY touched section; denied ones are listed, nothing else.
	gran := grantAt(t, env, "gran", cam.TenantID, authz.CamerasView, authz.ServersConfig, authz.CamerasConfigView, authz.CamerasConfigEdit, authz.CamerasConfigRecording)
	patches := eng.patches
	_, err = env.Svc.ApplyCameraConfig(ctx, gran, cam.ID, inventory.ConfigApplyInput{Patch: multi, ExpectedRevision: rev})
	var sf *inventory.SectionsForbiddenError
	if !errors.As(err, &sf) || !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("multi-section apply err = %v, want SectionsForbiddenError", err)
	}
	if len(sf.Sections) != 1 || sf.Sections[0] != "analytics" {
		t.Fatalf("denied sections = %v, want [analytics]", sf.Sections)
	}
	if _, err := env.Svc.ReviewCameraConfig(ctx, gran, cam.ID, multi); !errors.As(err, &sf) {
		t.Fatalf("review must enforce sections too: %v", err)
	}
	if eng.patches != patches {
		t.Fatal("a denied section must not write anything")
	}
	allowed := camconfig.Patch{Detection: multi.Detection, Recording: multi.Recording}
	res, err = env.Svc.ApplyCameraConfig(ctx, gran, cam.ID, inventory.ConfigApplyInput{Patch: allowed, ExpectedRevision: rev})
	if err != nil {
		t.Fatalf("granted sections refused: %v", err)
	}
	rev = res.State.DesiredRevision

	// Revoking one granular grant takes effect even for a servers.config holder; a deny wins.
	noVideo := grantAt(t, env, "novideo", cam.TenantID, authz.CamerasView, authz.ServersConfig, authz.CamerasConfigEdit)
	if _, err := env.Svc.ApplyCameraConfig(ctx, noVideo, cam.ID, inventory.ConfigApplyInput{Patch: recordingOnly}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("revoked section err = %v", err)
	}
	denied := grantAt(t, env, "denied", cam.TenantID, authz.CamerasView, authz.ServersConfig)
	if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
		SubjectType: "user", SubjectID: denied.UserID, Permission: authz.CamerasConfigRecording,
		Effect: authz.Deny, ScopeType: authz.ScopeTenant, ScopeID: &cam.TenantID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Svc.ApplyCameraConfig(ctx, denied, cam.ID, inventory.ConfigApplyInput{Patch: recordingOnly}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("denied section err = %v", err)
	}
	if _, err := env.Svc.ApplyCameraConfig(ctx, denied, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn(), ExpectedRevision: rev}); err != nil {
		t.Fatalf("a deny on recording must not block detection: %v", err)
	}
}

func TestCameraWebAccessPermission(t *testing.T) {
	env := demofix.Setup(t)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()
	check := func(u authz.Actor) error { return env.Svc.RequireCameraWebAccess(ctx, u, cam.ID) }

	if err := check(grantAt(t, env, "pair", cam.TenantID, authz.ServersManage, authz.ServersConfigSecrets)); err != nil {
		t.Errorf("legacy pair lost web access: %v", err)
	}
	if err := check(grantAt(t, env, "half", cam.TenantID, authz.ServersManage)); !errors.Is(err, access.ErrForbidden) {
		t.Errorf("servers.manage alone: %v", err)
	}
	if err := check(grantAt(t, env, "cfg", cam.TenantID, authz.ServersConfig, authz.CamerasManage)); !errors.Is(err, access.ErrForbidden) {
		t.Errorf("servers.config + cameras.manage must not open the camera web: %v", err)
	}
	if err := check(grantAt(t, env, "web", cam.TenantID, authz.CamerasWebAccess)); err != nil {
		t.Errorf("cameras.web.access refused: %v", err)
	}
	other, err := env.Svc.CreateTenant(ctx, env.Admin, "otro-web", "Otro")
	if err != nil {
		t.Fatal(err)
	}
	foreign := grantAt(t, env, "foreign", other.ID, authz.CamerasWebAccess)
	if err := check(foreign); err == nil {
		t.Error("a grant in another tenant must not open this camera's web")
	}
}

func TestCameraPTZPermission(t *testing.T) {
	env := demofix.Setup(t)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()
	if err := env.Svc.RequireCameraPTZ(ctx, grantAt(t, env, "mgr", cam.TenantID, authz.CamerasManage), cam.ID); err != nil {
		t.Errorf("legacy cameras.manage lost PTZ: %v", err)
	}
	if err := env.Svc.RequireCameraPTZ(ctx, grantAt(t, env, "viewer", cam.TenantID, authz.CamerasView), cam.ID); !errors.Is(err, access.ErrForbidden) {
		t.Errorf("cameras.view alone: %v", err)
	}
	if err := env.Svc.RequireCameraPTZ(ctx, grantAt(t, env, "ptz", cam.TenantID, authz.CamerasConfigPTZ), cam.ID); err != nil {
		t.Errorf("cameras.config.ptz refused: %v", err)
	}
	strict := grantAt(t, env, "strict", cam.TenantID, authz.CamerasManage, authz.CamerasConfigEdit)
	if err := env.Svc.RequireCameraPTZ(ctx, strict, cam.ID); !errors.Is(err, access.ErrForbidden) {
		t.Errorf("a granular actor without config.ptz must be refused: %v", err)
	}
}
