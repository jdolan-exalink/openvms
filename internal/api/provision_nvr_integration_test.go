//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/provisionnvr/nvmstest"
)

const detectPath = "/api/v1/servers/provision-nvr/detect"

func nvrBody(t *testing.T, siteID, nvrURL, password string) map[string]any {
	t.Helper()
	u, err := url.Parse(nvrURL)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	return map[string]any{"site_id": siteID, "host": u.Hostname(), "port": port, "username": "admin", "password": password}
}

func TestDetectProvisionNvrOverHTTP(t *testing.T) {
	te := setupMapsTest(t, false)
	site := te.Cameras["frigate-h01/plaza"].SiteID.String()
	const pass = "Correct-Horse-9"
	nvr := nvmstest.New(t, "admin", pass)
	operator := te.Demo.Tokens["operador"]

	t.Run("a user without servers.manage is refused and the recorder is never contacted", func(t *testing.T) {
		status, _, body := te.request(http.MethodPost, detectPath, operator, nil, nvrBody(t, site, nvr.URL, pass))
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403: %s", status, body)
		}
		if got := len(nvr.Calls()); got != 0 {
			t.Fatalf("a refused detect reached the recorder %d times", got)
		}
	})

	t.Run("an unknown site is 404", func(t *testing.T) {
		status, _, body := te.request(http.MethodPost, detectPath, te.AdminToken, nil, nvrBody(t, "00000000-0000-0000-0000-000000000001", nvr.URL, pass))
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", status, body)
		}
	})

	t.Run("a host with a scheme or a port is 400", func(t *testing.T) {
		for _, host := range []string{"http://192.0.2.1", "192.0.2.1:80", "", "a b"} {
			b := nvrBody(t, site, nvr.URL, pass)
			b["host"] = host
			if status, _, body := te.request(http.MethodPost, detectPath, te.AdminToken, nil, b); status != http.StatusBadRequest {
				t.Fatalf("host %q: status = %d, want 400: %s", host, status, body)
			}
		}
	})

	t.Run("detect returns the matrix and keeps the secrets out", func(t *testing.T) {
		status, _, body := te.request(http.MethodPost, detectPath, te.AdminToken, nil, nvrBody(t, site, nvr.URL, pass))
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", status, body)
		}
		var det gen.ProvisionNvrDetection
		if err := json.Unmarshal(body, &det); err != nil {
			t.Fatal(err)
		}
		if det.Device.Model != "NVR8-16400AN(1U)" || len(det.Channels) != 7 || len(det.Disks) != 1 || det.ChannelCapacity != 16 {
			t.Fatalf("detection = %+v", det)
		}
		states := map[string]gen.ProvisionNvrCapabilityState{}
		for _, c := range det.Capabilities {
			states[c.Name] = c.State
		}
		if states["channels"] != gen.ProvisionNvrCapabilityStateConfirmed || states["onvif"] != gen.ProvisionNvrCapabilityStateUnsupported || states["ptz"] != gen.ProvisionNvrCapabilityStateUnknown {
			t.Fatalf("capability states = %v", states)
		}
		for _, secret := range []string{pass, "SERIAL", "DIGEST", "test-session-token"} {
			if strings.Contains(string(body), secret) {
				t.Fatalf("response carries %q", secret)
			}
		}
	})

	t.Run("a rejected password is a 502 nvr_auth_failed, not a 401", func(t *testing.T) {
		before := nvr.FailedLogins()
		status, _, body := te.request(http.MethodPost, detectPath, te.AdminToken, nil, nvrBody(t, site, nvr.URL, "Wrong-Password-1"))
		if status != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502: %s", status, body)
		}
		var e gen.Error
		_ = json.Unmarshal(body, &e)
		if e.Code != "nvr_auth_failed" {
			t.Fatalf("code = %q: %s", e.Code, body)
		}
		if strings.Contains(string(body), "Wrong-Password-1") || nvr.FailedLogins() != before+1 {
			t.Fatalf("password echoed or retried: failed logins %d -> %d", before, nvr.FailedLogins())
		}
	})

	t.Run("an unreachable recorder is a 502 nvr_unreachable", func(t *testing.T) {
		dead := nvmstest.New(t, "admin", pass)
		deadURL := dead.URL
		dead.Close()
		status, _, body := te.request(http.MethodPost, detectPath, te.AdminToken, nil, nvrBody(t, site, deadURL, pass))
		var e gen.Error
		_ = json.Unmarshal(body, &e)
		if status != http.StatusBadGateway || e.Code != "nvr_unreachable" {
			t.Fatalf("status = %d code = %q: %s", status, e.Code, body)
		}
	})

	t.Run("a device that is not an NVMS recorder is nvr_unsupported", func(t *testing.T) {
		other := nvmstest.New(t, "admin", pass)
		other.Override["queryActivationStatus"] = "<html>router</html>"
		status, _, body := te.request(http.MethodPost, detectPath, te.AdminToken, nil, nvrBody(t, site, other.URL, pass))
		var e gen.Error
		_ = json.Unmarshal(body, &e)
		if status != http.StatusBadGateway || e.Code != "nvr_unsupported" {
			t.Fatalf("status = %d code = %q: %s", status, e.Code, body)
		}
	})
}

