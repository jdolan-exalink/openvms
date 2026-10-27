//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// frigateCamera returns a camera's block of a config.yml, and whether the config has it.
func frigateCamera(t *testing.T, yamlText, name string) (map[string]any, bool) {
	t.Helper()
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(yamlText), &cfg); err != nil {
		t.Fatal(err)
	}
	cams, _ := cfg["cameras"].(map[string]any)
	block, ok := cams[name].(map[string]any)
	return block, ok
}

func firstInputPath(t *testing.T, block map[string]any) string {
	t.Helper()
	ff, _ := block["ffmpeg"].(map[string]any)
	inputs, _ := ff["inputs"].([]any)
	if len(inputs) == 0 {
		t.Fatalf("camera block has no ffmpeg inputs: %v", block)
	}
	first, _ := inputs[0].(map[string]any)
	path, _ := first["path"].(string)
	return path
}

func TestRemoveCameraTakesItOutOfFrigateAndKeepsWhatRestoreNeeds(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mock := env.Mocks["frigate-h01"].Server
	if _, ok := frigateCamera(t, mock.ConfigYAML(), cam.RemoteName); !ok {
		t.Fatalf("the mock Frigate has no camera %q", cam.RemoteName)
	}

	if err := env.Svc.RemoveCamera(ctx, env.Admin, cam.ID); err != nil {
		t.Fatal(err)
	}

	if _, still := frigateCamera(t, mock.ConfigYAML(), cam.RemoteName); still {
		t.Fatal("the camera is still in Frigate's config.yml after the removal")
	}
	if _, other := frigateCamera(t, mock.ConfigYAML(), "acceso_norte"); !other {
		t.Fatal("removing one camera must leave the others in Frigate's config")
	}
	st := deletedStateOf(t, env, cam.ID)
	if !st.deletedAt || st.tombstones != 1 || st.retainDays == nil || *st.retainDays != 7 {
		t.Fatalf("state after remove = %+v, want deleted with a tombstone holding the camera's 7 recording days", st)
	}
	deleted, err := env.Svc.ListDeletedCameras(ctx, env.Admin, cam.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].ID != cam.ID || !deleted[0].HasFrigateConfig {
		t.Fatalf("deleted cameras = %+v, want the removed one with its Frigate block kept", deleted)
	}
	if got := len(mock.SaveCalls()); got != 1 {
		t.Fatalf("Frigate config saves = %d, want exactly 1", got)
	}
}

