package onvifdiscover

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const vdEnvelope = `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tr2="http://www.onvif.org/ver20/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body>%s</s:Body></s:Envelope>`

// vdCamera is a small ONVIF Media2 camera: one H265 profile that Set rewrites.
type vdCamera struct {
	mu     sync.Mutex
	srv    *httptest.Server
	fps    string
	ignore bool
	fault  string
	ops    []string
}

func newVDCamera(t *testing.T) *vdCamera {
	t.Helper()
	c := &vdCamera{fps: "20"}
	c.srv = httptest.NewServer(http.HandlerFunc(c.serve))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *vdCamera) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := string(raw)
	c.mu.Lock()
	defer c.mu.Unlock()
	w.Header().Set("Content-Type", "application/soap+xml")
	op := ""
	for _, n := range []string{"GetServices", "GetProfiles", "GetVideoEncoderConfigurationOptions", "SetVideoEncoderConfiguration"} {
		if strings.Contains(body, ":"+n+" ") || strings.Contains(body, ":"+n+">") {
			op = n
		}
	}
	c.ops = append(c.ops, op)
	if op == c.fault {
		fmt.Fprint(w, `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault><s:Code><s:Value>s:Sender</s:Value></s:Code><s:Reason><s:Text>admin:hunter2-secret</s:Text></s:Reason></s:Fault></s:Body></s:Envelope>`)
		return
	}
	switch op {
	case "GetServices":
		fmt.Fprintf(w, vdEnvelope, `<tds:GetServicesResponse><tds:Service><tds:Namespace>http://www.onvif.org/ver20/media/wsdl</tds:Namespace><tds:XAddr>`+c.srv.URL+`/media2</tds:XAddr><tds:Version><tt:Major>2</tt:Major><tt:Minor>0</tt:Minor></tds:Version></tds:Service></tds:GetServicesResponse>`)
	case "GetProfiles":
		fmt.Fprintf(w, vdEnvelope, `<tr2:GetProfilesResponse><tr2:Profiles token="prof_main"><tr2:Name>Main</tr2:Name><tr2:Configurations><tr2:VideoEncoder token="venc_1" GovLength="50" Profile="Main"><tt:Name>Main</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>H265</tt:Encoding><tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution><tt:RateControl ConstantBitRate="false"><tt:FrameRateLimit>`+c.fps+`</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl><tt:Quality>5</tt:Quality></tr2:VideoEncoder></tr2:Configurations></tr2:Profiles></tr2:GetProfilesResponse>`)
	case "GetVideoEncoderConfigurationOptions":
		fmt.Fprintf(w, vdEnvelope, `<tr2:GetVideoEncoderConfigurationOptionsResponse><tr2:Options GovLengthRange="1 100" FrameRatesSupported="5 10 15 20 25" ProfilesSupported="Main"><tt:Encoding>H265</tt:Encoding><tt:QualityRange><tt:Min>1</tt:Min><tt:Max>6</tt:Max></tt:QualityRange><tt:ResolutionsAvailable><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:ResolutionsAvailable><tt:BitrateRange><tt:Min>32</tt:Min><tt:Max>8192</tt:Max></tt:BitrateRange></tr2:Options></tr2:GetVideoEncoderConfigurationOptionsResponse>`)
	case "SetVideoEncoderConfiguration":
		if !c.ignore {
			if i := strings.Index(body, "<tt:FrameRateLimit>"); i >= 0 {
				rest := body[i+len("<tt:FrameRateLimit>"):]
				c.fps = rest[:strings.Index(rest, "<")]
			}
		}
		fmt.Fprintf(w, vdEnvelope, `<tr2:SetVideoEncoderConfigurationResponse/>`)
	default:
		http.Error(w, "unknown", http.StatusBadRequest)
	}
}