func TestProvisionNvrServerLifecycleOverHTTP(t *testing.T) {
	te := setupMapsTest(t, false)
	site := te.Cameras["frigate-h01/plaza"].SiteID.String()
	const pass = "Lifecycle-Pass-3"
	nvr := nvmstest.New(t, "admin", pass)
	operator := te.Demo.Tokens["operador"]
	createPath := "/api/v1/servers/provision-nvr"

	body := nvrBody(t, site, nvr.URL, pass)
	body["name"] = "NVR Feedlot"

	if status, _, out := te.request(http.MethodPost, createPath, operator, nil, body); status != http.StatusForbidden {
		t.Fatalf("operator create = %d, want 403: %s", status, out)
	}
	if got := len(nvr.Calls()); got != 0 {
		t.Fatalf("a refused create reached the recorder %d times", got)
	}

	status, _, out := te.request(http.MethodPost, createPath, te.AdminToken, nil, body)
	if status != http.StatusCreated {
		t.Fatalf("create = %d, want 201: %s", status, out)
	}
	var srv gen.Server
	if err := json.Unmarshal(out, &srv); err != nil {
		t.Fatal(err)
	}
	if srv.Kind != gen.ServerKindProvisionNvr || srv.CameraCount != 7 || srv.Provision == nil || srv.Provision.Model != "NVR8-16400AN(1U)" {
		t.Fatalf("server = kind %q cameras %d provision %+v", srv.Kind, srv.CameraCount, srv.Provision)
	}
	for _, secret := range []string{pass, "SERIAL", "test-session-token"} {
		if strings.Contains(string(out), secret) {
			t.Fatalf("response carries %q", secret)
		}
	}
	idPath := "/api/v1/servers/" + srv.Id.String()

	t.Run("the same recorder twice is a 409", func(t *testing.T) {
		body["name"] = "Again"
		if status, _, out := te.request(http.MethodPost, createPath, te.AdminToken, nil, body); status != http.StatusConflict {
			t.Fatalf("duplicate = %d, want 409: %s", status, out)
		}
	})

	t.Run("sync re-reads the recorder", func(t *testing.T) {
		status, _, out := te.request(http.MethodPost, idPath+"/sync", te.AdminToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("sync = %d, want 200: %s", status, out)
		}
		var res gen.SyncServer200JSONResponse
		_ = json.Unmarshal(out, &res)
		if res.Updated != 7 || res.Added != 0 || res.Server.Kind != gen.ServerKindProvisionNvr {
			t.Fatalf("sync = %+v", res)
		}
	})

	t.Run("rename works and the connection cannot be edited", func(t *testing.T) {
		if status, _, out := te.request(http.MethodPatch, idPath, te.AdminToken, nil, map[string]any{"name": "NVR renamed"}); status != http.StatusOK {
			t.Fatalf("rename = %d, want 200: %s", status, out)
		}
		if status, _, out := te.request(http.MethodPatch, idPath, te.AdminToken, nil, map[string]any{"password": "another-password"}); status != http.StatusBadRequest {
			t.Fatalf("password change = %d, want 400: %s", status, out)
		}
	})

	t.Run("Frigate-only operations are refused with 409 and never reach the recorder", func(t *testing.T) {
		before := len(nvr.Calls())
		status, _, out := te.request(http.MethodPost, idPath+"/restart", te.AdminToken, nil, nil)
		var e gen.Error
		_ = json.Unmarshal(out, &e)
		if status != http.StatusConflict || e.Code != "unsupported_for_server_kind" {
			t.Fatalf("restart = %d %q, want 409 unsupported_for_server_kind: %s", status, e.Code, out)
		}
		if got := len(nvr.Calls()); got != before {
			t.Fatalf("restart sent %d requests to the recorder", got-before)
		}
	})
}

