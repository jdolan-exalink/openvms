package onvifdiscover

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"
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

	t.Run("snapshot unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/onvif/snapshot", bytes.NewReader([]byte(`{}`)))
		rec := httptest.NewRecorder()
		handler.HandleSnapshot(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}

func TestHandleSnapshotReturnsOnlyJPEGBytes(t *testing.T) {
	const snapshotURI = "http://192.168.1.20/private-camera-path/snapshot.jpg?token=hidden"
	var jpegBody bytes.Buffer
	if err := jpeg.Encode(&jpegBody, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	jpeg := jpegBody.Bytes()
	soap := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body><trt:GetSnapshotUriResponse><trt:MediaUri><tt:Uri>` + snapshotURI + `</tt:Uri></trt:MediaUri></trt:GetSnapshotUriResponse></s:Body></s:Envelope>`
	soapCalls, snapshotCalls := 0, 0
	handler, err := NewOpsHandler("secret-agent-token", Config{AllowedCIDRs: []string{"192.168.1.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	handler.onvifTransport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		soapCalls++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(soap))}, nil
	})
	handler.snapshotTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		snapshotCalls++
		if r.URL.String() != snapshotURI {
			t.Errorf("snapshot request URL = %q, want camera URI", r.URL.String())
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(bytes.NewReader(jpeg)), Request: r}, nil
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/onvif/snapshot", strings.NewReader(`{"endpoint":"http://192.168.1.20/onvif/device_service","profile_token":"main"}`))
	req.Header.Set("Authorization", "Bearer secret-agent-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.HandleSnapshot(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(rec.Body.Bytes(), jpeg) {
		t.Fatalf("response status=%d headers=%v body=%x", rec.Code, rec.Header(), rec.Body.Bytes())
	}
	if strings.Contains(rec.Body.String(), snapshotURI) || soapCalls != 1 || snapshotCalls != 1 {
		t.Fatalf("snapshot URI leaked or unexpected call counts: soap=%d snapshot=%d body=%q", soapCalls, snapshotCalls, rec.Body.String())
	}
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
