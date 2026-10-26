package camconfig

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeEngine struct {
	doc      map[string]any
	docErr   error
	codecs   map[string]string
	codecErr error
}

func (f fakeEngine) Version() string { return "0.17.1" }
func (f fakeEngine) CameraConfig(context.Context, string, bool) (map[string]any, error) {
	return f.doc, f.docErr
}
func (f fakeEngine) StreamCodecs(context.Context) (map[string]string, error) {
	return f.codecs, f.codecErr
}

// userinfo is built at run time so the fixtures hold no credential-looking literal.
const userinfo = "admin" + ":" + "s3cret" + "@"

var twoInputDoc = strings.NewReplacer("USERINFO@", userinfo).Replace(`{
  "ffmpeg": {"inputs": [
    {"path": "rtsp://USERINFO@10.0.0.5:554/Streaming/Channels/102?token=abc", "roles": ["detect"]},
    {"path": "rtsp://USERINFO@10.0.0.5:554/Streaming/Channels/101", "roles": ["record", "audio"]}
  ]},
  "detect": {"enabled": true, "width": 640, "height": 360, "fps": 5},
  "record": {"enabled": true},
  "audio": {"enabled": false},
  "objects": {"track": ["person", "car"]},
  "lpr": {"enabled": true},
  "zones": {"gate": {"objects": ["car"]}, "door": {}}
}`)

