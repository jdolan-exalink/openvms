//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func names(t *testing.T, env *demofix.Env, actor authz.Actor) []string {
	t.Helper()
	cams, err := env.Svc.ListCameras(context.Background(), actor, inventory.CameraFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for key, c := range env.Cameras {
		for _, got := range cams {
			if got.ID == c.ID {
				out = append(out, key)
			}
		}
	}
	slices.Sort(out)
	return out
}

func TestPermissionEngine(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	operator := env.Actor(t, "operador")
	supervisor := env.Actor(t, "supervisor")

	t.Run("Operator-A sees only its cameras (PRD §129)", func(t *testing.T) {
		got := names(t, env, operator)
		want := []string{"frigate-c01/muelle", "frigate-h01/acceso_norte"}
		if !slices.Equal(got, want) {
			t.Fatalf("operator cameras = %v, want %v", got, want)
		}
		for _, key := range want {
			if _, err := env.Svc.GetCamera(ctx, operator, env.Cameras[key].ID); err != nil {
				t.Errorf("GetCamera(%s): %v", key, err)
			}
		}
		for _, key := range []string{"frigate-h01/plaza", "frigate-c01/plaza"} {
			if _, err := env.Svc.GetCamera(ctx, operator, env.Cameras[key].ID); !errors.Is(err, access.ErrForbidden) {
				t.Errorf("GetCamera(%s) = %v, want forbidden", key, err)
			}
		}
	})

	t.Run("filters by server return nothing unauthorized", func(t *testing.T) {
		srv := env.Cameras["frigate-h01/plaza"].ServerID
		cams, err := env.Svc.ListCameras(ctx, operator, inventory.CameraFilter{ServerID: &srv})
		if err != nil {
			t.Fatal(err)
		}
		if len(cams) != 1 || cams[0].RemoteName != "acceso_norte" {
			t.Fatalf("operator cameras on frigate-h01 = %+v", cams)
		}
	})

	t.Run("site grant with a camera DENY", func(t *testing.T) {
		got := names(t, env, supervisor)
		want := []string{"frigate-h01/acceso_norte", "frigate-h01/cementerio", "frigate-h01/plaza"}
		if !slices.Equal(got, want) {
			t.Fatalf("supervisor cameras = %v, want %v", got, want)
		}
		if _, err := env.Svc.GetCamera(ctx, supervisor, env.Cameras["frigate-h01/tesoreria"].ID); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("tesoreria = %v, want forbidden", err)
		}
		sites, err := env.Svc.ListSites(ctx, supervisor, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(sites) != 1 || sites[0].Name != "Helvecia" {
			t.Errorf("supervisor sites = %+v", sites)
		}
	})

	t.Run("operator cannot manage", func(t *testing.T) {
		name := "renamed"
		_, err := env.Svc.UpdateCamera(ctx, operator, env.Cameras["frigate-h01/acceso_norte"].ID, inventory.CameraUpdate{DisplayName: &name})
		if !errors.Is(err, access.ErrForbidden) {
			t.Errorf("UpdateCamera = %v, want forbidden", err)
		}
		site := env.Cameras["frigate-h01/acceso_norte"].SiteID
		_, err = env.Svc.CreateGrant(ctx, operator, inventory.GrantInput{
			SubjectType: "user", SubjectID: operator.UserID, Permission: authz.CamerasView,
			Effect: authz.Allow, ScopeType: authz.ScopeSite, ScopeID: &site,
		})
		if !errors.Is(err, access.ErrForbidden) {
			t.Errorf("self-grant = %v, want forbidden", err)
		}
	})

	// A second tenant with its own administrator.
	other, err := env.Svc.CreateTenant(ctx, env.Admin, "otro", "Otro municipio")
	if err != nil {
		t.Fatal(err)
	}
	otherAdmin, _, err := bootstrap.TenantUser(ctx, env.Store, other.ID, "admin-otro")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []authz.Permission{authz.PermissionsManage, authz.CamerasView, authz.SitesView, authz.SitesManage} {
		if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: otherAdmin.UserID, Permission: p,
			Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &other.ID,
		}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("tenant isolation returns 404", func(t *testing.T) {
		if got := names(t, env, otherAdmin); len(got) != 0 {
			t.Fatalf("other tenant sees %v", got)
		}
		if _, err := env.Svc.GetCamera(ctx, otherAdmin, env.Cameras["frigate-h01/plaza"].ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("cross-tenant GetCamera = %v, want not found", err)
		}
		if _, err := env.Svc.GetSite(ctx, otherAdmin, env.Cameras["frigate-h01/plaza"].SiteID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("cross-tenant GetSite = %v, want not found", err)
		}
		// A cross-tenant grant attempt must not reveal the demo camera either: it gets
		// the same "does not exist" validation error as a random id.
		cam := env.Cameras["frigate-h01/plaza"].ID
		_, err := env.Svc.CreateGrant(ctx, otherAdmin, inventory.GrantInput{
			SubjectType: "user", SubjectID: otherAdmin.UserID, Permission: authz.CamerasView,
			Effect: authz.Allow, ScopeType: authz.ScopeCamera, ScopeID: &cam,
		})
		var ve *inventory.ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("cross-tenant grant = %v, want does-not-exist validation error", err)
		}
	})

	t.Run("nobody grants what they do not hold", func(t *testing.T) {
		_, err := env.Svc.CreateGrant(ctx, otherAdmin, inventory.GrantInput{
			SubjectType: "user", SubjectID: otherAdmin.UserID, Permission: authz.LiveView,
			Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &other.ID,
		})
		if !errors.Is(err, access.ErrForbidden) {
			t.Errorf("escalation = %v, want forbidden", err)
		}
		// DENY needs only permissions.manage.
		if _, err := env.Svc.CreateGrant(ctx, otherAdmin, inventory.GrantInput{
			SubjectType: "user", SubjectID: otherAdmin.UserID, Permission: authz.LiveView,
			Effect: authz.Deny, ScopeType: authz.ScopeTenant, ScopeID: &other.ID,
		}); err != nil {
			t.Errorf("deny grant: %v", err)
		}
	})

	t.Run("row-level security hides other tenants even from raw SQL", func(t *testing.T) {
		var n int
		tx, err := env.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", other.ID.String()); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM cameras").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("tenant %s sees %d cameras through RLS", other.Slug, n)
		}
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM sites").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("tenant %s sees %d sites through RLS", other.Slug, n)
		}
	})

	t.Run("audit log is append-only and records changes", func(t *testing.T) {
		var n int
		if err := env.Pool.QueryRow(ctx, "SELECT count(*) FROM audit_log WHERE action = 'PERMISSION_CHANGED'").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Error("no PERMISSION_CHANGED audit entries")
		}
		if _, err := env.Pool.Exec(ctx, "UPDATE audit_log SET action = 'x'"); err == nil {
			t.Error("UPDATE audit_log succeeded")
		}
		if _, err := env.Pool.Exec(ctx, "DELETE FROM audit_log"); err == nil {
			t.Error("DELETE FROM audit_log succeeded")
		}
	})

	t.Run("unknown ids are 404, not 403", func(t *testing.T) {
		if _, err := env.Svc.GetCamera(ctx, operator, uuid.New()); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("GetCamera(random) = %v", err)
		}
	})
}
