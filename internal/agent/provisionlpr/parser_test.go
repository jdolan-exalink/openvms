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

func TestParserProvisionISRXMLRealPayload(t *testing.T) {
	parser := NewParser()
	xmlPayload := []byte(`<?xml version="1.0" encoding="utf-8"?>
<config version="1.0" xmlns="http://www.tvt.net.cn/schemas/config.xsd">
  <deviceName>Escuela</deviceName>
  <mac>00:11:22:33:44:55</mac>
  <snapTime>1715000000000000</snapTime>
  <sourceDataInfo>
    <sourceBase64Data>/9j/4AAQSkZJRg_SCENE==</sourceBase64Data>
  </sourceDataInfo>
  <items>
    <item>
      <plateNumber>AF123BC</plateNumber>
      <plateColor count="4"/>
      <vehiceColor count="1"/>
      <PlateConfidence count="95"/>
      <targetImageData>
        <targetBase64Data>/9j/4AAQSkZJRg==</targetBase64Data>
      </targetImageData>
    </item>
  </items>
</config>`)

	req := httptest.NewRequest(http.MethodPost, "/SendKeepalive", bytes.NewReader(xmlPayload))
	req.Header.Set("Content-Type", "application/xml")
	req.RemoteAddr = "10.0.15.107:54321"

	now := time.Now()
	event, err := parser.Parse(req, xmlPayload, now)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if event.Plate != "AF123BC" {
		t.Errorf("Plate = %q, want AF123BC", event.Plate)
	}
	if event.CameraID != "Escuela" {
		t.Errorf("CameraID = %q, want Escuela", event.CameraID)
	}
	if event.CameraIP != "10.0.15.107" {
		t.Errorf("CameraIP = %q, want 10.0.15.107", event.CameraIP)
	}
	// 1715000000000000 us = 1715000000 seconds = 2024-05-06 12:53:20 UTC
	expectedYear := 2024
	if event.EventTime.Year() != expectedYear {
		t.Errorf("EventTime.Year = %d, want %d (event.EventTime=%v)", event.EventTime.Year(), expectedYear, event.EventTime)
	}
	if event.Confidence == nil || *event.Confidence != 0.95 {
		t.Errorf("Confidence = %v, want 0.95", event.Confidence)
	}
	if event.VehicleColor == nil || *event.VehicleColor != "Blanco" {
		t.Errorf("VehicleColor = %v, want Blanco", event.VehicleColor)
	}
	if event.PlateCrop == nil || *event.PlateCrop != "/9j/4AAQSkZJRg==" {
		got := "<nil>"
		if event.PlateCrop != nil {
			got = *event.PlateCrop
		}
		t.Errorf("PlateCrop = %q, want /9j/4AAQSkZJRg==", got)
	}
	if event.PanoramicImage == nil || *event.PanoramicImage != "/9j/4AAQSkZJRg_SCENE==" {
		got := "<nil>"
		if event.PanoramicImage != nil {
			got = *event.PanoramicImage
		}
		t.Errorf("PanoramicImage = %q, want /9j/4AAQSkZJRg_SCENE==", got)
	}
}

func TestParserProvisionISRLineCrossingAndMotionAlarm(t *testing.T) {
	parser := NewParser()
	xmlPayload := []byte(`<?xml version="1.0" encoding="UTF-8" ?>
<config version="1.7" xmlns="http://www.ipc.com/ver10">
<alarmStatusInfo>
<motionAlarm type="boolean" id="1">true</motionAlarm>
<lineCrossAlarm type="boolean" id="1">true</lineCrossAlarm>
<vehiceAlarm type="boolean" id="1">false</vehiceAlarm>
</alarmStatusInfo>
<dataTime><![CDATA[30/10/2026 16:32:28]]></dataTime>
<deviceInfo>
<deviceName><![CDATA[Ruta 1 y PPP sella]]></deviceName>
<deviceNo.><![CDATA[1]]></deviceNo.>
<sn><![CDATA[ID2C10A3G7B1]]></sn>
<ipAddress><![CDATA[10.0.15.107]]></ipAddress>
<macAddress><![CDATA[a0:e0:25:07:d2:c1]]></macAddress>
</deviceInfo>
<targetImageData>
<targetBase64Data><![CDATA[/9j/4TARGET_CROP]]></targetBase64Data>
</targetImageData>
<sourceDataInfo>
<sourceBase64Data><![CDATA[/9j/4SCENE_FULL]]></sourceBase64Data>
</sourceDataInfo>
</config>`)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(xmlPayload))
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.RemoteAddr = "10.0.15.107:54321"

	now := time.Now()
	event, err := parser.Parse(req, xmlPayload, now)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if event.EventType != "line_crossing" {
		t.Errorf("EventType = %q, want line_crossing", event.EventType)
	}
	if event.CameraID != "Ruta 1 y PPP sella" {
		t.Errorf("CameraID = %q, want 'Ruta 1 y PPP sella'", event.CameraID)
	}
	if event.CameraIP != "10.0.15.107" {
		t.Errorf("CameraIP = %q, want 10.0.15.107", event.CameraIP)
	}
	if event.ChannelID == nil || *event.ChannelID != 1 {
		t.Errorf("ChannelID = %v, want 1", event.ChannelID)
	}
	if event.PlateCrop == nil || *event.PlateCrop != "/9j/4TARGET_CROP" {
		t.Errorf("PlateCrop = %v, want /9j/4TARGET_CROP", event.PlateCrop)
	}
	if event.PanoramicImage == nil || *event.PanoramicImage != "/9j/4SCENE_FULL" {
		t.Errorf("PanoramicImage = %v, want /9j/4SCENE_FULL", event.PanoramicImage)
	}
}

