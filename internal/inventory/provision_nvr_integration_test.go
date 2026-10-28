//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

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

func newPolledNVR(t *testing.T) (*demofix.Env, *nvmstest.Server, inventory.ServerView, *inventory.HealthPoller) {
	t.Helper()
	env := demofix.Setup(t)
	site := env.Cameras["frigate-h01/plaza"].SiteID
	nvr := nvmstest.New(t, "admin", nvrPass)
	srv, err := env.Svc.CreateProvisionNVR(context.Background(), env.Admin, inventory.CreateNVRInput{SiteID: site, Name: "Feedlot NVR", Conn: nvrConn(t, nvr, nvrPass)})
	if err != nil {
		t.Fatal(err)
	}
	return env, nvr, srv, &inventory.HealthPoller{Svc: env.Svc, Interval: time.Second, Concurrency: 2}
}

func healthOf(t *testing.T, env *demofix.Env, id uuid.UUID) (status, lastError string, stats string) {
	t.Helper()
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT status, last_error, stats::text FROM frigate_servers WHERE id = $1`, id).Scan(&status, &lastError, &stats)
	})
	if err != nil {
		t.Fatal(err)
	}
	return status, lastError, stats
}

func cameraStatuses(t *testing.T, env *demofix.Env, serverID uuid.UUID) map[string]int {
	t.Helper()
	out := map[string]int{}
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT status, count(*)::int FROM cameras WHERE server_id = $1 AND deleted_at IS NULL GROUP BY status`, serverID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var st string
			var n int
			if err := rows.Scan(&st, &n); err != nil {
				return err
			}
			out[st] = n
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProvisionRecorderHealth(t *testing.T) {
	t.Run("a healthy recorder is online with its cameras online", func(t *testing.T) {
		env, _, srv, poller := newPolledNVR(t)
		poller.PollOnce(context.Background())
		if status, lastErr, _ := healthOf(t, env, srv.ID); status != "online" || lastErr != "" {
			t.Fatalf("health = %q %q, want online", status, lastErr)
		}
		if got := cameraStatuses(t, env, srv.ID); got["online"] != 7 {
			t.Fatalf("camera statuses = %v, want 7 online", got)
		}
	})

	t.Run("an offline channel degrades the recorder and is named", func(t *testing.T) {
		env, nvr, srv, poller := newPolledNVR(t)
		chl, _ := nvmstest.Fixture("queryChlStatus")
		nvr.Override["queryChlStatus"] = strings.Replace(chl, "<online>true</online>", "<online>false</online>", 1)
		poller.PollOnce(context.Background())
		status, _, stats := healthOf(t, env, srv.ID)
		if status != "degraded" || !strings.Contains(stats, "cameras_offline") {
			t.Fatalf("health = %q stats=%s, want degraded with cameras_offline", status, stats)
		}
		if got := cameraStatuses(t, env, srv.ID); got["online"] != 6 || got["offline"] != 1 {
			t.Fatalf("camera statuses = %v, want 6 online and 1 offline", got)
		}
	})

	t.Run("a bad disk degrades the recorder", func(t *testing.T) {
		env, nvr, srv, poller := newPolledNVR(t)
		disk, _ := nvmstest.Fixture("queryDiskStatus")
		nvr.Override["queryDiskStatus"] = strings.Replace(disk, "<diskStatus>read/write</diskStatus>", "<diskStatus>bad</diskStatus>", 1)
		poller.PollOnce(context.Background())
		if status, _, stats := healthOf(t, env, srv.ID); status != "degraded" || !strings.Contains(stats, "recorder_disk_bad") {
			t.Fatalf("health = %q stats=%s, want degraded with recorder_disk_bad", status, stats)
		}
	})

	t.Run("an unreachable recorder is offline and its cameras unknown", func(t *testing.T) {
		env, nvr, srv, poller := newPolledNVR(t)
		poller.PollOnce(context.Background())
		nvr.Close()
		poller.PollOnce(context.Background())
		status, lastErr, _ := healthOf(t, env, srv.ID)
		if status != "offline" || !strings.HasPrefix(lastErr, "offline:") {
			t.Fatalf("health = %q %q, want offline", status, lastErr)
		}
		if got := cameraStatuses(t, env, srv.ID); got["unknown"] != 7 {
			t.Fatalf("camera statuses = %v, want 7 unknown", got)
		}
	})

	t.Run("a rejected password is reported once and never retried", func(t *testing.T) {
		env, nvr, srv, poller := newPolledNVR(t)
		poller.PollOnce(context.Background())
		nvr.SetPassword("changed-on-the-device")
		// The cached session still holds a valid token until the recorder drops it; force a new login.
		nvr.ExpireSession()
		for range 4 {
			poller.PollOnce(context.Background())
		}
		status, lastErr, _ := healthOf(t, env, srv.ID)
		if status != "offline" || !strings.HasPrefix(lastErr, "auth_failed:") {
			t.Fatalf("health = %q %q, want offline with auth_failed", status, lastErr)
		}
		if strings.Contains(lastErr, nvrPass) {
			t.Fatal("last_error must not carry the password")
		}
		if nvr.FailedLogins() != 1 {
			t.Fatalf("failed logins = %d, want 1: the poller must stop sending a rejected password", nvr.FailedLogins())
		}
	})
}

func addChannel(xml, id, name string) string {
	start := strings.Index(xml, "<item ")
	end := strings.Index(xml, "</item>")
	item := xml[start : end+len("</item>")]
	item = regexp.MustCompile(`<item id="[^"]*"`).ReplaceAllString(item, `<item id="{`+id+`}"`)
	item = regexp.MustCompile(`<name>[^<]*</name>`).ReplaceAllString(item, "<name>"+name+"</name>")
	item = regexp.MustCompile(`<devID>[^<]*</devID>`).ReplaceAllString(item, "<devID>{"+id+"}</devID>")
	return xml[:end+len("</item>")] + "\n" + item + xml[end+len("</item>"):]
}

func TestChannelsCanBeChosenAndAddedLater(t *testing.T) {
	env := demofix.Setup(t)
	site := env.Cameras["frigate-h01/plaza"].SiteID
	nvr := nvmstest.New(t, "admin", nvrPass)
	det, err := env.Svc.DetectProvisionNvr(context.Background(), env.Admin, site, nvrConn(t, nvr, nvrPass))
	if err != nil {
		t.Fatal(err)
	}
	first, second := det.Channels[0].ID, det.Channels[1].ID
	srv, err := env.Svc.CreateProvisionNVR(context.Background(), env.Admin, inventory.CreateNVRInput{SiteID: site, Name: "Some channels", Conn: nvrConn(t, nvr, nvrPass), ChannelIDs: []string{first, second}})
	if err != nil {
		t.Fatal(err)
	}

	list, err := env.Svc.ListProvisionNVRChannels(context.Background(), env.Admin, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	imported := 0
	for _, c := range list {
		if c.Imported {
			imported++
		}
	}
	if len(list) != 7 || imported != 2 {
		t.Fatalf("list = %d channels, %d imported; want 7 and 2", len(list), imported)
	}

	// Importing everything adds only the five that are missing; the two already there are left alone.
	all := make([]string, 0, len(list))
	for _, c := range list {
		all = append(all, c.ID)
	}
	added, skipped, err := env.Svc.ImportProvisionNVRChannels(context.Background(), env.Admin, srv.ID, all)
	if err != nil || added != 5 || skipped != 0 {
		t.Fatalf("import all = added %d skipped %d err %v, want 5/0", added, skipped, err)
	}
	if got := channelCameras(t, env, srv.ID); len(got) != 7 {
		t.Fatalf("cameras = %d, want 7", len(got))
	}

	// A new channel appears on the recorder: the list shows it as not imported and it can be added.
	nodes, _ := nvmstest.Fixture("queryNodeList")
	nvr.Override["queryNodeList"] = addChannel(nodes, "00000063-0000-0000-0000-000000000000", "Cámara nueva")
	list, err = env.Svc.ListProvisionNVRChannels(context.Background(), env.Admin, srv.ID)
	if err != nil || len(list) != 8 {
		t.Fatalf("list after a new channel = %d, %v; want 8", len(list), err)
	}
	var fresh string
	for _, c := range list {
		if !c.Imported {
			fresh = c.ID
		}
	}
	if fresh != "00000063-0000-0000-0000-000000000000" {
		t.Fatalf("the new channel must be the only one not imported, got %q", fresh)
	}
	added, _, err = env.Svc.ImportProvisionNVRChannels(context.Background(), env.Admin, srv.ID, []string{fresh})
	if err != nil || added != 1 {
		t.Fatalf("import the new channel = added %d err %v, want 1", added, err)
	}

	// A channel deleted here is skipped, not resurrected.
	var camID uuid.UUID
	err = env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT id FROM cameras WHERE server_id = $1 AND remote_name = $2`, srv.ID, first).Scan(&camID)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Svc.RemoveCamera(context.Background(), env.Admin, camID); err != nil {
		t.Fatal(err)
	}
	added, skipped, err = env.Svc.ImportProvisionNVRChannels(context.Background(), env.Admin, srv.ID, []string{first})
	if err != nil || added != 0 || skipped != 1 {
		t.Fatalf("import a deleted channel = added %d skipped %d err %v, want 0/1", added, skipped, err)
	}

	// Refusals.
	var ve *inventory.ValidationError
	if _, _, err := env.Svc.ImportProvisionNVRChannels(context.Background(), env.Admin, srv.ID, []string{"no-such-channel"}); !errors.As(err, &ve) {
		t.Fatalf("an unknown channel must be a validation error, got %v", err)
	}
	if _, _, err := env.Svc.ImportProvisionNVRChannels(context.Background(), env.Admin, srv.ID, nil); !errors.As(err, &ve) {
		t.Fatalf("an empty selection must be a validation error, got %v", err)
	}
	if _, err := env.Svc.ListProvisionNVRChannels(context.Background(), env.Actor(t, "operador"), srv.ID); err == nil {
		t.Fatal("a user without servers.manage must be refused")
	}
	frigateServer := env.Cameras["frigate-h01/plaza"].ServerID
	var uke *inventory.UnsupportedKindError
	if _, err := env.Svc.ListProvisionNVRChannels(context.Background(), env.Admin, frigateServer); !errors.As(err, &uke) {
		t.Fatalf("a Frigate server has no recorder channels, got %v", err)
	}
}
