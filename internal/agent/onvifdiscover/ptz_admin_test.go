package onvifdiscover

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const ptzEnv = `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body>%s</s:Body></s:Envelope>`

// ptzCamera answers the PTZ operations and SystemReboot, remembering each request body.
type ptzCamera struct {
	mu     sync.Mutex
	srv    *httptest.Server
	bodies []string
	fail   bool
}

func newPTZCamera(t *testing.T) *ptzCamera {
	t.Helper()
	c := &ptzCamera{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		fail := c.fail
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/soap+xml")
		if fail {
			fmt.Fprint(w, `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault><s:Code><s:Value>s:Sender</s:Value></s:Code><s:Reason><s:Text>admin:hunter2-secret</s:Text></s:Reason></s:Fault></s:Body></s:Envelope>`)
			return
		}
		switch {
		case strings.Contains(body, "<tptz:SetPreset "):
			fmt.Fprintf(w, ptzEnv, `<tptz:SetPresetResponse><tptz:PresetToken>7</tptz:PresetToken></tptz:SetPresetResponse>`)
		case strings.Contains(body, "<tptz:RemovePreset "):
			fmt.Fprintf(w, ptzEnv, `<tptz:RemovePresetResponse/>`)
		case strings.Contains(body, "<tptz:GotoHomePosition "):
			fmt.Fprintf(w, ptzEnv, `<tptz:GotoHomePositionResponse/>`)
		case strings.Contains(body, "<tptz:SetHomePosition "):
			fmt.Fprintf(w, ptzEnv, `<tptz:SetHomePositionResponse/>`)
		case strings.Contains(body, "<tptz:GetNodes "):
			fmt.Fprintf(w, ptzEnv, `<tptz:GetNodesResponse><tptz:PTZNode token="node1" FixedHomePosition="false"><tt:Name>Main</tt:Name><tt:SupportedPTZSpaces><tt:ContinuousPanTiltVelocitySpace/></tt:SupportedPTZSpaces><tt:MaximumNumberOfPresets>64</tt:MaximumNumberOfPresets><tt:HomeSupported>true</tt:HomeSupported></tptz:PTZNode></tptz:GetNodesResponse>`)
		case strings.Contains(body, "<tds:SystemReboot"):
			fmt.Fprintf(w, ptzEnv, `<tds:SystemRebootResponse><tds:Message>Rebooting in 5s</tds:Message></tds:SystemRebootResponse>`)
		default:
			http.Error(w, "unknown", http.StatusBadRequest)
		}
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *ptzCamera) post(t *testing.T, call func(*OpsHandler, http.ResponseWriter, *http.Request), path, body string) (int, string) {
	t.Helper()
	h, err := NewOpsHandler("agent-token", Config{AllowedCIDRs: []string{"127.0.0.0/8"}})
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"endpoint":%q,"username":"admin","password":"hunter2-secret"%s}`, c.srv.URL+"/onvif/ptz", body)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer agent-token")
	rec := httptest.NewRecorder()
	call(h, rec, req)
	return rec.Code, rec.Body.String()
}

func ptzCall(h *OpsHandler, w http.ResponseWriter, r *http.Request) { h.HandlePTZ(w, r) }

func TestHandlePTZPresetHomeAndNodeActions(t *testing.T) {
	cam := newPTZCamera(t)
	status, body := cam.post(t, ptzCall, "/v1/onvif/ptz", `,"action":"set_preset","profile_token":"prof","preset_name":"Entrada"`)
	if status != 200 || !strings.Contains(body, `"preset_token":"7"`) || !strings.Contains(cam.bodies[len(cam.bodies)-1], "<tptz:PresetName>Entrada</tptz:PresetName>") {
		t.Fatalf("set_preset: %d %s", status, body)
	}
	if strings.Contains(cam.bodies[len(cam.bodies)-1], "<tptz:PresetToken>") {
		t.Fatal("a new preset must not send a token")
	}
	status, _ = cam.post(t, ptzCall, "/v1/onvif/ptz", `,"action":"set_preset","profile_token":"prof","preset_token":"7","preset_name":"Portón"`)
	if status != 200 || !strings.Contains(cam.bodies[len(cam.bodies)-1], "<tptz:PresetToken>7</tptz:PresetToken>") {
		t.Fatalf("rename must reuse the token: %d", status)
	}
	if status, _ = cam.post(t, ptzCall, "/v1/onvif/ptz", `,"action":"remove_preset","profile_token":"prof","preset_token":"7"`); status != 200 {
		t.Errorf("remove_preset: %d", status)
	}
	if status, _ = cam.post(t, ptzCall, "/v1/onvif/ptz", `,"action":"remove_preset","profile_token":"prof"`); status != http.StatusBadRequest {
		t.Errorf("remove_preset without token: %d", status)
	}
	for _, a := range []string{"goto_home", "set_home"} {
		if status, _ = cam.post(t, ptzCall, "/v1/onvif/ptz", `,"action":"`+a+`","profile_token":"prof"`); status != 200 {
			t.Errorf("%s: %d", a, status)
		}
	}
	status, body = cam.post(t, ptzCall, "/v1/onvif/ptz", `,"action":"get_nodes"`)
	if status != 200 || !strings.Contains(body, `"home_supported":true`) || !strings.Contains(body, `"maximum_presets":64`) {
		t.Fatalf("get_nodes: %d %s", status, body)
	}
}

func TestHandlePTZNewActionsNeverEchoCredentials(t *testing.T) {
	cam := newPTZCamera(t)
	cam.fail = true
	for _, a := range []string{"set_preset", "remove_preset", "goto_home", "set_home", "get_nodes"} {
		status, body := cam.post(t, ptzCall, "/v1/onvif/ptz", `,"action":"`+a+`","profile_token":"prof","preset_token":"7"`)
		if status != http.StatusBadGateway {
			t.Errorf("%s: status %d", a, status)
		}
		for _, secret := range []string{"hunter2-secret", "admin", cam.srv.URL} {
			if strings.Contains(body, secret) {
				t.Errorf("%s leaks %q: %s", a, secret, body)
			}
		}
	}
}

func TestHandleRebootSendsSystemRebootAndHidesFailures(t *testing.T) {
	cam := newPTZCamera(t)
	reboot := func(h *OpsHandler, w http.ResponseWriter, r *http.Request) { h.HandleReboot(w, r) }
	status, body := cam.post(t, reboot, "/v1/onvif/reboot", ``)
	if status != 200 || !strings.Contains(body, `"success":true`) || !strings.Contains(cam.bodies[len(cam.bodies)-1], "SystemReboot") {
		t.Fatalf("reboot: %d %s", status, body)
	}
	cam.fail = true
	status, body = cam.post(t, reboot, "/v1/onvif/reboot", ``)
	if status != http.StatusBadGateway || strings.Contains(body, "hunter2-secret") {
		t.Fatalf("failed reboot: %d %s", status, body)
	}
	h, _ := NewOpsHandler("agent-token", Config{AllowedCIDRs: []string{"192.168.1.0/24"}})
	rec := httptest.NewRecorder()
	h.HandleReboot(rec, httptest.NewRequest(http.MethodPost, "/v1/onvif/reboot", strings.NewReader(`{}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated reboot: %d", rec.Code)
	}
}