func videoDevice(t *testing.T, cam *vdCamera, body string) (int, VideoDeviceResult, string) {
	t.Helper()
	h, err := NewOpsHandler("agent-token", Config{AllowedCIDRs: []string{"127.0.0.0/8"}})
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"endpoint":%q,"username":"admin","password":"hunter2-secret",%s}`, cam.srv.URL+"/onvif/device_service", body)
	req := httptest.NewRequest(http.MethodPost, "/v1/onvif/video-device", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer agent-token")
	rec := httptest.NewRecorder()
	h.HandleVideoDevice(rec, req)
	var res VideoDeviceResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return rec.Code, res, rec.Body.String()
}

func TestVideoDeviceProfileStreamsAndOptions(t *testing.T) {
	cam := newVDCamera(t)
	status, res, raw := videoDevice(t, cam, `"op":"profile_streams"`)
	if status != 200 || !res.Success || len(res.Streams) != 1 || res.Streams[0].Token != "prof_main" || res.Streams[0].Codec != "H265" || res.Streams[0].FPS != 20 {
		t.Fatalf("streams: %d %s", status, raw)
	}
	status, res, raw = videoDevice(t, cam, `"op":"video_options","profile_token":"prof_main"`)
	if status != 200 || res.Options == nil || len(res.Options.Codecs) != 1 || res.Options.Codecs[0].Bitrate.Max != 8192 {
		t.Fatalf("options: %d %s", status, raw)
	}
	for _, op := range cam.ops {
		if op == "SetVideoEncoderConfiguration" {
			t.Fatal("reading must never write")
		}
	}
}

func TestVideoDeviceSetChangesOnlyRequestedFieldAndVerifies(t *testing.T) {
	cam := newVDCamera(t)
	status, res, raw := videoDevice(t, cam, `"op":"set_video_encoder","profile_token":"prof_main","desired":{"FPS":10}`)
	if status != 200 || !res.Success || res.Stream == nil || res.Stream.FPS != 10 || res.Stream.Codec != "H265" || res.Stream.BitrateKbps != 4096 {
		t.Fatalf("set: %d %s", status, raw)
	}
}

func TestVideoDeviceSetRefusesUnsupportedWithoutWriting(t *testing.T) {
	cam := newVDCamera(t)
	status, res, _ := videoDevice(t, cam, `"op":"set_video_encoder","profile_token":"prof_main","desired":{"FPS":99}`)
	if status != http.StatusUnprocessableEntity || res.Code != VideoDeviceNotSupported || len(res.Violations) != 1 || res.Violations[0].Field != "fps" {
		t.Fatalf("status %d result %+v", status, res)
	}
	for _, op := range cam.ops {
		if op == "SetVideoEncoderConfiguration" {
			t.Fatal("an unsupported change must not be written")
		}
	}
}

func TestVideoDeviceSetReportsIgnoredWriteAndUnknownProfile(t *testing.T) {
	cam := newVDCamera(t)
	cam.ignore = true
	status, res, _ := videoDevice(t, cam, `"op":"set_video_encoder","profile_token":"prof_main","desired":{"FPS":10}`)
	if status != http.StatusConflict || res.Code != VideoDeviceNotApplied || strings.Join(res.Fields, ",") != "fps" || res.Stream == nil || res.Stream.FPS != 20 {
		t.Fatalf("status %d result %+v", status, res)
	}
	status, res, _ = videoDevice(t, cam, `"op":"set_video_encoder","profile_token":"nope","desired":{"FPS":10}`)
	if status != http.StatusUnprocessableEntity || res.Code != VideoDeviceProfileNotFound {
		t.Fatalf("status %d result %+v", status, res)
	}
}

func TestVideoDeviceNeverEchoesCredentialsOrCameraText(t *testing.T) {
	cam := newVDCamera(t)
	cam.fault = "GetProfiles"
	status, res, raw := videoDevice(t, cam, `"op":"profile_streams"`)
	if status != http.StatusBadGateway || res.Code != VideoDeviceCameraError {
		t.Fatalf("status %d result %+v", status, res)
	}
	for _, secret := range []string{"hunter2-secret", "admin", cam.srv.URL} {
		if strings.Contains(raw, secret) {
			t.Fatalf("response leaks %q: %s", secret, raw)
		}
	}
}

func TestVideoDeviceValidatesRequest(t *testing.T) {
	cam := newVDCamera(t)
	for _, body := range []string{`"op":"video_options"`, `"op":"set_video_encoder","profile_token":"p"`, `"op":"reboot_everything"`} {
		if status, _, _ := videoDevice(t, cam, body); status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, status)
		}
	}
	h, _ := NewOpsHandler("agent-token", Config{AllowedCIDRs: []string{"192.168.1.0/24"}})
	req := httptest.NewRequest(http.MethodPost, "/v1/onvif/video-device", strings.NewReader(`{"endpoint":"http://10.0.0.1/x","op":"profile_streams"}`))
	rec := httptest.NewRecorder()
	h.HandleVideoDevice(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", rec.Code)
	}
	req.Header.Set("Authorization", "Bearer agent-token")
	rec = httptest.NewRecorder()
	h.HandleVideoDevice(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("disallowed endpoint: %d", rec.Code)
	}
}
