//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func TestCameraPTZTours(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	in := inventory.PTZTourInput{Name: "Ronda", RepeatCount: 3, Steps: []inventory.PTZTourStep{
		{PresetToken: "1", PresetName: "Puerta", DwellSeconds: 10, Speed: 0.5},
		{PresetToken: "2", PresetName: "Playa", DwellSeconds: 5, Speed: 0.3},
	}}

	tour, err := env.Svc.CreateCameraPTZTour(ctx, env.Admin, cam.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	list, err := env.Svc.ListCameraPTZTours(ctx, env.Admin, cam.ID)
	if err != nil || len(list) != 1 || list[0].ID != tour.ID || len(list[0].Steps) != 2 || list[0].Steps[1].PresetName != "Playa" || list[0].RepeatCount != 3 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := env.Svc.CreateCameraPTZTour(ctx, env.Admin, cam.ID, in); err == nil {
		t.Fatal("duplicate name must be rejected")
	}

	in.Name, in.Steps = "Ronda 2", in.Steps[:1]
	upd, err := env.Svc.UpdateCameraPTZTour(ctx, env.Admin, cam.ID, tour.ID, in)
	if err != nil || upd.Name != "Ronda 2" || len(upd.Steps) != 1 {
		t.Fatalf("update = %+v, %v", upd, err)
	}

	t.Run("operator without cameras.manage cannot write", func(t *testing.T) {
		operator := env.Actor(t, "operador")
		if _, err := env.Svc.CreateCameraPTZTour(ctx, operator, cam.ID, in); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("create = %v, want forbidden", err)
		}
		if err := env.Svc.DeleteCameraPTZTour(ctx, operator, cam.ID, tour.ID); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("delete = %v, want forbidden", err)
		}
	})

	t.Run("another tenant sees nothing", func(t *testing.T) {
		other, err := env.Svc.CreateTenant(ctx, env.Admin, "otro", "Otro")
		if err != nil {
			t.Fatal(err)
		}
		otherAdmin, _, err := bootstrap.TenantUser(ctx, env.Store, other.ID, "admin-otro")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []authz.Permission{authz.CamerasView, authz.CamerasManage} {
			if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{SubjectType: "user", SubjectID: otherAdmin.UserID, Permission: p, Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &other.ID}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := env.Svc.ListCameraPTZTours(ctx, otherAdmin, cam.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("cross-tenant list = %v, want not found", err)
		}
		if err := env.Svc.DeleteCameraPTZTour(ctx, otherAdmin, cam.ID, tour.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("cross-tenant delete = %v, want not found", err)
		}
	})

	if err := env.Svc.DeleteCameraPTZTour(ctx, env.Admin, cam.ID, tour.ID); err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.DeleteCameraPTZTour(ctx, env.Admin, cam.ID, tour.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second delete = %v, want not found", err)
	}
}