func TestParserProvisionISRMotionAlarmAlone(t *testing.T) {
	parser := NewParser()
	xmlPayload := []byte(`<?xml version="1.0" encoding="UTF-8" ?>
<config version="1.7" xmlns="http://www.ipc.com/ver10">
<alarmStatusInfo>
<motionAlarm type="boolean" id="1">true</motionAlarm>
<vehiceAlarm type="boolean" id="1">false</vehiceAlarm>
</alarmStatusInfo>
<dataTime><![CDATA[30/10/2026 16:32:28]]></dataTime>
<deviceInfo>
<deviceName><![CDATA[Cam-Entrada]]></deviceName>
<deviceNo.><![CDATA[3]]></deviceNo.>
<ipAddress><![CDATA[10.0.15.108]]></ipAddress>
</deviceInfo>
</config>`)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(xmlPayload))
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.RemoteAddr = "10.0.15.108:54321"

	event, err := parser.Parse(req, xmlPayload, time.Now())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if event.EventType != "motion" {
		t.Errorf("EventType = %q, want motion", event.EventType)
	}
	if event.ChannelID == nil || *event.ChannelID != 3 {
		t.Errorf("ChannelID = %v, want 3", event.ChannelID)
	}
}

func TestParserProvisionISRAlarmClearIgnored(t *testing.T) {
	parser := NewParser()
	xmlPayload := []byte(`<?xml version="1.0" encoding="UTF-8" ?>
<config version="1.7" xmlns="http://www.ipc.com/ver10">
<alarmStatusInfo>
<motionAlarm type="boolean" id="1">false</motionAlarm>
<vehiceAlarm type="boolean" id="1">false</vehiceAlarm>
</alarmStatusInfo>
<deviceInfo>
<deviceName><![CDATA[Cam-Entrada]]></deviceName>
</deviceInfo>
</config>`)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(xmlPayload))
	req.Header.Set("Content-Type", "application/xml")
	req.RemoteAddr = "10.0.15.108:54321"

	_, err := parser.Parse(req, xmlPayload, time.Now())
	if err == nil {
		t.Fatalf("expected error for alarm clear with no plate, got nil")
	}
}

func TestParserPlateBoxXML(t *testing.T) {
	parser := NewParser()
	xmlPayload := []byte(`<?xml version="1.0" encoding="UTF-8" ?>
<config version="1.7" xmlns="http://www.ipc.com/ver10">
<plate>AF123BC</plate>
<rect x="192" y="108" w="384" h="216" picWidth="1920" picHeight="1080"/>
<deviceInfo>
<deviceName><![CDATA[Cam-LPR]]></deviceName>
<deviceNo.><![CDATA[1]]></deviceNo.>
<ipAddress><![CDATA[10.0.15.107]]></ipAddress>
</deviceInfo>
</config>`)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(xmlPayload))
	req.Header.Set("Content-Type", "application/xml")
	req.RemoteAddr = "10.0.15.107:54321"

	event, err := parser.Parse(req, xmlPayload, time.Now())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if event.Plate != "AF123BC" {
		t.Errorf("Plate = %q, want AF123BC", event.Plate)
	}
	if len(event.PlateBox) != 4 {
		t.Fatalf("PlateBox length = %d, want 4", len(event.PlateBox))
	}
	// [top, left, height, width]
	// top = 108/1080 = 0.1, left = 192/1920 = 0.1, height = 216/1080 = 0.2, width = 384/1920 = 0.2
	want := []float32{0.1, 0.1, 0.2, 0.2}
	for i := range want {
		diff := event.PlateBox[i] - want[i]
		if diff < -0.01 || diff > 0.01 {
			t.Errorf("PlateBox[%d] = %v, want %v", i, event.PlateBox[i], want[i])
		}
	}
}

