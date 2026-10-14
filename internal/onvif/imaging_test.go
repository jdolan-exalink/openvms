package onvif

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type imagingRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f imagingRoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestImagingClientOperations(t *testing.T) {
	endpoint, err := ParseEndpoint("http://192.168.1.100/onvif/imaging_service")
	if err != nil {
		t.Fatal(err)
	}

	var lastBody string
	transport := imagingRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		lastBody = string(body)

		if strings.Contains(lastBody, "GetImagingSettings") {
			xmlResp := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
				<s:Body>
					<timg:GetImagingSettingsResponse>
						<timg:ImagingSettings>
							<tt:Brightness>65.0</tt:Brightness>
							<tt:ColorSaturation>70.0</tt:ColorSaturation>
							<tt:Contrast>55.0</tt:Contrast>
							<tt:Sharpness>45.0</tt:Sharpness>
							<tt:IrCutFilter>AUTO</tt:IrCutFilter>
							<tt:WideDynamicRange>
								<tt:Mode>ON</tt:Mode>
								<tt:Level>2.0</tt:Level>
							</tt:WideDynamicRange>
						</timg:ImagingSettings>
					</timg:GetImagingSettingsResponse>
				</s:Body>
			</s:Envelope>`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(xmlResp)),
				Header:     make(http.Header),
			}, nil
		}

		if strings.Contains(lastBody, "SetImagingSettings") {
			xmlResp := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl">
				<s:Body>
					<timg:SetImagingSettingsResponse/>
				</s:Body>
			</s:Envelope>`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(xmlResp)),
				Header:     make(http.Header),
			}, nil
		}

		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(""))}, nil
	})

	client := NewClient(transport, Config{})
	imagingClient := NewImagingClient(client, endpoint)

	settings, err := imagingClient.GetImagingSettings(context.Background(), "VideoSource_1")
	if err != nil {
		t.Fatalf("GetImagingSettings failed: %v", err)
	}
	if settings.Brightness != 65.0 || settings.IrCutFilter != "AUTO" || settings.WideDynamicRange == nil || settings.WideDynamicRange.Mode != "ON" {
		t.Errorf("unexpected settings: %+v", settings)
	}

	err = imagingClient.SetImagingSettings(context.Background(), "VideoSource_1", ImagingSettings{
		Brightness:      80.0,
		Contrast:        60.0,
		ColorSaturation: 50.0,
		Sharpness:       50.0,
		IrCutFilter:     "ON",
		WideDynamicRange: &WideDynamicRange{
			Mode:  "OFF",
			Level: 0,
		},
	})
	if err != nil {
		t.Fatalf("SetImagingSettings failed: %v", err)
	}
	if !strings.Contains(lastBody, "SetImagingSettings") || !strings.Contains(lastBody, "80.000000") || !strings.Contains(lastBody, "IrCutFilter") || !strings.Contains(lastBody, ">ON<") {
		t.Errorf("SetImagingSettings unexpected body: %s", lastBody)
	}
}