func TestProvisionNvrChannelRoutesOverHTTP(t *testing.T) {
	te := setupMapsTest(t, false)
	site := te.Cameras["frigate-h01/plaza"].SiteID.String()
	const pass = "Channels-Pass-5"
	nvr := nvmstest.New(t, "admin", pass)
	operator := te.Demo.Tokens["operador"]

	body := nvrBody(t, site, nvr.URL, pass)
	body["name"] = "NVR channels"
	status, _, out := te.request(http.MethodPost, "/api/v1/servers/provision-nvr/detect", te.AdminToken, nil, nvrBody(t, site, nvr.URL, pass))
	if status != http.StatusOK {
		t.Fatalf("detect = %d: %s", status, out)
	}
	var det gen.ProvisionNvrDetection
	if err := json.Unmarshal(out, &det); err != nil {
		t.Fatal(err)
	}
	body["channel_ids"] = []string{det.Channels[0].Id, det.Channels[1].Id}
	status, _, out = te.request(http.MethodPost, "/api/v1/servers/provision-nvr", te.AdminToken, nil, body)
	if status != http.StatusCreated {
		t.Fatalf("create with two channels = %d: %s", status, out)
	}
	var srv gen.Server
	_ = json.Unmarshal(out, &srv)
	if srv.CameraCount != 2 {
		t.Fatalf("camera count = %d, want the 2 chosen channels", srv.CameraCount)
	}
	path := "/api/v1/servers/" + srv.Id.String() + "/provision-nvr/channels"

	if status, _, out := te.request(http.MethodGet, path, operator, nil, nil); status != http.StatusForbidden {
		t.Fatalf("operator list = %d, want 403: %s", status, out)
	}
	status, _, out = te.request(http.MethodGet, path, te.AdminToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list = %d: %s", status, out)
	}
	var list gen.ProvisionNvrChannelList
	_ = json.Unmarshal(out, &list)
	imported := 0
	for _, c := range list.Items {
		if c.Imported != nil && *c.Imported {
			imported++
		}
	}
	if len(list.Items) != 7 || imported != 2 {
		t.Fatalf("list = %d channels, %d imported; want 7 and 2", len(list.Items), imported)
	}

	if status, _, out := te.request(http.MethodPost, path, te.AdminToken, nil, map[string]any{"channel_ids": []string{"nope"}}); status != http.StatusBadRequest {
		t.Fatalf("unknown channel = %d, want 400: %s", status, out)
	}
	if status, _, out := te.request(http.MethodPost, path, operator, nil, map[string]any{"channel_ids": []string{det.Channels[2].Id}}); status != http.StatusForbidden {
		t.Fatalf("operator import = %d, want 403: %s", status, out)
	}
	status, _, out = te.request(http.MethodPost, path, te.AdminToken, nil, map[string]any{"channel_ids": []string{det.Channels[2].Id, det.Channels[3].Id}})
	if status != http.StatusOK {
		t.Fatalf("import = %d: %s", status, out)
	}
	var res gen.ProvisionNvrImportResult
	_ = json.Unmarshal(out, &res)
	if res.Added != 2 || res.Skipped != 0 {
		t.Fatalf("import result = %+v, want 2 added", res)
	}

	frigate := "/api/v1/servers/" + te.Cameras["frigate-h01/plaza"].ServerID.String() + "/provision-nvr/channels"
	status, _, out = te.request(http.MethodGet, frigate, te.AdminToken, nil, nil)
	var e gen.Error
	_ = json.Unmarshal(out, &e)
	if status != http.StatusConflict || e.Code != "unsupported_for_server_kind" {
		t.Fatalf("channels of a Frigate server = %d %q, want 409 unsupported_for_server_kind", status, e.Code)
	}
}
