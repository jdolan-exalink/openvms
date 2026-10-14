package provisionlpr

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReceiverSendKeepalive(t *testing.T) {
	cfg := DefaultConfig()
	receiver := NewReceiver(cfg, nil)

	// Keepalive request with Content-Length 0 as observed in Provision cameras
	req := httptest.NewRequest(http.MethodPost, "/SendKeepalive?camera_id=cam-lpr-01", bytes.NewReader(nil))
	req.RemoteAddr = "10.0.15.107:54321"
	rec := httptest.NewRecorder()

	receiver.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	status, found := receiver.Heartbeat().Status("cam-lpr-01", "10.0.15.107", time.Now())
	if !found {
		t.Fatal("expected camera status to be recorded")
	}
	if !status.Online || status.KeepaliveCount != 1 {
		t.Fatalf("unexpected camera status: %+v", status)
	}
}

func TestReceiverEventDeduplicationAndSubscription(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeduplicationWindow = 10 * time.Second
	receiver := NewReceiver(cfg, nil)

	var receivedEvents []LPREvent
	receiver.Subscribe(func(ev LPREvent) {
		receivedEvents = append(receivedEvents, ev)
	})

	body := []byte(`{"plate":"AF123BC","camera_id":"cam-01"}`)

	// Send first event
	req1 := httptest.NewRequest(http.MethodPost, "/v1/provision-lpr/event", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	req1.RemoteAddr = "10.0.15.107:54321"
	rec1 := httptest.NewRecorder()

	receiver.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec1.Code)
	}

	// Give background worker a brief moment to process
	time.Sleep(50 * time.Millisecond)

	if len(receivedEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(receivedEvents))
	}
	if receivedEvents[0].Plate != "AF123BC" {
		t.Fatalf("expected plate AF123BC, got %q", receivedEvents[0].Plate)
	}

	// Send duplicate immediately: must return 200 OK to camera, but NOT emit to subscriber
	req2 := httptest.NewRequest(http.MethodPost, "/v1/provision-lpr/event", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.RemoteAddr = "10.0.15.107:54321"
	rec2 := httptest.NewRecorder()

	receiver.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on duplicate, got %d", rec2.Code)
	}

	time.Sleep(50 * time.Millisecond)
	if len(receivedEvents) != 1 {
		t.Fatalf("expected duplicate to be suppressed, got %d events", len(receivedEvents))
	}
}

func TestReceiverStatusEndpoint(t *testing.T) {
	cfg := DefaultConfig()
	receiver := NewReceiver(cfg, nil)

	// Record a keepalive
	req := httptest.NewRequest(http.MethodPost, "/SendKeepalive?camera_id=cam-gate", nil)
	req.RemoteAddr = "10.0.15.107:54321"
	receiver.ServeHTTP(httptest.NewRecorder(), req)

	// Query status
	statusReq := httptest.NewRequest(http.MethodGet, "/v1/provision-lpr/status", nil)
	statusRec := httptest.NewRecorder()
	receiver.ServeHTTP(statusRec, statusReq)

	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", statusRec.Code)
	}

	var resp struct {
		Cameras []CameraStatus `json:"cameras"`
	}
	if err := json.NewDecoder(statusRec.Body).Decode(&resp); err != nil {
		t.Fatalf("json decode error: %v", err)
	}

	if len(resp.Cameras) != 1 || resp.Cameras[0].CameraID != "cam-gate" || !resp.Cameras[0].Online {
		t.Fatalf("unexpected statuses: %+v", resp.Cameras)
	}
}
