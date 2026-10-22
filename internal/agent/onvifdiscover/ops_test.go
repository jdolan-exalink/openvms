package onvifdiscover

import (
	"bytes"
	"net/http"
	"net/http/httptest"
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
