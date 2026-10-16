//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func groupIDs(g inventory.CameraGroupView) []uuid.UUID {
	ids := slices.Clone(g.CameraIDs)
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return ids
}

func TestDynamicCameraGroups(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	acceso := env.Cameras["frigate-h01/acceso_norte"]
	plaza := env.Cameras["frigate-h01/plaza"]
	muelle := env.Cameras["frigate-c01/muelle"]
	tenant := acceso.TenantID
	tag := func(c uuid.UUID, tags ...string) {
		t.Helper()
		if _, err := env.Svc.UpdateCamera(ctx, env.Admin, c, inventory.CameraUpdate{Tags: &tags}); err != nil {
			t.Fatal(err)
		}
	}
	tag(acceso.ID, "exterior")
	tag(muelle.ID, "Exterior", "agua")

	t.Run("materializes by tags and re-materializes on camera update", func(t *testing.T) {
		g, err := env.Svc.CreateCameraGroup(ctx, env.Admin, inventory.CameraGroupInput{
			TenantID: tenant, Name: "Exteriores", Filter: &inventory.CameraGroupFilter{Tags: []string{"EXTERIOR"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if g.MemberCountTotal != 2 || g.MemberCountHidden != 0 || g.Version != 1 || g.DynamicFilter == nil {
			t.Fatalf("created = %+v", g)
		}
		tag(plaza.ID, "exterior")
		tag(acceso.ID, "interior")
		got, err := env.Svc.GetCameraGroup(ctx, env.Admin, g.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := []uuid.UUID{muelle.ID, plaza.ID}
		slices.SortFunc(want, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
		if !slices.Equal(groupIDs(got), want) || got.MemberCountTotal != 2 {
			t.Fatalf("members = %v total %d, want %v", got.CameraIDs, got.MemberCountTotal, want)
		}
	})

	t.Run("AND across fields, OR within a field", func(t *testing.T) {
		g, err := env.Svc.CreateCameraGroup(ctx, env.Admin, inventory.CameraGroupInput{
			TenantID: tenant, Name: "Helvecia acceso",
			Filter: &inventory.CameraGroupFilter{SiteIDs: []uuid.UUID{acceso.SiteID, muelle.SiteID}, NameContains: "acc"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(g.CameraIDs, []uuid.UUID{acceso.ID}) {
			t.Fatalf("members = %v, want only acceso_norte", g.CameraIDs)
		}
	})

	t.Run("rejects invalid filters and manual edits on dynamic groups", func(t *testing.T) {
		var ve *inventory.ValidationError
		_, err := env.Svc.CreateCameraGroup(ctx, env.Admin, inventory.CameraGroupInput{
			TenantID: tenant, Name: "vacio", Filter: &inventory.CameraGroupFilter{},
		})
		if !errors.As(err, &ve) {
			t.Fatalf("empty filter err = %v", err)
		}
		_, err = env.Svc.CreateCameraGroup(ctx, env.Admin, inventory.CameraGroupInput{
			TenantID: tenant, Name: "mixto", CameraIDs: []uuid.UUID{acceso.ID},
			Filter: &inventory.CameraGroupFilter{Tags: []string{"x"}},
		})
		if !errors.As(err, &ve) {
			t.Fatalf("members+filter err = %v", err)
		}
		g, err := env.Svc.CreateCameraGroup(ctx, env.Admin, inventory.CameraGroupInput{
			TenantID: tenant, Name: "dyn", Filter: &inventory.CameraGroupFilter{NameContains: "plaza"},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = env.Svc.ReplaceCameraGroup(ctx, env.Admin, g.ID, inventory.CameraGroupInput{
			TenantID: tenant, Name: "dyn", CameraIDs: []uuid.UUID{acceso.ID},
		})
		if !errors.As(err, &ve) {
			t.Fatalf("manual edit on dynamic err = %v", err)
		}
		manual, err := env.Svc.CreateCameraGroup(ctx, env.Admin, inventory.CameraGroupInput{TenantID: tenant, Name: "man", CameraIDs: []uuid.UUID{acceso.ID}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = env.Svc.ReplaceCameraGroup(ctx, env.Admin, manual.ID, inventory.CameraGroupInput{
			TenantID: tenant, Name: "man", Filter: &inventory.CameraGroupFilter{Tags: []string{"x"}},
		})
		if !errors.As(err, &ve) {
			t.Fatalf("manual->dynamic err = %v", err)
		}
	})

	t.Run("update re-materializes and checks the version", func(t *testing.T) {
		g, err := env.Svc.CreateCameraGroup(ctx, env.Admin, inventory.CameraGroupInput{
			TenantID: tenant, Name: "versionado", Filter: &inventory.CameraGroupFilter{NameContains: "plaza"},
		})
		if err != nil {
			t.Fatal(err)
		}
		one := int32(1)
		up, err := env.Svc.ReplaceCameraGroup(ctx, env.Admin, g.ID, inventory.CameraGroupInput{
			TenantID: tenant, Name: "versionado", Filter: &inventory.CameraGroupFilter{NameContains: "muelle"}, ExpectedVersion: &one,
		})
		if err != nil {
			t.Fatal(err)
		}
		if up.Version != 2 || !slices.Equal(up.CameraIDs, []uuid.UUID{muelle.ID}) {
			t.Fatalf("updated = v%d %v", up.Version, up.CameraIDs)
		}
		_, err = env.Svc.ReplaceCameraGroup(ctx, env.Admin, g.ID, inventory.CameraGroupInput{
			TenantID: tenant, Name: "versionado", Filter: &inventory.CameraGroupFilter{NameContains: "muelle"}, ExpectedVersion: &one,
		})
		if !errors.Is(err, inventory.ErrCameraGroupVersionConflict) {
			t.Fatalf("stale err = %v, want ErrCameraGroupVersionConflict", err)
		}
	})

	t.Run("coverage counts hide members the caller cannot view", func(t *testing.T) {
		g, err := env.Svc.CreateCameraGroup(ctx, env.Admin, inventory.CameraGroupInput{
			TenantID: tenant, Name: "Helvecia todo", Filter: &inventory.CameraGroupFilter{SiteIDs: []uuid.UUID{acceso.SiteID}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if g.MemberCountHidden != 0 || g.MemberCountTotal < 4 {
			t.Fatalf("admin counts = total %d hidden %d", g.MemberCountTotal, g.MemberCountHidden)
		}
		operator := env.Actor(t, "operador")
		got, err := env.Svc.GetCameraGroup(ctx, operator, g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.CameraIDs, []uuid.UUID{acceso.ID}) || got.MemberCountTotal != g.MemberCountTotal || got.MemberCountHidden != g.MemberCountTotal-1 {
			t.Fatalf("operator view = %v total %d hidden %d", got.CameraIDs, got.MemberCountTotal, got.MemberCountHidden)
		}
		list, err := env.Svc.ListCameraGroups(ctx, operator)
		if err != nil {
			t.Fatal(err)
		}
		for _, lg := range list {
			if lg.ID == g.ID && lg.MemberCountHidden != got.MemberCountHidden {
				t.Fatalf("list hidden = %d, want %d", lg.MemberCountHidden, got.MemberCountHidden)
			}
		}
	})
}
