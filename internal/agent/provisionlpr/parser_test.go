package provisionlpr

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParserJSONPayload(t *testing.T) {
	parser := NewParser()
	body := []byte(`{
		"event_type": "lpr",
		"camera_id": "cam-lpr-01",
		"plate": "af 123-bc",
		"time": "2026-08-11T15:25:40-03:00",
		"confidence": 98.5,
		"list_status": "allowlist"
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/lpr/event", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.15.107:45678"

	now := time.Now()
	event, err := parser.Parse(req, body, now)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if event.Plate != "AF123BC" {
		t.Fatalf("expected plate AF123BC, got %q", event.Plate)
	}
	if event.CameraID != "cam-lpr-01" {
		t.Fatalf("expected camera_id cam-lpr-01, got %q", event.CameraID)
	}
	if event.CameraIP != "10.0.15.107" {
		t.Fatalf("expected camera_ip 10.0.15.107, got %q", event.CameraIP)
	}
	if event.Confidence == nil || *event.Confidence != 98.5 {
		t.Fatalf("expected confidence 98.5, got %v", event.Confidence)
	}
	if event.ListStatus == nil || *event.ListStatus != "allowlist" {
		t.Fatalf("expected list_status allowlist, got %v", event.ListStatus)
	}
	if event.EventTime.IsZero() {
		t.Fatal("expected non-zero event time")
	}
}

func TestParserXMLPayload(t *testing.T) {
	parser := NewParser()
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<EventNotificationAlert>
	<deviceId>cam-lpr-02</deviceId>
	<plateNumber>AE-456-FG</plateNumber>
	<time>2026-08-11 15:26:03</time>
	<confidence>95.0</confidence>
	<listStatus>blocklist</listStatus>
</EventNotificationAlert>`)

	req := httptest.NewRequest(http.MethodPost, "/Notification/LPR", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/xml")
	req.RemoteAddr = "10.0.15.108:34567"

	now := time.Now()
	event, err := parser.Parse(req, body, now)
	if err != nil {
		t.Fatalf("Parse XML error: %v", err)
	}

	if event.Plate != "AE456FG" {
		t.Fatalf("expected plate AE456FG, got %q", event.Plate)
	}
	if event.CameraID != "cam-lpr-02" {
		t.Fatalf("expected camera_id cam-lpr-02, got %q", event.CameraID)
	}
	if event.CameraIP != "10.0.15.108" {
		t.Fatalf("expected camera_ip 10.0.15.108, got %q", event.CameraIP)
	}
}

func TestParserMultipartWithImageCrop(t *testing.T) {
	parser := NewParser()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// JSON metadata part
	metaPart, err := mw.CreateFormField("metadata")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = metaPart.Write([]byte(`{"plate":"AB123CD","camera_id":"cam-gate"}`))

	// Plate crop part
	cropPart, err := mw.CreateFormFile("plate_crop", "cutout.jpg")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = cropPart.Write([]byte("\xFF\xD8\xFF\xE0\x00\x10JFIFfakecropimagebytes"))

	// Panorama part
	scenePart, err := mw.CreateFormFile("scene_image", "panorama.jpg")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = scenePart.Write([]byte("\xFF\xD8\xFF\xE0\x00\x10JFIFfakesceneimagebytes"))

	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/SendLPR", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.RemoteAddr = "10.0.15.109:55555"

	event, err := parser.Parse(req, buf.Bytes(), time.Now())
	if err != nil {
		t.Fatalf("Parse multipart error: %v", err)
	}

	if event.Plate != "AB123CD" {
		t.Fatalf("expected AB123CD, got %q", event.Plate)
	}
	if event.PlateCrop == nil || *event.PlateCrop == "" {
		t.Fatal("expected plate crop to be parsed and encoded")
	}
	if event.PanoramicImage == nil || *event.PanoramicImage == "" {
		t.Fatal("expected panorama image to be parsed and encoded")
	}
}

func TestParserFormUrlEncoded(t *testing.T) {
	parser := NewParser()
	formBody := []byte("plate=AA999ZZ&camera_id=cam-front&confidence=99")

	req := httptest.NewRequest(http.MethodPost, "/v1/lpr/event", bytes.NewReader(formBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "10.0.15.110:44444"

	event, err := parser.Parse(req, formBody, time.Now())
	if err != nil {
		t.Fatalf("Parse form error: %v", err)
	}

	if event.Plate != "AA999ZZ" {
		t.Fatalf("expected AA999ZZ, got %q", event.Plate)
	}
	if event.CameraID != "cam-front" {
		t.Fatalf("expected cam-front, got %q", event.CameraID)
	}
}

func TestParserRegexFallback(t *testing.T) {
	parser := NewParser()
	rawBody := []byte("SOME PROPRIETARY RAW PROTOCOL HEADER ... PLATE: [XY789ZT] ... END")

	req := httptest.NewRequest(http.MethodPost, "/unknown-endpoint", bytes.NewReader(rawBody))
	req.RemoteAddr = "10.0.15.111:33333"

	event, err := parser.Parse(req, rawBody, time.Now())
	if err != nil {
		t.Fatalf("expected regex fallback to extract plate, got err: %v", err)
	}

	if event.Plate != "XY789ZT" {
		t.Fatalf("expected XY789ZT, got %q", event.Plate)
	}
}

func TestParserVehicleAttributes(t *testing.T) {
	parser := NewParser()
	body := []byte(`{
		"plate": "AE456CD",
		"vehicle_color": "Blue",
		"vehicle_brand": "Toyota",
		"vehicle_model": "Corolla",
		"vehicle_type": "Sedan",
		"speed": 45.2,
		"direction": "North"
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/lpr/event", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	event, err := parser.Parse(req, body, time.Now())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if event.Plate != "AE456CD" {
		t.Errorf("Plate = %q, want AE456CD", event.Plate)
	}
	if event.VehicleColor == nil || *event.VehicleColor != "Blue" {
		t.Errorf("VehicleColor = %v, want Blue", event.VehicleColor)
	}
	if event.VehicleBrand == nil || *event.VehicleBrand != "Toyota" {
		t.Errorf("VehicleBrand = %v, want Toyota", event.VehicleBrand)
	}
	if event.VehicleModel == nil || *event.VehicleModel != "Corolla" {
		t.Errorf("VehicleModel = %v, want Corolla", event.VehicleModel)
	}
	if event.VehicleType == nil || *event.VehicleType != "Sedan" {
		t.Errorf("VehicleType = %v, want Sedan", event.VehicleType)
	}
	if event.Speed == nil || *event.Speed != 45.2 {
		t.Errorf("Speed = %v, want 45.2", event.Speed)
	}
	if event.Direction == nil || *event.Direction != "North" {
		t.Errorf("Direction = %v, want North", event.Direction)
	}
}