func TestReinstateCameraPutsItsBlockBackAndKeepsTheId(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mock := env.Mocks["frigate-h01"].Server
	original, _ := frigateCamera(t, mock.ConfigYAML(), cam.RemoteName)
	if err := env.Svc.RemoveCamera(ctx, env.Admin, cam.ID); err != nil {
		t.Fatal(err)
	}

	restored, err := env.Svc.ReinstateCamera(ctx, env.Admin, cam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID != cam.ID {
		t.Fatalf("restored camera id = %s, want the original %s", restored.ID, cam.ID)
	}
	back, ok := frigateCamera(t, mock.ConfigYAML(), cam.RemoteName)
	if !ok {
		t.Fatal("the camera is not back in Frigate's config.yml")
	}
	if got, want := firstInputPath(t, back), firstInputPath(t, original); got != want {
		t.Fatalf("restored input path = %q, want the original %q", got, want)
	}
	if st := deletedStateOf(t, env, cam.ID); st.deletedAt || st.tombstones != 0 {
		t.Fatalf("state after reinstate = %+v, want live with no tombstone", st)
	}
	res, err := env.Svc.SyncServer(ctx, env.Admin, cam.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 || res.Skipped != 0 {
		t.Fatalf("sync after reinstate: added=%d skipped=%d, want 0 and 0", res.Added, res.Skipped)
	}
}

func TestRemoveCameraFailsClosedWhenFrigateCannotBeReached(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	env.Mocks["frigate-h01"].Server.Offline.Store(true)

	err := env.Svc.RemoveCamera(ctx, env.Admin, cam.ID)
	var fe *inventory.FrigateError
	if !errors.As(err, &fe) {
		t.Fatalf("remove with Frigate offline = %v, want a Frigate error", err)
	}
	if st := deletedStateOf(t, env, cam.ID); st.deletedAt || st.tombstones != 0 {
		t.Fatalf("a failed removal changed the camera: %+v", st)
	}
	if got := auditCount(t, env, "CAMERA_REMOVED", cam.ID); got != 0 {
		t.Fatalf("CAMERA_REMOVED audit entries = %d, want 0", got)
	}
}

func TestRemoveCameraAlreadyAbsentFromFrigateStillDeletesIt(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mock := env.Mocks["frigate-h01"].Server
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(mock.ConfigYAML()), &cfg); err != nil {
		t.Fatal(err)
	}
	delete(cfg["cameras"].(map[string]any), cam.RemoteName)
	without, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Svc.SaveServerFrigateRaw(ctx, env.Admin, cam.ServerID, string(without), true); err != nil {
		t.Fatal(err)
	}
	savesBefore := len(mock.SaveCalls())

	if err := env.Svc.RemoveCamera(ctx, env.Admin, cam.ID); err != nil {
		t.Fatal(err)
	}
	if got := len(mock.SaveCalls()); got != savesBefore {
		t.Fatalf("Frigate config saves = %d, want %d (nothing to remove)", got, savesBefore)
	}
	st := deletedStateOf(t, env, cam.ID)
	if !st.deletedAt || st.tombstones != 1 || st.retainDays != nil {
		t.Fatalf("state = %+v, want deleted with a tombstone and no known recording days", st)
	}
	deleted, err := env.Svc.ListDeletedCameras(ctx, env.Admin, cam.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].HasFrigateConfig {
		t.Fatalf("deleted cameras = %+v, want one without a kept Frigate block", deleted)
	}
	// Nothing to put back: the restore only brings the camera back in OpenVMS and writes no config.
	if _, err := env.Svc.ReinstateCamera(ctx, env.Admin, cam.ID); err != nil {
		t.Fatal(err)
	}
	if got := len(mock.SaveCalls()); got != savesBefore {
		t.Fatalf("Frigate config saves after reinstate = %d, want %d", got, savesBefore)
	}
}

func TestReinstateCameraRefusesAConflictBeforeTouchingFrigate(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mock := env.Mocks["frigate-h01"].Server
	if err := env.Svc.RemoveCamera(ctx, env.Admin, cam.ID); err != nil {
		t.Fatal(err)
	}
	insertLiveCamera(t, env, cam.TenantID, cam.SiteID, cam.ServerID, cam.RemoteName)
	saves := len(mock.SaveCalls())

	if _, err := env.Svc.ReinstateCamera(ctx, env.Admin, cam.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("reinstate over a live camera of the same name = %v, want conflict", err)
	}
	if got := len(mock.SaveCalls()); got != saves {
		t.Fatalf("Frigate config saves = %d, want %d: a refused restore must not edit Frigate", got, saves)
	}
	if _, back := frigateCamera(t, mock.ConfigYAML(), cam.RemoteName); back {
		t.Fatal("a refused restore put the camera back into Frigate's config")
	}
}

func TestRemoveAndReinstateNeedServersManage(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mock := env.Mocks["frigate-h01"].Server
	operator := env.Actor(t, "operador")

	if err := env.Svc.RemoveCamera(ctx, operator, cam.ID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("remove by an operator without servers.manage = %v, want forbidden", err)
	}
	if got := len(mock.SaveCalls()); got != 0 {
		t.Fatalf("a refused removal saved Frigate's config %d times", got)
	}
	if err := env.Svc.RemoveCamera(ctx, env.Admin, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("remove of an unknown camera = %v, want not found", err)
	}
	if err := env.Svc.RemoveCamera(ctx, env.Admin, cam.ID); err != nil {
		t.Fatal(err)
	}
	saves := len(mock.SaveCalls())
	if _, err := env.Svc.ReinstateCamera(ctx, operator, cam.ID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("reinstate by an operator without servers.manage = %v, want forbidden", err)
	}
	if _, err := env.Svc.ListDeletedCameras(ctx, operator, cam.ServerID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("list deleted cameras by an operator = %v, want forbidden", err)
	}
	if got := len(mock.SaveCalls()); got != saves {
		t.Fatalf("a refused reinstate saved Frigate's config (%d saves, want %d)", got, saves)
	}
}

func TestAddCameraToFrigateRefusesADeletedCamerasName(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mock := env.Mocks["frigate-h01"].Server
	if err := env.Svc.RemoveCamera(ctx, env.Admin, cam.ID); err != nil {
		t.Fatal(err)
	}
	saves := len(mock.SaveCalls())

	_, err := env.Svc.AddCameraToFrigate(ctx, env.Admin, cam.ServerID, inventory.AddCameraToFrigateInput{Name: cam.RemoteName, MainStreamURL: "rtsp://10.0.0.50/plaza"})
	var ve *inventory.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("add with a deleted camera's name = %v, want a validation error pointing to restore", err)
	}
	if got := len(mock.SaveCalls()); got != saves {
		t.Fatalf("a refused add saved Frigate's config (%d saves, want %d)", got, saves)
	}
}

