package onvifdiscover

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpsHandlerAuthAndEndpointValidation(t *testing.T) {
	handler, err := NewOpsHandler("secret-agent-token", Config{AllowedCIDRs: []string{"192.168.1.0/24"}})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/onvif/sync-time", bytes.NewReader([]byte(`{}`)))
		rec := httptest.NewRecorder()
		handler.HandleSyncTime(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("disallowed CIDR", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/onvif/sync-time", bytes.NewReader([]byte(`{"endpoint":"http://10.0.0.1/onvif/device_service"}`)))
		req.Header.Set("Authorization", "Bearer secret-agent-token")
		rec := httptest.NewRecorder()
		handler.HandleSyncTime(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 bad request for disallowed CIDR", rec.Code)
		}
	})

	t.Run("ptz unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/onvif/ptz", bytes.NewReader([]byte(`{}`)))
		rec := httptest.NewRecorder()
		handler.HandlePTZ(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("video-config unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/onvif/video-config", bytes.NewReader([]byte(`{}`)))
		rec := httptest.NewRecorder()
		handler.HandleVideoConfig(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("video-encoding unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/onvif/video-encoding", bytes.NewReader([]byte(`{}`)))
		rec := httptest.NewRecorder()
		handler.HandleVideoEncoding(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("video-encoding refuses an endpoint outside the allowed networks", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/onvif/video-encoding", bytes.NewReader([]byte(`{"endpoint":"http://10.0.0.1/onvif/device_service","action":"options"}`)))
		req.Header.Set("Authorization", "Bearer secret-agent-token")
		rec := httptest.NewRecorder()
		handler.HandleVideoEncoding(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("imaging unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/onvif/imaging", bytes.NewReader([]byte(`{}`)))
		rec := httptest.NewRecorder()
		handler.HandleImaging(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}

func TestHandleImagingReturnsAdvertisedOptions(t *testing.T) {
	camera := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body><timg:GetOptionsResponse><timg:ImagingOptions><tt:Brightness><tt:Min>5</tt:Min><tt:Max>95</tt:Max></tt:Brightness></timg:ImagingOptions></timg:GetOptionsResponse></s:Body></s:Envelope>`
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = w.Write([]byte(body))
	}))
	defer camera.Close()

	handler, err := NewOpsHandler("secret-agent-token", Config{AllowedCIDRs: []string{"127.0.0.0/8"}})
	if err != nil {
		t.Fatal(err)
	}
	requestBody := `{"endpoint":"` + camera.URL + `","video_source_token":"source-1","action":"options"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/onvif/imaging", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer secret-agent-token")
	recorder := httptest.NewRecorder()
	handler.HandleImaging(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"brightness":{"min":5,"max":95}`) {
		t.Errorf("response omitted advertised options: %s", recorder.Body.String())
	}
}
