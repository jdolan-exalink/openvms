//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func TestCameraFolders(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	operator := env.Actor(t, "operador")

	plaza := env.Cameras["frigate-h01/plaza"]
	norte := env.Cameras["frigate-h01/acceso_norte"]
	muelle := env.Cameras["frigate-c01/muelle"]

	folder, err := env.Svc.CreateCameraFolder(ctx, env.Admin, plaza.ServerID, "Accesos")
	if err != nil {
		t.Fatal(err)
	}
	other, err := env.Svc.CreateCameraFolder(ctx, env.Admin, muelle.ServerID, "Puertos")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a user without cameras.manage cannot manage folders", func(t *testing.T) {
		if _, err := env.Svc.CreateCameraFolder(ctx, operator, norte.ServerID, "Mia"); !errors.Is(err, access.ErrForbidden) {
			t.Fatalf("create = %v, want forbidden", err)
		}
		place := []inventory.CameraPlacement{{CameraID: norte.ID, FolderID: &folder.ID}}
		if err := env.Svc.ReorderCameraFolders(ctx, operator, place, nil); !errors.Is(err, access.ErrForbidden) {
			t.Fatalf("reorder = %v, want forbidden", err)
		}
		if err := env.Svc.DeleteCameraFolder(ctx, operator, folder.ID); !errors.Is(err, access.ErrForbidden) {
			t.Fatalf("delete = %v, want forbidden", err)
		}
	})

	t.Run("cameras only move within their own server", func(t *testing.T) {
		var ve *inventory.ValidationError
		cross := []inventory.CameraPlacement{{CameraID: muelle.ID, FolderID: &folder.ID}}
		if err := env.Svc.ReorderCameraFolders(ctx, env.Admin, cross, nil); !errors.As(err, &ve) {
			t.Fatalf("cross-server move = %v, want ValidationError", err)
		}
		// The whole batch is rejected: the valid first item must not stick.
		batch := []inventory.CameraPlacement{{CameraID: plaza.ID, FolderID: &folder.ID, SortOrder: 1}, cross[0]}
		if err := env.Svc.ReorderCameraFolders(ctx, env.Admin, batch, nil); !errors.As(err, &ve) {
			t.Fatalf("mixed batch = %v, want ValidationError", err)
		}
		got, err := env.Svc.GetCamera(ctx, env.Admin, plaza.ID)
		if err != nil || got.FolderID != nil {
			t.Fatalf("plaza after rejected batch: folder=%v err=%v", got.FolderID, err)
		}
		ok := []inventory.CameraPlacement{{CameraID: plaza.ID, FolderID: &folder.ID, SortOrder: 3}}
		if err := env.Svc.ReorderCameraFolders(ctx, env.Admin, ok, []inventory.FolderPosition{{FolderID: folder.ID, SortOrder: 2}}); err != nil {
			t.Fatal(err)
		}
		got, _ = env.Svc.GetCamera(ctx, env.Admin, plaza.ID)
		if got.FolderID == nil || *got.FolderID != folder.ID || got.SortOrder != 3 {
			t.Fatalf("plaza = folder %v order %d", got.FolderID, got.SortOrder)
		}
	})

	t.Run("listing hides folders the caller has no camera in", func(t *testing.T) {
		res, err := env.Svc.ListCameraFolders(ctx, operator)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Folders) != 0 || len(res.ManageableServerIDs) != 0 {
			t.Fatalf("operator sees %d folders, manages %d servers", len(res.Folders), len(res.ManageableServerIDs))
		}
		all, err := env.Svc.ListCameraFolders(ctx, env.Admin)
		if err != nil || len(all.Folders) != 2 {
			t.Fatalf("admin folders = %d (%v)", len(all.Folders), err)
		}
		_ = other
	})

	t.Run("deleting a folder returns its cameras to the server root", func(t *testing.T) {
		if err := env.Svc.DeleteCameraFolder(ctx, env.Admin, folder.ID); err != nil {
			t.Fatal(err)
		}
		got, _ := env.Svc.GetCamera(ctx, env.Admin, plaza.ID)
		if got.FolderID != nil {
			t.Fatalf("plaza still in folder %v", got.FolderID)
		}
	})

	t.Run("hard deleting a server removes its folders", func(t *testing.T) {
		if err := env.Svc.DeleteServer(ctx, env.Admin, muelle.ServerID); err != nil {
			t.Fatal(err)
		}
		res, err := env.Svc.ListCameraFolders(ctx, env.Admin)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range res.Folders {
			if f.ID == other.ID {
				t.Fatal("folder of the deleted server survived")
			}
		}
	})
}
