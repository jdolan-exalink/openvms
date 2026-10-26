//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// tenantAdmin is a user of the demo tenant who may view and manage its cameras and edit the
// configuration of its servers.
func tenantAdmin(t *testing.T, env *demofix.Env) authz.Actor {
	t.Helper()
	ctx := context.Background()
	u, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "perfiles")
	if err != nil {
		t.Fatal(err)
	}
	tid := env.Demo.TenantID
	for _, p := range []authz.Permission{authz.CamerasView, authz.CamerasManage, authz.ServersView, authz.ServersConfig} {
		if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: u.UserID, Permission: p, Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tid,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return u
}

func exteriorProfile() camconfig.Patch {
	return camconfig.Patch{
		Detection: &camconfig.DetectionPatch{Enabled: bptr(true)},
		Recording: &camconfig.RecordingPatch{Enabled: bptr(false)},
	}
}

func storedDesired(t *testing.T, env *demofix.Env, a authz.Actor, cam uuid.UUID) camconfig.Stored {
	t.Helper()
	ce, err := env.Svc.OpenCameraEngine(context.Background(), a, cam)
	if err != nil {
		t.Fatal(err)
	}
	return inventory.StoredConfigState(ce.State)
}

func auditCount(t *testing.T, env *demofix.Env, action string, target uuid.UUID) int {
	t.Helper()
	var n int
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = $1 AND target_id = $2`, action, target).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestProfileAssignPreviewsThenAppliesWithOriginProfile(t *testing.T) {
	env := demofix.Setup(t)
	eng := newFakeEngine()
	useEngine(env, eng)
	a := tenantAdmin(t, env)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]

	p, err := env.Svc.CreateConfigProfile(ctx, a, inventory.ProfileInput{Name: "Exterior", Patch: exteriorProfile()})
	if err != nil {
		t.Fatal(err)
	}
	in := inventory.ProfileAssignInput{ProfileID: &p.ID}
	rv, err := env.Svc.ReviewCameraProfile(ctx, a, cam.ID, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rv.Parameters != 2 || len(rv.Review.Changes) != 2 {
		t.Fatalf("review = %+v", rv)
	}
	if eng.patches != 0 || auditCount(t, env, inventory.ActionCameraConfigProfileChanged, cam.ID) != 0 {
		t.Fatal("review wrote something")
	}

	res, err := env.Svc.ApplyCameraProfile(ctx, a, cam.ID, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Apply == nil || res.Apply.State.SyncState != camconfig.SyncSynced {
		t.Fatalf("apply = %+v", res.Apply)
	}
	if eng.doc["detect"].(map[string]any)["enabled"] != true || eng.doc["record"].(map[string]any)["enabled"] != false {
		t.Fatalf("engine = %+v", eng.doc)
	}
	// The camera keeps no override: the values come from the profile.
	st := storedDesired(t, env, a, cam.ID)
	if !st.Desired.Empty() {
		t.Fatalf("assigning a profile wrote overrides: %+v", st.Desired)
	}
	rows := configAudit(t, env, cam.ID)
	if len(rows) != 1 || rows[0].Details["origin"] != "profile" {
		t.Fatalf("config audit = %+v", rows)
	}
	if auditCount(t, env, inventory.ActionCameraConfigProfileChanged, cam.ID) != 1 {
		t.Fatal("assignment was not audited")
	}
	ref, patch, err := env.Svc.CameraProfileContext(ctx, a, cam.ID)
	if err != nil || ref == nil || ref.Name != "Exterior" || ref.Outdated || patch.Empty() {
		t.Fatalf("context = %+v %+v %v", ref, patch, err)
	}

	// Editing the profile does not push to the camera; it marks it outdated.
	up, err := env.Svc.UpdateConfigProfile(ctx, a, p.ID, inventory.ProfileInput{Name: "Exterior", Patch: camconfig.Patch{Detection: &camconfig.DetectionPatch{Enabled: bptr(false)}}})
	if err != nil || up.Version != 2 {
		t.Fatalf("update = %+v %v", up, err)
	}
	if eng.doc["detect"].(map[string]any)["enabled"] != true {
		t.Fatal("profile edit reached the camera")
	}
	ref, _, _ = env.Svc.CameraProfileContext(ctx, a, cam.ID)
	if !ref.Outdated {
		t.Fatal("the camera is not marked outdated")
	}
	v, cams, err := env.Svc.GetConfigProfile(ctx, a, p.ID)
	if err != nil || v.CameraCount != 1 || len(cams) != 1 || !cams[0].Outdated {
		t.Fatalf("detail = %+v %+v %v", v, cams, err)
	}
}

func TestProfileNeverRewritesOverrides(t *testing.T) {
	env := demofix.Setup(t)
	eng := newFakeEngine()
	useEngine(env, eng)
	a := tenantAdmin(t, env)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]

	if _, err := env.Svc.ApplyCameraConfig(ctx, a, cam.ID, inventory.ConfigApplyInput{Patch: camconfig.Patch{Recording: &camconfig.RecordingPatch{Enabled: bptr(true)}}}); err != nil {
		t.Fatal(err)
	}
	p, _ := env.Svc.CreateConfigProfile(ctx, a, inventory.ProfileInput{Name: "Exterior", Patch: exteriorProfile()})
	in := inventory.ProfileAssignInput{ProfileID: &p.ID}
	rv, err := env.Svc.ReviewCameraProfile(ctx, a, cam.ID, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rv.Parameters != 1 || len(rv.Shadowed) != 1 || rv.Shadowed[0] != "recording.enabled" {
		t.Fatalf("review = %+v", rv)
	}
	if _, err := env.Svc.ApplyCameraProfile(ctx, a, cam.ID, in, nil); err != nil {
		t.Fatal(err)
	}
	if eng.doc["record"].(map[string]any)["enabled"] != true {
		t.Fatal("the profile overwrote the camera's own override")
	}
	if st := storedDesired(t, env, a, cam.ID); st.Desired.Recording == nil || st.Desired.Detection != nil {
		t.Fatalf("overrides = %+v", st.Desired)
	}
}

func TestProfileUnlinkKeepValues(t *testing.T) {
	for _, keep := range []bool{true, false} {
		env := demofix.Setup(t)
		useEngine(env, newFakeEngine())
		a := tenantAdmin(t, env)
		ctx := context.Background()
		cam := env.Cameras["frigate-h01/acceso_norte"]
		p, _ := env.Svc.CreateConfigProfile(ctx, a, inventory.ProfileInput{Name: "Exterior", Patch: exteriorProfile()})
		if _, err := env.Svc.ApplyCameraProfile(ctx, a, cam.ID, inventory.ProfileAssignInput{ProfileID: &p.ID}, nil); err != nil {
			t.Fatal(err)
		}
		if err := env.Svc.DeleteConfigProfile(ctx, a, p.ID); err == nil {
			t.Fatal("deleting a profile in use succeeded")
		}
		un := inventory.ProfileAssignInput{KeepValues: keep}
		rv, err := env.Svc.ReviewCameraProfile(ctx, a, cam.ID, un, nil)
		if err != nil || (keep && len(rv.Converted) != 2) || (!keep && len(rv.Dropped) != 2) {
			t.Fatalf("keep=%v review = %+v %v", keep, rv, err)
		}
		if _, err := env.Svc.ApplyCameraProfile(ctx, a, cam.ID, un, nil); err != nil {
			t.Fatal(err)
		}
		st := storedDesired(t, env, a, cam.ID)
		if keep == st.Desired.Empty() {
			t.Fatalf("keep=%v overrides = %+v", keep, st.Desired)
		}
		if ref, _, _ := env.Svc.CameraProfileContext(ctx, a, cam.ID); ref != nil {
			t.Fatal("still linked after unlink")
		}
		if err := env.Svc.DeleteConfigProfile(ctx, a, p.ID); err != nil {
			t.Fatalf("delete after unlink: %v", err)
		}
		if auditCount(t, env, inventory.ActionCameraConfigProfileChanged, cam.ID) != 2 {
			t.Fatal("assign and unassign were not both audited")
		}
	}
}

func TestProfileDeleteInUseIsRefused(t *testing.T) {
	env := demofix.Setup(t)
	useEngine(env, newFakeEngine())
	a := tenantAdmin(t, env)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	p, _ := env.Svc.CreateConfigProfile(ctx, a, inventory.ProfileInput{Name: "Exterior", Patch: exteriorProfile()})
	if _, err := env.Svc.ApplyCameraProfile(ctx, a, cam.ID, inventory.ProfileAssignInput{ProfileID: &p.ID}, nil); err != nil {
		t.Fatal(err)
	}
	var inUse *inventory.ProfileInUseError
	if err := env.Svc.DeleteConfigProfile(ctx, a, p.ID); !errors.As(err, &inUse) || inUse.Cameras != 1 {
		t.Fatalf("err = %v", err)
	}
}

func TestProfilesAreTenantIsolatedAndNamesUnique(t *testing.T) {
	env := demofix.Setup(t)
	a := tenantAdmin(t, env)
	ctx := context.Background()
	p, err := env.Svc.CreateConfigProfile(ctx, a, inventory.ProfileInput{Name: "Exterior", Patch: exteriorProfile()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Svc.CreateConfigProfile(ctx, a, inventory.ProfileInput{Name: "exterior", Patch: exteriorProfile()}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate name err = %v", err)
	}
	if _, err := env.Svc.CreateConfigProfile(ctx, a, inventory.ProfileInput{Name: "Bad", Patch: camconfig.Patch{}}); err == nil {
		t.Fatal("empty profile accepted")
	}
	other, err := env.Svc.CreateTenant(ctx, env.Admin, "other", "Other")
	if err != nil {
		t.Fatal(err)
	}
	oa, _, err := bootstrap.TenantUser(ctx, env.Store, other.ID, "otheradmin")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.Svc.GetConfigProfile(ctx, oa, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant get err = %v", err)
	}
	if err := env.Svc.DeleteConfigProfile(ctx, oa, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant delete err = %v", err)
	}
	if list, err := env.Svc.ListConfigProfiles(ctx, oa); err == nil && len(list) != 0 {
		t.Fatalf("other tenant sees %d profiles", len(list))
	}
}
