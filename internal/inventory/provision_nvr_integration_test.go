//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/provisionnvr/nvmstest"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

const nvrPass = "Recorder-Pass-7"

func nvrConn(t *testing.T, srv *nvmstest.Server, password string) inventory.NVRConnInput {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	return inventory.NVRConnInput{Host: u.Hostname(), Port: port, Username: "admin", Password: password}
}

func channelCameras(t *testing.T, env *demofix.Env, serverID uuid.UUID) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT remote_name, display_name FROM cameras WHERE server_id = $1 AND deleted_at IS NULL`, serverID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var remote, display string
			if err := rows.Scan(&remote, &display); err != nil {
				return err
			}
			out[remote] = display
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCreateProvisionNVRImportsChannelsAsCameras(t *testing.T) {
	env := demofix.Setup(t)
	site := env.Cameras["frigate-h01/plaza"].SiteID
	nvr := nvmstest.New(t, "admin", nvrPass)

	srv, err := env.Svc.CreateProvisionNVR(context.Background(), env.Admin, inventory.CreateNVRInput{SiteID: site, Name: "Feedlot NVR", Conn: nvrConn(t, nvr, nvrPass)})
	if err != nil {
		t.Fatal(err)
	}
	if srv.Kind != inventory.KindProvisionNVR || srv.Name != "Feedlot NVR" || srv.CameraCount != 7 {
		t.Fatalf("server = kind %q name %q cameras %d", srv.Kind, srv.Name, srv.CameraCount)
	}
	info := srv.Provision()
	if info == nil || info.Model != "NVR8-16400AN(1U)" || info.ChannelCapacity != 16 || len(info.Capabilities) == 0 {
		t.Fatalf("provision info = %+v", info)
	}
	cams := channelCameras(t, env, srv.ID)
	if len(cams) != 7 {
		t.Fatalf("cameras = %v", cams)
	}
	for remote, display := range cams {
		if display == "" || display == remote {
			t.Fatalf("camera %q display name = %q, want the channel's name", remote, display)
		}
	}
	// The password is sealed, never stored in the clear, and the Frigate flags stay off.
	if string(srv.PasswordSealed) == nvrPass || len(srv.PasswordSealed) == 0 {
		t.Fatal("password must be stored sealed")
	}
	if c := srv.Caps(); c.FaceRecognition || c.LPR || c.PTZ {
		t.Fatalf("Frigate capability flags must stay off for a recorder: %+v", c)
	}
}

func TestCreateProvisionNVRImportsOnlyTheChosenChannels(t *testing.T) {
	env := demofix.Setup(t)
	site := env.Cameras["frigate-h01/plaza"].SiteID
	nvr := nvmstest.New(t, "admin", nvrPass)
	det, err := env.Svc.DetectProvisionNvr(context.Background(), env.Admin, site, nvrConn(t, nvr, nvrPass))
	if err != nil {
		t.Fatal(err)
	}
	chosen := det.Channels[0].ID
	srv, err := env.Svc.CreateProvisionNVR(context.Background(), env.Admin, inventory.CreateNVRInput{SiteID: site, Name: "One channel", Conn: nvrConn(t, nvr, nvrPass), ChannelIDs: []string{chosen, chosen}})
	if err != nil {
		t.Fatal(err)
	}
	if cams := channelCameras(t, env, srv.ID); len(cams) != 1 || cams[chosen] == "" {
		t.Fatalf("cameras = %v, want only %s", cams, chosen)
	}
	_, err = env.Svc.CreateProvisionNVR(context.Background(), env.Admin, inventory.CreateNVRInput{SiteID: site, Name: "Bad", Conn: nvrConn(t, nvr, nvrPass), ChannelIDs: []string{"no-such-channel"}})
	var ve *inventory.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("an unknown channel must be a validation error, got %v", err)
	}
}

func TestCreateProvisionNVRRefusals(t *testing.T) {
	env := demofix.Setup(t)
	site := env.Cameras["frigate-h01/plaza"].SiteID
	nvr := nvmstest.New(t, "admin", nvrPass)
	operator := env.Actor(t, "operador")

	if _, err := env.Svc.CreateProvisionNVR(context.Background(), operator, inventory.CreateNVRInput{SiteID: site, Name: "x", Conn: nvrConn(t, nvr, nvrPass)}); err == nil {
		t.Fatal("a user without servers.manage must be refused")
	}
	if len(nvr.Calls()) != 0 {
		t.Fatalf("a refused request reached the recorder: %v", nvr.Calls())
	}
	if _, err := env.Svc.CreateProvisionNVR(context.Background(), env.Admin, inventory.CreateNVRInput{SiteID: site, Name: "x", Conn: nvrConn(t, nvr, "wrong-password")}); err == nil {
		t.Fatal("a wrong password must fail")
	}
	if nvr.FailedLogins() != 1 {
		t.Fatalf("failed logins = %d, want 1", nvr.FailedLogins())
	}
	if _, err := env.Svc.CreateProvisionNVR(context.Background(), env.Admin, inventory.CreateNVRInput{SiteID: site, Name: "first", Conn: nvrConn(t, nvr, nvrPass)}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Svc.CreateProvisionNVR(context.Background(), env.Admin, inventory.CreateNVRInput{SiteID: site, Name: "same address", Conn: nvrConn(t, nvr, nvrPass)}); !errors.Is(err, inventory.ErrDuplicateServer) {
		t.Fatalf("the same recorder twice must be a duplicate, got %v", err)
	}
}

func TestSyncProvisionNVRRefreshesAndMarksMissing(t *testing.T) {
	env := demofix.Setup(t)
	site := env.Cameras["frigate-h01/plaza"].SiteID
	nvr := nvmstest.New(t, "admin", nvrPass)
	srv, err := env.Svc.CreateProvisionNVR(context.Background(), env.Admin, inventory.CreateNVRInput{SiteID: site, Name: "Feedlot NVR", Conn: nvrConn(t, nvr, nvrPass)})
	if err != nil {
		t.Fatal(err)
	}

	res, err := env.Svc.SyncServer(context.Background(), env.Admin, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated != 7 || res.Added != 0 || res.Missing != 0 {
		t.Fatalf("sync = updated %d added %d missing %d, want 7/0/0", res.Updated, res.Added, res.Missing)
	}

	// One channel disappears from the recorder.
	list, err := nvmstest.Fixture("queryNodeList")
	if err != nil {
		t.Fatal(err)
	}
	nvr.Override["queryNodeList"] = dropFirstItem(list)
	res, err = env.Svc.SyncServer(context.Background(), env.Admin, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Missing != 1 || res.Updated != 6 {
		t.Fatalf("sync after a channel was removed = updated %d missing %d, want 6/1", res.Updated, res.Missing)
	}

	// Nothing in a sync writes to the recorder.
	for _, cmd := range nvr.Calls() {
		if cmd != "reqLogin" && cmd != "doLogin" && cmd != "doLogout" && len(cmd) >= 5 && cmd[:5] != "query" {
			t.Fatalf("sync sent %q to the recorder", cmd)
		}
	}
}

func TestSyncProvisionNVRReportsAnUnreachableRecorder(t *testing.T) {
	env := demofix.Setup(t)
	site := env.Cameras["frigate-h01/plaza"].SiteID
	nvr := nvmstest.New(t, "admin", nvrPass)
	srv, err := env.Svc.CreateProvisionNVR(context.Background(), env.Admin, inventory.CreateNVRInput{SiteID: site, Name: "Feedlot NVR", Conn: nvrConn(t, nvr, nvrPass)})
	if err != nil {
		t.Fatal(err)
	}
	nvr.Close()
	_, err = env.Svc.SyncServer(context.Background(), env.Admin, srv.ID)
	var ne *inventory.NVRError
	if !errors.As(err, &ne) {
		t.Fatalf("sync of a dead recorder = %v, want an NVRError", err)
	}
	if got := channelCameras(t, env, srv.ID); len(got) != 7 {
		t.Fatalf("a failed sync must not touch the cameras, got %d", len(got))
	}
}

func TestRemoveAndRestoreAProvisionChannel(t *testing.T) {
	env := demofix.Setup(t)
	site := env.Cameras["frigate-h01/plaza"].SiteID
	nvr := nvmstest.New(t, "admin", nvrPass)
	srv, err := env.Svc.CreateProvisionNVR(context.Background(), env.Admin, inventory.CreateNVRInput{SiteID: site, Name: "Feedlot NVR", Conn: nvrConn(t, nvr, nvrPass)})
	if err != nil {
		t.Fatal(err)
	}
	var camID uuid.UUID
	var remote string
	err = env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT id, remote_name FROM cameras WHERE server_id = $1 AND deleted_at IS NULL ORDER BY remote_name LIMIT 1`, srv.ID).Scan(&camID, &remote)
	})
	if err != nil {
		t.Fatal(err)
	}
	callsBefore := len(nvr.Calls())

	if err := env.Svc.RemoveCamera(context.Background(), env.Admin, camID); err != nil {
		t.Fatalf("removing a channel camera: %v", err)
	}
	if got := len(nvr.Calls()); got != callsBefore {
		t.Fatalf("removing a channel sent %d requests to the recorder; it must stay untouched", got-callsBefore)
	}
	if got := channelCameras(t, env, srv.ID); len(got) != 6 || got[remote] != "" {
		t.Fatalf("cameras after removal = %v", got)
	}
	deleted, err := env.Svc.ListDeletedCameras(context.Background(), env.Admin, srv.ID)
	if err != nil || len(deleted) != 1 || deleted[0].HasFrigateConfig || deleted[0].RetainDays != nil {
		t.Fatalf("deleted cameras = %+v, %v; want one without a Frigate block and no recording window", deleted, err)
	}

	// A sync must not bring it back.
	if _, err := env.Svc.SyncServer(context.Background(), env.Admin, srv.ID); err != nil {
		t.Fatal(err)
	}
	if got := channelCameras(t, env, srv.ID); len(got) != 6 {
		t.Fatalf("sync resurrected a removed channel: %v", got)
	}

	if _, err := env.Svc.ReinstateCamera(context.Background(), env.Admin, camID); err != nil {
		t.Fatalf("restoring a channel camera: %v", err)
	}
	if got := channelCameras(t, env, srv.ID); len(got) != 7 {
		t.Fatalf("cameras after restore = %v", got)
	}
}