func TestParserPlateBoxJSON(t *testing.T) {
	parser := NewParser()
	jsonPayload := []byte(`{
		"plate": "AE456CD",
		"camera_id": "cam-gate",
		"rect": {
			"x": 0.15,
			"y": 0.25,
			"w": 0.30,
			"h": 0.10
		}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/lpr/event", bytes.NewReader(jsonPayload))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.15.107:54321"

	event, err := parser.Parse(req, jsonPayload, time.Now())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(event.PlateBox) != 4 {
		t.Fatalf("PlateBox length = %d, want 4", len(event.PlateBox))
	}
	// [top, left, height, width] -> [0.25, 0.15, 0.10, 0.30]
	want := []float32{0.25, 0.15, 0.10, 0.30}
	for i := range want {
		diff := event.PlateBox[i] - want[i]
		if diff < -0.01 || diff > 0.01 {
			t.Errorf("PlateBox[%d] = %v, want %v", i, event.PlateBox[i], want[i])
		}
	}
}

func TestParserFaceAnalyticsXML(t *testing.T) {
	parser := NewParser()
	xmlPayload := []byte(`<?xml version="1.0" encoding="UTF-8" ?>
<config version="1.7" xmlns="http://www.ipc.com/ver10">
<alarmStatusInfo>
<faceAlarm type="boolean" id="1">true</faceAlarm>
</alarmStatusInfo>
<personName><![CDATA[Maria Gomez]]></personName>
<similarity>94.5</similarity>
<faceID>face-77</faceID>
<targetBase64Data><![CDATA[/9j/4FACE_CROP]]></targetBase64Data>
<sourceBase64Data><![CDATA[/9j/4FULL_SCENE]]></sourceBase64Data>
<deviceInfo>
<deviceName><![CDATA[Cam-Facial]]></deviceName>
<deviceNo.><![CDATA[2]]></deviceNo.>
<ipAddress><![CDATA[10.0.15.109]]></ipAddress>
</deviceInfo>
</config>`)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(xmlPayload))
	req.Header.Set("Content-Type", "application/xml")
	req.RemoteAddr = "10.0.15.109:54321"

	event, err := parser.Parse(req, xmlPayload, time.Now())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if event.EventType != "face" {
		t.Errorf("EventType = %q, want face", event.EventType)
	}
	if event.PersonName == nil || *event.PersonName != "Maria Gomez" {
		t.Errorf("PersonName = %v, want 'Maria Gomez'", event.PersonName)
	}
	if event.FaceScore == nil || *event.FaceScore < 0.94 || *event.FaceScore > 0.95 {
		t.Errorf("FaceScore = %v, want ~0.945", event.FaceScore)
	}
	if event.FaceID == nil || *event.FaceID != "face-77" {
		t.Errorf("FaceID = %v, want 'face-77'", event.FaceID)
	}
	if event.PlateCrop == nil || *event.PlateCrop != "/9j/4FACE_CROP" {
		t.Errorf("PlateCrop = %v, want /9j/4FACE_CROP", event.PlateCrop)
	}
}

func TestParserPeopleCountingXML(t *testing.T) {
	parser := NewParser()
	xmlPayload := []byte(`<?xml version="1.0" encoding="UTF-8" ?>
<config version="1.7" xmlns="http://www.ipc.com/ver10">
<alarmStatusInfo>
<peopleCountAlarm type="boolean" id="1">true</peopleCountAlarm>
</alarmStatusInfo>
<enterCount>14</enterCount>
<exitCount>9</exitCount>
<passCount>23</passCount>
<deviceInfo>
<deviceName><![CDATA[Cam-Pasillo]]></deviceName>
<deviceNo.><![CDATA[4]]></deviceNo.>
<ipAddress><![CDATA[10.0.15.110]]></ipAddress>
</deviceInfo>
</config>`)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(xmlPayload))
	req.Header.Set("Content-Type", "application/xml")
	req.RemoteAddr = "10.0.15.110:54321"

	event, err := parser.Parse(req, xmlPayload, time.Now())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if event.EventType != "people_counting" {
		t.Errorf("EventType = %q, want people_counting", event.EventType)
	}
	if event.EnterCount == nil || *event.EnterCount != 14 {
		t.Errorf("EnterCount = %v, want 14", event.EnterCount)
	}
	if event.ExitCount == nil || *event.ExitCount != 9 {
		t.Errorf("ExitCount = %v, want 9", event.ExitCount)
	}
	if event.PassCount == nil || *event.PassCount != 23 {
		t.Errorf("PassCount = %v, want 23", event.PassCount)
	}
}

func TestParserTVTCountPlaceholderPlate(t *testing.T) {
	parser := NewParser()
	xmlPayload := []byte(`<?xml version="1.0" encoding="UTF-8" ?>
<config version="1.7" xmlns="http://www.ipc.com/ver10">
<plate>COUNT</plate>
<enterCount>5</enterCount>
<exitCount>2</exitCount>
<deviceInfo>
<deviceName><![CDATA[Cam-Conteo]]></deviceName>
<ipAddress><![CDATA[10.0.15.111]]></ipAddress>
</deviceInfo>
</config>`)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(xmlPayload))
	req.Header.Set("Content-Type", "application/xml")
	req.RemoteAddr = "10.0.15.111:54321"

	event, err := parser.Parse(req, xmlPayload, time.Now())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if event.Plate != "" {
		t.Errorf("Plate = %q, want empty", event.Plate)
	}
	if event.EventType != "people_counting" {
		t.Errorf("EventType = %q, want people_counting", event.EventType)
	}
	if event.EnterCount == nil || *event.EnterCount != 5 {
		t.Errorf("EnterCount = %v, want 5", event.EnterCount)
	}
}


