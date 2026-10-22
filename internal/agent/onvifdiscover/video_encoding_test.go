package onvifdiscover

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/onvif"
)

type fakeEncoderCamera struct {
	mu        sync.Mutex
	encoding  string // H264 or H265
	supports  []string
	media2URL string // "" = the camera does not advertise Media2
	ignoreSet bool
	calls     []string
}

func (c *fakeEncoderCamera) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	body := string(raw)
	reply := func(inner string) (*http.Response, error) {
		xml := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tr2="http://www.onvif.org/ver20/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body>` + inner + `</s:Body></s:Envelope>`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(xml)), Header: make(http.Header)}, nil
	}
	switch {
	case strings.Contains(body, "<tds:GetServices"):
		c.calls = append(c.calls, "GetServices")
		services := `<tds:Service><tds:Namespace>http://www.onvif.org/ver10/device/wsdl</tds:Namespace><tds:XAddr>http://192.168.1.50/onvif/device_service</tds:XAddr><tds:Version><tt:Major>2</tt:Major><tt:Minor>0</tt:Minor></tds:Version></tds:Service>`
		if c.media2URL != "" {
			services += `<tds:Service><tds:Namespace>http://www.onvif.org/ver20/media/wsdl</tds:Namespace><tds:XAddr>` + c.media2URL + `</tds:XAddr><tds:Version><tt:Major>2</tt:Major><tt:Minor>0</tt:Minor></tds:Version></tds:Service>`
		}
		return reply(`<tds:GetServicesResponse>` + services + `</tds:GetServicesResponse>`)
	case strings.Contains(body, "<tr2:GetVideoEncoderConfigurationOptions"):
		c.calls = append(c.calls, "Options")
		var opts strings.Builder
		for _, e := range c.supports {
			profiles := "Main"
			if e == "H264" {
				profiles = "Baseline Main High"
			}
			fmt.Fprintf(&opts, `<tr2:Options GovLengthRange="1 100" FrameRatesSupported="5 10 15 20 25" ProfilesSupported="%s"><tt:Encoding>%s</tt:Encoding><tt:QualityRange><tt:Min>1</tt:Min><tt:Max>6</tt:Max></tt:QualityRange><tt:ResolutionsAvailable><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:ResolutionsAvailable><tt:BitrateRange><tt:Min>32</tt:Min><tt:Max>8192</tt:Max></tt:BitrateRange></tr2:Options>`, profiles, e)
		}
		return reply(`<tr2:GetVideoEncoderConfigurationOptionsResponse>` + opts.String() + `</tr2:GetVideoEncoderConfigurationOptionsResponse>`)
	case strings.Contains(body, "<tr2:GetVideoEncoderConfigurations"):
		c.calls = append(c.calls, "Get")
		return reply(`<tr2:GetVideoEncoderConfigurationsResponse><tr2:Configurations token="enc_main" GovLength="50" Profile="Main"><tt:Name>Main</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>` + c.encoding + `</tt:Encoding><tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution><tt:RateControl ConstantBitRate="false"><tt:FrameRateLimit>20</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl><tt:Quality>5</tt:Quality></tr2:Configurations></tr2:GetVideoEncoderConfigurationsResponse>`)
	case strings.Contains(body, "<tr2:SetVideoEncoderConfiguration"):
		c.calls = append(c.calls, "Set")
		if !c.ignoreSet {
			if strings.Contains(body, "<tt:Encoding>H264</tt:Encoding>") {
				c.encoding = "H264"
			} else if strings.Contains(body, "<tt:Encoding>H265</tt:Encoding>") {
				c.encoding = "H265"
			}
		}
		return reply(`<tr2:SetVideoEncoderConfigurationResponse/>`)
	}
	return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
}

func (c *fakeEncoderCamera) did(op string) bool {
	for _, x := range c.calls {
		if x == op {
			return true
		}
	}
	return false
}

func newEncodingOps(cam *fakeEncoderCamera) (videoEncodingOps, onvif.Endpoint) {
	device, _ := onvif.ParseEndpoint("http://192.168.1.50/onvif/device_service")
	allowed := netip.MustParsePrefix("192.168.1.0/24")
	return videoEncodingOps{
		client:  onvif.NewClient(cam, onvif.Config{}),
		allowed: func(a netip.Addr) bool { return allowed.Contains(a) },
	}, device
}

func hevcCamera() *fakeEncoderCamera {
	return &fakeEncoderCamera{encoding: "H265", supports: []string{"H265", "H264"}, media2URL: "http://192.168.1.50/onvif/media2"}
}

func TestVideoEncodingOptionsListsWhatEachConfigurationCanDo(t *testing.T) {
	cam := hevcCamera()
	ops, device := newEncodingOps(cam)
	res, status := ops.run(context.Background(), device, VideoEncodingRequest{Action: "options"})
	if status != 200 || !res.Success || !res.Supported {
		t.Fatalf("status %d, result %+v", status, res)
	}
	if len(res.Configurations) != 1 {
		t.Fatalf("configurations: %+v", res.Configurations)
	}
	c := res.Configurations[0]
	if c.Token != "enc_main" || c.Encoding != "H265" || c.Width != 1920 || c.Height != 1080 || c.FPS != 20 || c.Bitrate != 4096 {
		t.Errorf("configuration: %+v", c)
	}
	if strings.Join(c.SupportedEncodings, ",") != "H265,H264" {
		t.Errorf("supported encodings: %v", c.SupportedEncodings)
	}
	if cam.did("Set") {
		t.Fatal("reading the options must never write")
	}
}