// A removal that saved Frigate's config and then failed before deleting the camera here is retried:
// the retry must keep what the first attempt saved, not record a camera that cannot be restored.
func TestRemoveCameraRetryAfterAPartialFailureKeepsTheRestoreData(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	if err := env.Svc.RemoveCamera(ctx, env.Admin, cam.ID); err != nil {
		t.Fatal(err)
	}
	// Undo only the OpenVMS side, as if the delete transaction had failed after Frigate was saved.
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM camera_tombstones WHERE camera_id = $1`, cam.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE cameras SET deleted_at = NULL, deleted_by = NULL WHERE id = $1`, cam.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := env.Svc.RemoveCamera(ctx, env.Admin, cam.ID); err != nil {
		t.Fatal(err)
	}
	st := deletedStateOf(t, env, cam.ID)
	if st.tombstones != 1 || st.retainDays == nil || *st.retainDays != 7 {
		t.Fatalf("tombstone after the retry = %+v, want the 7 recording days the first attempt saw", st)
	}
	deleted, err := env.Svc.ListDeletedCameras(ctx, env.Admin, cam.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || !deleted[0].HasFrigateConfig {
		t.Fatalf("deleted cameras = %+v, want the Frigate block still available to restore", deleted)
	}
	if _, err := env.Svc.ReinstateCamera(ctx, env.Admin, cam.ID); err != nil {
		t.Fatalf("restore after the retried removal = %v, want it to put the camera back", err)
	}
	if _, back := frigateCamera(t, env.Mocks["frigate-h01"].Server.ConfigYAML(), cam.RemoteName); !back {
		t.Fatal("the camera is not back in Frigate's config after the restore")
	}
}

// If the kept Frigate revision is ever removed, restoring refuses instead of bringing the camera
// back in OpenVMS only, where it would not record.
func TestReinstateCameraRefusesWhenItsKeptFrigateBlockWasLost(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mock := env.Mocks["frigate-h01"].Server
	if err := env.Svc.RemoveCamera(ctx, env.Admin, cam.ID); err != nil {
		t.Fatal(err)
	}
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM frigate_config_revisions WHERE id = (SELECT config_revision_id FROM camera_tombstones WHERE camera_id = $1)`, cam.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	saves := len(mock.SaveCalls())

	_, err = env.Svc.ReinstateCamera(ctx, env.Admin, cam.ID)
	var ve *inventory.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("reinstate with the kept block lost = %v, want a validation error", err)
	}
	if st := deletedStateOf(t, env, cam.ID); !st.deletedAt || st.tombstones != 1 {
		t.Fatalf("a refused restore changed the camera: %+v", st)
	}
	if got := len(mock.SaveCalls()); got != saves {
		t.Fatalf("a refused restore saved Frigate's config (%d saves, want %d)", got, saves)
	}
}
