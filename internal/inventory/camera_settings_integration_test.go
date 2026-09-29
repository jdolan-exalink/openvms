//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func strp(s string) *string { return &s }

func TestCameraVMSSettings(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]

	t.Run("new cameras default to sub with empty metadata", func(t *testing.T) {
		if cam.DefaultLiveQuality != "sub" || cam.Description != "" || cam.Location != "" || len(cam.Tags) != 0 {
			t.Fatalf("defaults = %q %q %q %v", cam.DefaultLiveQuality, cam.Description, cam.Location, cam.Tags)
		}
	})

	t.Run("operator without cameras.manage cannot edit", func(t *testing.T) {
		operator := env.Actor(t, "operador")
		_, err := env.Svc.UpdateCamera(ctx, operator, cam.ID, inventory.CameraUpdate{Location: strp("x")})
		if !errors.Is(err, access.ErrForbidden) {
			t.Errorf("UpdateCamera = %v, want forbidden", err)
		}
	})

	t.Run("invalid input is rejected and nothing changes", func(t *testing.T) {
		if _, err := env.Svc.UpdateCamera(ctx, env.Admin, cam.ID, inventory.CameraUpdate{DefaultLiveQuality: strp("hd")}); err == nil {
			t.Fatal("want validation error")
		}
		got, err := env.Svc.GetCamera(ctx, env.Admin, cam.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.DefaultLiveQuality != "sub" {
			t.Errorf("quality = %q", got.DefaultLiveQuality)
		}
	})

	tags := []string{"acceso", "exterior"}
	t.Run("update persists, is partial and is audited", func(t *testing.T) {
		got, err := env.Svc.UpdateCamera(ctx, env.Admin, cam.ID, inventory.CameraUpdate{
			DefaultLiveQuality: strp("main"), Description: strp("Entrada"), Location: strp("Planta baja"), Tags: &tags,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.DefaultLiveQuality != "main" || got.Description != "Entrada" || got.Location != "Planta baja" || !slices.Equal(got.Tags, tags) {
			t.Fatalf("got %+v", got)
		}
		// A later partial update leaves the other settings alone.
		got, err = env.Svc.UpdateCamera(ctx, env.Admin, cam.ID, inventory.CameraUpdate{Description: strp("Otra")})
		if err != nil {
			t.Fatal(err)
		}
		if got.DefaultLiveQuality != "main" || got.Description != "Otra" || !slices.Equal(got.Tags, tags) {
			t.Fatalf("partial update clobbered fields: %+v", got)
		}
		var n int
		err = env.Pool.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE action = 'CAMERA_UPDATED' AND target_id = $1 AND details ? 'default_live_quality'`,
			cam.ID).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Error("camera settings change not audited")
		}
	})

	t.Run("inventory sync does not overwrite VMS-owned settings", func(t *testing.T) {
		srv := cam.ServerID
		if _, err := env.Svc.SyncServer(ctx, env.Admin, srv); err != nil {
			t.Fatal(err)
		}
		got, err := env.Svc.GetCamera(ctx, env.Admin, cam.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.DefaultLiveQuality != "main" || got.Location != "Planta baja" || !slices.Equal(got.Tags, tags) || got.Description != "Otra" {
			t.Errorf("sync overwrote settings: %+v", got)
		}
	})
}