func TestVideoEncodingOptionsWithoutMedia2IsNotAnError(t *testing.T) {
	cam := hevcCamera()
	cam.media2URL = ""
	ops, device := newEncodingOps(cam)
	res, status := ops.run(context.Background(), device, VideoEncodingRequest{Action: "options"})
	if status != 200 || !res.Success || res.Supported || res.Code != "media2_unsupported" || len(res.Configurations) != 0 {
		t.Fatalf("status %d, result %+v", status, res)
	}
}

func TestVideoEncodingIgnoresAMedia2EndpointOutsideTheAllowedNetworks(t *testing.T) {
	cam := hevcCamera()
	cam.media2URL = "http://10.9.9.9/onvif/media2" // a camera pointing the agent at another host
	ops, device := newEncodingOps(cam)
	res, status := ops.run(context.Background(), device, VideoEncodingRequest{Action: "set", ConfigToken: "enc_main", Encoding: "H264"})
	if status != 200 || res.Supported || res.Code != "media2_unsupported" {
		t.Fatalf("status %d, result %+v", status, res)
	}
	if cam.did("Set") || cam.did("Get") {
		t.Fatalf("nothing may be sent to a host outside the allowed networks: %v", cam.calls)
	}
}

func TestVideoEncodingSetSwitchesTheEncodingAndConfirmsIt(t *testing.T) {
	cam := hevcCamera()
	ops, device := newEncodingOps(cam)
	res, status := ops.run(context.Background(), device, VideoEncodingRequest{Action: "set", ConfigToken: "enc_main", Encoding: "h264"})
	if status != 200 || !res.Success || res.PreviousEncoding != "H265" || res.Encoding != "H264" {
		t.Fatalf("status %d, result %+v", status, res)
	}
	if cam.encoding != "H264" {
		t.Fatalf("the camera still reports %s", cam.encoding)
	}
}

func TestVideoEncodingSetRefusesWhatTheCameraDidNotList(t *testing.T) {
	cam := hevcCamera()
	cam.supports = []string{"H265"}
	ops, device := newEncodingOps(cam)
	res, status := ops.run(context.Background(), device, VideoEncodingRequest{Action: "set", ConfigToken: "enc_main", Encoding: "H264"})
	if status != 422 || res.Success || res.Code != "encoding_unsupported" {
		t.Fatalf("status %d, result %+v", status, res)
	}
	if cam.did("Set") {
		t.Fatal("an unsupported encoding must not be written")
	}
}

func TestVideoEncodingSetOnAnAlreadyMatchingEncoding(t *testing.T) {
	cam := hevcCamera()
	ops, device := newEncodingOps(cam)
	res, status := ops.run(context.Background(), device, VideoEncodingRequest{Action: "set", ConfigToken: "enc_main", Encoding: "H265"})
	if status != 409 || res.Success || res.Code != "encoding_unchanged" || cam.did("Set") {
		t.Fatalf("status %d, result %+v, calls %v", status, res, cam.calls)
	}
}

func TestVideoEncodingSetUnknownConfiguration(t *testing.T) {
	cam := hevcCamera()
	ops, device := newEncodingOps(cam)
	res, status := ops.run(context.Background(), device, VideoEncodingRequest{Action: "set", ConfigToken: "nope", Encoding: "H264"})
	if status != 404 || res.Code != "configuration_not_found" || cam.did("Set") {
		t.Fatalf("status %d, result %+v", status, res)
	}
}

func TestVideoEncodingSetReportsAChangeTheCameraDidNotApply(t *testing.T) {
	cam := hevcCamera()
	cam.ignoreSet = true
	ops, device := newEncodingOps(cam)
	res, status := ops.run(context.Background(), device, VideoEncodingRequest{Action: "set", ConfigToken: "enc_main", Encoding: "H264"})
	if status != 502 || res.Success || res.Code != "not_applied" {
		t.Fatalf("status %d, result %+v", status, res)
	}
}

func TestVideoEncodingRejectsBadRequests(t *testing.T) {
	cam := hevcCamera()
	ops, device := newEncodingOps(cam)
	for name, req := range map[string]VideoEncodingRequest{
		"unknown action":      {Action: "reboot"},
		"set without token":   {Action: "set", Encoding: "H264"},
		"set without target":  {Action: "set", ConfigToken: "enc_main"},
		"only h264 and h265":  {Action: "set", ConfigToken: "enc_main", Encoding: "AV1"},
		"mjpeg is not a goal": {Action: "set", ConfigToken: "enc_main", Encoding: "JPEG"},
	} {
		res, status := ops.run(context.Background(), device, req)
		if status != 400 || res.Success || res.Code == "" {
			t.Errorf("%s: status %d, result %+v", name, status, res)
		}
	}
	if len(cam.calls) != 0 {
		t.Fatalf("a bad request must not reach the camera: %v", cam.calls)
	}
}
