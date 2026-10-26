package inventory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/camconfig"
)

func TestAdvancedONVIFReducesStreamURIsAndSkipsIdentityAndSerial(t *testing.T) {
	const ui = "admin" + ":" + "hunter2" + "@"
	caps := strings.ReplaceAll(`{"manufacturer":"Axis","model":"P3245","serial_number":"SN123","has_ptz":true,
		 "main_stream_url":"rtsp://USERINFOh/main?token=abc",
		 "media_profiles":[{"token":"p1","name":"main","encoding":"H264","width":1920,"height":1080,"frame_rate":25.5,"bitrate":4096,"stream_uri":"rtsp://USERINFOh:554/main?token=abc"},"junk"],
		 "ptz_presets":[{"token":"1"}],"analytics":["person"]}`, "USERINFO", ui)
	in := camconfig.Inventory{
		ONVIFEndpoint: "http://" + ui + "192.168.1.50/onvif/device_service", ONVIFVersion: "2.0", ONVIFProfile: "ONVIF-T",
		ONVIFCapabilities: json.RawMessage(caps),
	}
	got := advancedONVIF(in)
	raw, _ := json.Marshal(got)
	for _, leak := range []string{"hunter2", "token=abc", "SN123"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("leaks %q: %s", leak, raw)
		}
	}
	if len(got.Profiles) != 1 || got.Profiles[0].StreamURI != "rtsp://h:554/main" || got.Profiles[0].FPS != 25.5 {
		t.Fatalf("profiles = %+v", got.Profiles)
	}
	if got.Endpoint != "http://192.168.1.50/onvif/device_service" || !got.Capabilities["has_ptz"] {
		t.Fatalf("onvif = %+v", got)
	}
	if _, has := got.Reported["ptz_presets"]; has {
		t.Fatal("nested documents are not echoed")
	}
	if _, has := got.Reported["analytics"]; !has {
		t.Fatal("lists of scalars are reported")
	}
	if empty := advancedONVIF(camconfig.Inventory{}); empty.Configured || empty.Profiles == nil || empty.Capabilities == nil {
		t.Fatalf("empty = %+v", empty)
	}
}

func TestClassifyHistoryMarksRolledBackAndRollbackable(t *testing.T) {
	at := time.Now()
	mk := func(id int64, origin, result string, prev map[string]any, rollbackOf int64) auditRecord {
		d := map[string]any{"origin": origin, "result": result, "previous_values": prev, "new_values": map[string]any{}}
		if rollbackOf != 0 {
			d["rollback_of"] = float64(rollbackOf)
		}
		return auditRecord{id: id, at: at, name: "ana", details: d}
	}
	// newest first, as the audit query returns them
	out := classifyHistory([]auditRecord{
		mk(3, "ui", "ok", map[string]any{"recording.enabled": true}, 2),
		mk(2, "ui", "ok", map[string]any{"recording.enabled": false}, 0),
		mk(1, "ui", "ok", map[string]any{"detection.enabled": nil}, 0),
	})
	if !out[0].Rollbackable || out[0].RollbackOf != 2 {
		t.Fatalf("rollback entry = %+v", out[0])
	}
	if out[1].Rollbackable || out[1].RolledBackBy != 3 || out[1].RollbackReason != "el cambio ya fue restaurado" {
		t.Fatalf("undone entry = %+v", out[1])
	}
	if out[2].Rollbackable || !strings.Contains(out[2].RollbackReason, "valor anterior") {
		t.Fatalf("no old value entry = %+v", out[2])
	}
}
