package onvif

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type ptzRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f ptzRoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestPTZClientOperations(t *testing.T) {
	endpoint, err := ParseEndpoint("http://192.168.1.100/onvif/ptz_service")
	if err != nil {
		t.Fatal(err)
	}

	var lastAction string
	var lastBody string

	transport := ptzRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		lastBody = string(body)
		lastAction = r.Header.Get("Content-Type")

		switch {
		case strings.Contains(lastBody, "ContinuousMove"):
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tptz:ContinuousMoveResponse xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl"/></s:Body></s:Envelope>`)),
				Header:     make(http.Header),
			}, nil
		case strings.Contains(lastBody, "Stop"):
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tptz:StopResponse xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl"/></s:Body></s:Envelope>`)),
				Header:     make(http.Header),
			}, nil
		case strings.Contains(lastBody, "GetPresets"):
			xmlResp := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
				<s:Body>
					<tptz:GetPresetsResponse>
						<tptz:Preset token="1"><tt:Name>Gate</tt:Name></tptz:Preset>
						<tptz:Preset token="2"><tt:Name>Parking</tt:Name></tptz:Preset>
					</tptz:GetPresetsResponse>
				</s:Body>
			</s:Envelope>`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(xmlResp)),
				Header:     make(http.Header),
			}, nil
		case strings.Contains(lastBody, "GotoPreset"):
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tptz:GotoPresetResponse xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl"/></s:Body></s:Envelope>`)),
				Header:     make(http.Header),
			}, nil
		}

		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(""))}, nil
	})

	client := NewClient(transport, Config{})
	ptzClient := NewPTZClient(client, endpoint)

	// ContinuousMove
	if err := ptzClient.ContinuousMove(context.Background(), "Profile_1", 0.5, -0.2, 0.0, 3); err != nil {
		t.Fatalf("ContinuousMove failed: %v", err)
	}
	if !strings.Contains(lastBody, "ContinuousMove") || !strings.Contains(lastBody, "Profile_1") {
		t.Errorf("ContinuousMove unexpected body: %s", lastBody)
	}

	// Stop
	if err := ptzClient.Stop(context.Background(), "Profile_1", true, true); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if !strings.Contains(lastBody, "Stop") {
		t.Errorf("Stop unexpected body: %s", lastBody)
	}

	// GetPresets
	presets, err := ptzClient.GetPresets(context.Background(), "Profile_1")
	if err != nil {
		t.Fatalf("GetPresets failed: %v", err)
	}
	if len(presets) != 2 || presets[0].Name != "Gate" || presets[1].Name != "Parking" {
		t.Errorf("unexpected presets: %+v", presets)
	}

	// GotoPreset
	if err := ptzClient.GotoPreset(context.Background(), "Profile_1", "2"); err != nil {
		t.Fatalf("GotoPreset failed: %v", err)
	}
	if !strings.Contains(lastBody, "GotoPreset") || !strings.Contains(lastBody, "2") {
		t.Errorf("GotoPreset unexpected body: %s", lastBody)
	}

	_ = lastAction
}