func parse(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func baseInv() Inventory {
	return Inventory{
		CameraID: "c1", Name: "Gate", RemoteName: "gate", Enabled: true, ServerID: "s1", ServerName: "Site A",
		Tags: []string{"Fisheye"}, ONVIFEndpoint: "http://10.0.0.5/onvif/device_service",
		ONVIFCapabilities: []byte(`{"manufacturer":"Hikvision","model":"DS-2CD","firmware_version":"V5.7","has_ptz":true,"has_events":true}`),
		HQStream:          "gate", LiveStream: "gate_sub", HQCodec: "h265", LiveCodec: "h264",
	}
}

func TestFrigateNormalizesTwoInputCamera(t *testing.T) {
	eng := fakeEngine{doc: parse(t, twoInputDoc), codecs: map[string]string{"101": "h265", "102": "h264"}}
	cam, err := FrigateAdapter{Engine: eng}.Read(context.Background(), baseInv())
	if err != nil {
		t.Fatal(err)
	}
	if cam.Origin.Engine != "frigate" || cam.Origin.EngineVersion != "0.17.1" || cam.Origin.ServerName != "Site A" {
		t.Fatalf("origin: %+v", cam.Origin)
	}
	d := cam.Streams.Detection
	if d == nil || d.Width != 640 || d.Height != 360 || d.FPS != 5 || d.Codec != "h264" {
		t.Fatalf("detection: %+v", d)
	}
	p := cam.Streams.Primary
	if p == nil || p.Codec != "h265" || p.URL != "rtsp://10.0.0.5:554/Streaming/Channels/101" {
		t.Fatalf("primary: %+v", p)
	}
	if p.Width != 0 || p.Height != 0 || p.FPS != 0 {
		t.Fatalf("primary resolution must stay unknown: %+v", p)
	}
	if cam.Streams.Recording != nil {
		t.Fatalf("recording must be omitted when it is the primary stream: %+v", cam.Streams.Recording)
	}
	if got := strings.Join(cam.Detection.Objects, ","); got != "person,car" {
		t.Fatalf("objects: %s", got)
	}
	if len(cam.Zones) != 2 || cam.Zones[0].Name != "door" || cam.Zones[1].Objects[0] != "car" {
		t.Fatalf("zones: %+v", cam.Zones)
	}
	if cam.Device.Manufacturer != "Hikvision" || cam.Device.Address != "10.0.0.5" || cam.Device.Firmware != "V5.7" {
		t.Fatalf("device: %+v", cam.Device)
	}
}

func TestFrigateRecordingKeptWhenDifferentFromPrimary(t *testing.T) {
	doc := parse(t, `{"ffmpeg":{"inputs":[
	  {"path":"rtsp://127.0.0.1:8554/gate","roles":["detect"]},
	  {"path":"rtsp://127.0.0.1:8554/gate_rec","roles":["record"]}]}}`)
	inv := baseInv()
	inv.HQStream = "gate"
	cam, _ := FrigateAdapter{Engine: fakeEngine{doc: doc}}.Read(context.Background(), inv)
	if cam.Streams.Primary == nil || cam.Streams.Primary.Name != "gate" {
		t.Fatalf("primary: %+v", cam.Streams.Primary)
	}
	if cam.Streams.Recording == nil || cam.Streams.Recording.Name != "gate_rec" {
		t.Fatalf("recording: %+v", cam.Streams.Recording)
	}
}

func TestFrigateSingleInputIsPrimaryAndDetection(t *testing.T) {
	doc := parse(t, `{"ffmpeg":{"inputs":[{"path":"rtsp://h/x","roles":["detect","record"]}]},"detect":{"width":1280,"height":720,"fps":10}}`)
	inv := Inventory{CameraID: "c1", RemoteName: "x"}
	cam, _ := FrigateAdapter{Engine: fakeEngine{doc: doc}}.Read(context.Background(), inv)
	if cam.Streams.Primary == nil || cam.Streams.Detection == nil || cam.Streams.Recording != nil {
		t.Fatalf("streams: %+v", cam.Streams)
	}
	if cam.Streams.Detection.Width != 1280 {
		t.Fatalf("detection: %+v", cam.Streams.Detection)
	}
}

func TestCapabilitiesAreDerivedFromEvidence(t *testing.T) {
	eng := fakeEngine{doc: parse(t, twoInputDoc)}
	cam, _ := FrigateAdapter{Engine: eng}.Read(context.Background(), baseInv())
	want := []string{CapONVIF, CapAudio, CapPTZ, CapLPR, CapFisheye, CapObjectDetection, CapRecording, CapEvents}
	if strings.Join(cam.Capabilities, ",") != strings.Join(want, ",") {
		t.Fatalf("capabilities = %v, want %v", cam.Capabilities, want)
	}
	if !cam.PTZ.Supported || !cam.Fisheye.Supported {
		t.Fatalf("ptz/fisheye: %+v %+v", cam.PTZ, cam.Fisheye)
	}

	bare := Inventory{CameraID: "c2", RemoteName: "bare"}
	cam, _ = FrigateAdapter{}.Read(context.Background(), bare)
	if len(cam.Capabilities) != 0 || cam.Capabilities == nil {
		t.Fatalf("bare camera must report no capabilities, got %v", cam.Capabilities)
	}

	tagged := Inventory{CameraID: "c3", RemoteName: "t", Tags: []string{"lpr_provision", "ojo_de_pez"}}
	cam, _ = FrigateAdapter{}.Read(context.Background(), tagged)
	if strings.Join(cam.Capabilities, ",") != CapLPR+","+CapFisheye {
		t.Fatalf("tag capabilities: %v", cam.Capabilities)
	}
}

func TestRedactsStreamCredentials(t *testing.T) {
	eng := fakeEngine{doc: parse(t, twoInputDoc)}
	cam, _ := FrigateAdapter{Engine: eng}.Read(context.Background(), baseInv())
	b, _ := json.Marshal(cam)
	for _, leak := range []string{"s3cret", "admin:", "token=abc", "@10.0.0.5"} {
		if strings.Contains(string(b), leak) {
			t.Fatalf("response leaks %q: %s", leak, b)
		}
	}
	if got := RedactURL("rtsp://" + "u" + ":" + "p@ss@host:554/a/b?x=1#f"); got != "rtsp://host:554/a/b" {
		t.Fatalf("RedactURL = %q", got)
	}
	if got := RedactURL("not a url"); got != "" {
		t.Fatalf("RedactURL(garbage) = %q", got)
	}
}

func TestEngineUnreachableDegradesToInventory(t *testing.T) {
	boom := errors.New("dial tcp rtsp://USERINFO@10.0.0.5: refused")
	for name, a := range map[string]FrigateAdapter{
		"no engine":    {},
		"read failure": {Engine: fakeEngine{docErr: boom, codecErr: boom}},
	} {
		t.Run(name, func(t *testing.T) {
			cam, err := a.Read(context.Background(), baseInv())
			if err != nil {
				t.Fatal(err)
			}
			if len(cam.Sources) == 0 || cam.Sources[0].Name != "frigate" || cam.Sources[0].Status != SourceUnavailable {
				t.Fatalf("sources: %+v", cam.Sources)
			}
			if cam.Streams.Primary == nil || cam.Streams.Primary.Codec != "h265" || cam.Streams.Detection.Codec != "h264" {
				t.Fatalf("inventory streams: %+v", cam.Streams)
			}
			if cam.Detection.Enabled != nil || cam.Recording.Enabled != nil {
				t.Fatal("engine-only fields must stay unknown")
			}
			if cam.Device.Manufacturer != "Hikvision" {
				t.Fatalf("device: %+v", cam.Device)
			}
			if b, _ := json.Marshal(cam); strings.Contains(string(b), "s3cret") {
				t.Fatalf("engine error leaked: %s", b)
			}
		})
	}
}

func TestReadRequiresCameraID(t *testing.T) {
	if _, err := (FrigateAdapter{}).Read(context.Background(), Inventory{}); err == nil {
		t.Fatal("expected error")
	}
}
