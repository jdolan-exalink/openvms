//go:build integration

package inventory_test

import (
	"context"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/vehicle"
)

func modulePatch(id string, on bool) camconfig.Patch {
	return camconfig.Patch{Analytics: &camconfig.AnalyticsPatch{Modules: map[string]*camconfig.ModulePatch{id: {Enabled: bptr(on)}}}}
}

func TestOpenVMSAnalyticsSwitchesAreStoredAuditedAndRespectedByTheWorker(t *testing.T) {
	env := demofix.Setup(t)
	useEngine(env, newFakeEngine())
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()

	local, err := env.Svc.LocalAnalytics(ctx, env.Admin, cam)
	if err != nil {
		t.Fatal(err)
	}
	for id, on := range local.Enabled {
		if !on {
			t.Fatalf("%s must default to on, as the worker did before the switch", id)
		}
	}

	rev := int64(0)
	for _, id := range []string{camconfig.ModuleVehicleColor, camconfig.ModuleClothingColor, camconfig.ModuleVehicleClassification} {
		res, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: modulePatch(id, false), ExpectedRevision: rev})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if res.State.SyncState != camconfig.SyncSynced || len(res.State.Drift) != 0 || res.Outcome.Result != camconfig.ResultOK {
			t.Fatalf("%s result = %+v", id, res)
		}
		rev = res.State.DesiredRevision
	}

	local, err = env.Svc.LocalAnalytics(ctx, env.Admin, cam)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{camconfig.ModuleVehicleColor, camconfig.ModuleClothingColor, camconfig.ModuleVehicleClassification} {
		if local.Enabled[id] {
			t.Fatalf("%s should be off: %+v", id, local)
		}
	}
	if on, err := vehicle.ModuleEnabled(ctx, env.Store, cam.ID, vehicle.ModuleVehicleColor); err != nil || on {
		t.Fatalf("worker vehicle color = %v (%v)", on, err)
	}
	if on, err := vehicle.ModuleEnabled(ctx, env.Store, cam.ID, vehicle.ModuleClothingColor); err != nil || on {
		t.Fatalf("worker clothing color = %v (%v)", on, err)
	}
	if on, err := vehicle.BodyEnabled(ctx, env.Store, cam.ServerID, cam.ID); err != nil || on {
		t.Fatalf("worker body classification = %v (%v)", on, err)
	}
	other := env.Cameras["frigate-h01/plaza"]
	if on, _ := vehicle.ModuleEnabled(ctx, env.Store, other.ID, vehicle.ModuleVehicleColor); !on {
		t.Fatal("another camera must keep the default")
	}

	rows := configAudit(t, env, cam.ID)
	if len(rows) != 3 {
		t.Fatalf("audit rows = %d", len(rows))
	}
	next, _ := rows[0].Details["new_values"].(map[string]any)
	if next["analytics.modules.vehicle_color.enabled"] != false {
		t.Fatalf("audit new_values = %+v", rows[0].Details)
	}

	// Switching it back on is a plain upsert.
	if _, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: modulePatch(camconfig.ModuleVehicleColor, true), ExpectedRevision: rev}); err != nil {
		t.Fatal(err)
	}
	if on, _ := vehicle.ModuleEnabled(ctx, env.Store, cam.ID, vehicle.ModuleVehicleColor); !on {
		t.Fatal("vehicle color should be on again")
	}
}

func TestOpenVMSAnalyticsSwitchesAreTenantIsolated(t *testing.T) {
	env := demofix.Setup(t)
	useEngine(env, newFakeEngine())
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()
	if _, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: modulePatch(camconfig.ModuleClothingColor, false)}); err != nil {
		t.Fatal(err)
	}
	tenant, err := env.Svc.CreateTenant(ctx, env.Admin, "otro-analitica", "Otro")
	if err != nil {
		t.Fatal(err)
	}
	intruder, _, err := bootstrap.TenantUser(ctx, env.Store, tenant.ID, "admin-otro-analitica")
	if err != nil {
		t.Fatal(err)
	}
	local, err := env.Svc.LocalAnalytics(ctx, intruder, cam)
	if err != nil {
		t.Fatal(err)
	}
	if !local.Enabled[camconfig.ModuleClothingColor] {
		t.Fatal("another tenant must not read this tenant's analytics rows")
	}
}
