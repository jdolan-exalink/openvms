package provisionlpr

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ErrEmptyPayload    = errors.New("empty LPR payload")
	ErrNoPlateFound    = errors.New("no license plate detected in payload")
	labeledPlateRegex  = regexp.MustCompile(`(?i)(?:plate|patente|matricula|licence|license)[^\w]*([A-Z0-9]{5,10})`)
	alphaNumPlateRegex = regexp.MustCompile(`\b([A-Z]{1,4}[0-9]{1,4}[A-Z0-9]{1,4})\b`)
)

// Parser parses incoming HTTP requests from Provision-ISR / TVT cameras into normalized LPREvent.
type Parser struct{}

// NewParser creates a new Parser instance.
func NewParser() *Parser {
	return &Parser{}
}

// Parse extracts the normalized event from the HTTP request and its body bytes.
func (p *Parser) Parse(r *http.Request, body []byte, receivedAt time.Time) (LPREvent, error) {
	if len(bytes.TrimSpace(body)) == 0 && r.URL.RawQuery == "" {
		return LPREvent{}, ErrEmptyPayload
	}

	endpoint := r.URL.Path
	event := LPREvent{
		EventType:    "lpr",
		Source:       "provision_isr",
		ReceivedTime: receivedAt,
		EventTime:    receivedAt,
		RawEndpoint:  &endpoint,
	}

	// Capture client IP
	event.CameraIP = extractClientIP(r)

	// Check content type
	contentType := r.Header.Get("Content-Type")
	mediaType, params, _ := mime.ParseMediaType(contentType)

	var parsed bool
	var err error

	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		parsed, err = p.parseMultipart(params["boundary"], body, &event)
	case mediaType == "application/json" || looksLikeJSON(body):
		parsed, err = p.parseJSON(body, &event)
	case mediaType == "application/xml" || mediaType == "text/xml" || looksLikeXML(body):
		parsed, err = p.parseXML(body, &event)
	case mediaType == "application/x-www-form-urlencoded" || r.URL.RawQuery != "":
		parsed, err = p.parseForm(r, body, &event)
	default:
		// Attempt JSON first, then XML, then Form
		if p.tryParseAny(body, &event) {
			parsed = true
		}
	}

	if err != nil && !parsed {
		return LPREvent{}, err
	}

	// If plate not found yet, try regex scan of the body as a fallback
	if event.Plate == "" {
		if match := labeledPlateRegex.FindSubmatch(body); len(match) > 1 {
			event.Plate = NormalizePlate(string(match[1]))
		} else if match := alphaNumPlateRegex.FindSubmatch(body); len(match) > 1 {
			event.Plate = NormalizePlate(string(match[1]))
		}
	}

	if event.Plate == "" {
		return LPREvent{}, ErrNoPlateFound
	}

	event.Plate = NormalizePlate(event.Plate)
	if event.CameraID == "" {
		if event.CameraIP != "" {
			event.CameraID = event.CameraIP
		} else {
			event.CameraID = "unknown-provision"
		}
	}

	return event, nil
}

func (p *Parser) parseMultipart(boundary string, body []byte, ev *LPREvent) (bool, error) {
	if boundary == "" {
		return false, errors.New("missing boundary in multipart content-type")
	}
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	foundPlate := false

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return foundPlate, err
		}
		partData, err := io.ReadAll(io.LimitReader(part, 10<<20))
		if err != nil {
			continue
		}

		partType := part.Header.Get("Content-Type")
		pMediaType, _, _ := mime.ParseMediaType(partType)

		isImage := strings.HasPrefix(pMediaType, "image/") ||
			strings.HasSuffix(strings.ToLower(part.FileName()), ".jpg") ||
			strings.HasSuffix(strings.ToLower(part.FileName()), ".jpeg") ||
			strings.HasSuffix(strings.ToLower(part.FileName()), ".png") ||
			bytes.HasPrefix(partData, []byte("\xFF\xD8\xFF")) ||
			bytes.HasPrefix(partData, []byte("\x89PNG"))

		if isImage {
			encoded := base64.StdEncoding.EncodeToString(partData)
			partName := strings.ToLower(part.FileName())
			if partName == "" {
				partName = strings.ToLower(part.FormName())
			}
			if strings.Contains(partName, "cut") || strings.Contains(partName, "crop") || strings.Contains(partName, "plate") {
				ev.PlateCrop = &encoded
			} else {
				ev.PanoramicImage = &encoded
			}
		} else {
			if p.tryParseAny(partData, ev) {
				foundPlate = true
			}
		}
	}
	return foundPlate, nil
}

func (p *Parser) parseJSON(data []byte, ev *LPREvent) (bool, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return false, err
	}
	extractFromMap(raw, ev)
	return ev.Plate != "", nil
}

func (p *Parser) parseXML(data []byte, ev *LPREvent) (bool, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := token.(xml.StartElement); ok {
			name := strings.ToLower(se.Name.Local)
			switch {
			case isPlateKey(name):
				var plate string
				if err := decoder.DecodeElement(&plate, &se); err == nil {
					ev.Plate = plate
				}
			case isTimeKey(name):
				var rawTime string
				if err := decoder.DecodeElement(&rawTime, &se); err == nil {
					if t, err := parseTime(rawTime); err == nil {
						ev.EventTime = t
					}
				}
			case isCameraKey(name):
				var camID string
				if err := decoder.DecodeElement(&camID, &se); err == nil && camID != "" {
					ev.CameraID = camID
				}
			case isConfidenceKey(name):
				var conf float64
				if err := decoder.DecodeElement(&conf, &se); err == nil {
					ev.Confidence = &conf
				}
			case isListStatusKey(name):
				var listStatus string
				if err := decoder.DecodeElement(&listStatus, &se); err == nil && listStatus != "" {
					ev.ListStatus = &listStatus
				}
			case isCropKey(name):
				var img string
				if err := decoder.DecodeElement(&img, &se); err == nil && img != "" {
					ev.PlateCrop = &img
				}
			case isPanoramaKey(name):
				var img string
				if err := decoder.DecodeElement(&img, &se); err == nil && img != "" {
					ev.PanoramicImage = &img
				}
			case isColorKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && s != "" {
					ev.VehicleColor = &s
				}
			case isBrandKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && s != "" {
					ev.VehicleBrand = &s
				}
			case isModelKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && s != "" {
					ev.VehicleModel = &s
				}
			case isTypeKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && s != "" {
					ev.VehicleType = &s
				}
			case isSpeedKey(name):
				var sp float64
				if err := decoder.DecodeElement(&sp, &se); err == nil {
					ev.Speed = &sp
				}
			case isDirectionKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && s != "" {
					ev.Direction = &s
				}
			}
		}
	}
	return ev.Plate != "", nil
}

func (p *Parser) parseForm(r *http.Request, body []byte, ev *LPREvent) (bool, error) {
	values, err := url.ParseQuery(string(body))
	if err != nil || len(values) == 0 {
		values = r.URL.Query()
	}
	for k, v := range values {
		if len(v) == 0 {
			continue
		}
		val := v[0]
		kLower := strings.ToLower(k)
		switch {
		case isPlateKey(kLower):
			ev.Plate = val
		case isTimeKey(kLower):
			if t, err := parseTime(val); err == nil {
				ev.EventTime = t
			}
		case isCameraKey(kLower):
			ev.CameraID = val
		case isConfidenceKey(kLower):
			if c, err := strconv.ParseFloat(val, 64); err == nil {
				ev.Confidence = &c
			}
		case isListStatusKey(kLower):
			ev.ListStatus = &val
		case isColorKey(kLower):
			ev.VehicleColor = &val
		case isBrandKey(kLower):
			ev.VehicleBrand = &val
		case isModelKey(kLower):
			ev.VehicleModel = &val
		case isTypeKey(kLower):
			ev.VehicleType = &val
		case isSpeedKey(kLower):
			if sp, err := strconv.ParseFloat(val, 64); err == nil {
				ev.Speed = &sp
			}
		case isDirectionKey(kLower):
			ev.Direction = &val
		}
	}
	return ev.Plate != "", nil
}

func (p *Parser) tryParseAny(data []byte, ev *LPREvent) bool {
	if looksLikeJSON(data) {
		if ok, _ := p.parseJSON(data, ev); ok {
			return true
		}
	}
	if looksLikeXML(data) {
		if ok, _ := p.parseXML(data, ev); ok {
			return true
		}
	}
	if values, err := url.ParseQuery(string(data)); err == nil && len(values) > 0 {
		for k, v := range values {
			if len(v) > 0 && isPlateKey(strings.ToLower(k)) {
				ev.Plate = v[0]
				return true
			}
		}
	}
	return false
}

func extractFromMap(m map[string]any, ev *LPREvent) {
	for k, v := range m {
		kLower := strings.ToLower(k)
		switch val := v.(type) {
		case string:
			switch {
			case isPlateKey(kLower):
				ev.Plate = val
			case isTimeKey(kLower):
				if t, err := parseTime(val); err == nil {
					ev.EventTime = t
				}
			case isCameraKey(kLower):
				ev.CameraID = val
			case isListStatusKey(kLower):
				ev.ListStatus = &val
			case isCropKey(kLower):
				ev.PlateCrop = &val
			case isPanoramaKey(kLower):
				ev.PanoramicImage = &val
			case isColorKey(kLower):
				ev.VehicleColor = &val
			case isBrandKey(kLower):
				ev.VehicleBrand = &val
			case isModelKey(kLower):
				ev.VehicleModel = &val
			case isTypeKey(kLower):
				ev.VehicleType = &val
			case isDirectionKey(kLower):
				ev.Direction = &val
			}
		case float64:
			if isConfidenceKey(kLower) {
				c := val
				ev.Confidence = &c
			} else if isSpeedKey(kLower) {
				s := val
				ev.Speed = &s
			}
		case map[string]any:
			extractFromMap(val, ev)
		case []any:
			for _, item := range val {
				if itemMap, ok := item.(map[string]any); ok {
					extractFromMap(itemMap, ev)
				}
			}
		}
	}
}

func isPlateKey(k string) bool {
	return k == "plate" || k == "plate_num" || k == "plateno" || k == "license_plate" ||
		k == "licenseplate" || k == "platenumber" || k == "carnum" || k == "car_num"
}

func isTimeKey(k string) bool {
	return k == "time" || k == "event_time" || k == "eventtime" || k == "timestamp" ||
		k == "snap_time" || k == "snaptime" || k == "capture_time" || k == "datetime"
}

func isCameraKey(k string) bool {
	return k == "camera_id" || k == "cameraid" || k == "device_id" || k == "deviceid" ||
		k == "dev_id" || k == "channel" || k == "cam_id" || k == "cameraname" || k == "camera_name"
}

func isConfidenceKey(k string) bool {
	return k == "confidence" || k == "accuracy" || k == "score" || k == "plate_confidence"
}

func isListStatusKey(k string) bool {
	return k == "list_status" || k == "liststatus" || k == "plate_type" || k == "platetype" ||
		k == "group" || k == "list_type" || k == "listtype" || k == "alarm_type"
}

func isCropKey(k string) bool {
	return k == "plate_crop" || k == "platecrop" || k == "cutout" || k == "close_up" || k == "plate_image"
}

func isPanoramaKey(k string) bool {
	return k == "panoramic_image" || k == "panorama" || k == "scene_image" || k == "scene_pic"
}

func isColorKey(k string) bool {
	return k == "color" || k == "vehicle_color" || k == "vehiclecolor" || k == "car_color" ||
		k == "carcolor" || k == "plate_color" || k == "color_name"
}

func isBrandKey(k string) bool {
	return k == "brand" || k == "vehicle_brand" || k == "vehiclebrand" || k == "car_brand" ||
		k == "carbrand" || k == "make" || k == "carmake" || k == "marca"
}

func isModelKey(k string) bool {
	return k == "model" || k == "vehicle_model" || k == "vehiclemodel" || k == "car_model" ||
		k == "carmodel" || k == "modelo"
}

func isTypeKey(k string) bool {
	return k == "vehicle_type" || k == "vehicletype" || k == "car_type" || k == "cartype" ||
		k == "tipo" || k == "type"
}

func isSpeedKey(k string) bool {
	return k == "speed" || k == "vehicle_speed" || k == "vehiclespeed" || k == "velocidad"
}

func isDirectionKey(k string) bool {
	return k == "direction" || k == "heading" || k == "direccion"
}

func looksLikeJSON(b []byte) bool {
	t := bytes.TrimSpace(b)
	return len(t) > 1 && ((t[0] == '{' && t[len(t)-1] == '}') || (t[0] == '[' && t[len(t)-1] == ']'))
}

func looksLikeXML(b []byte) bool {
	t := bytes.TrimSpace(b)
	return len(t) > 1 && t[0] == '<' && t[len(t)-1] == '>'
}

// extractClientIP is the address of the connection's peer, nothing else. Cameras connect to the
// agent directly, so a forwarding header can only come from whoever sent the request: trusting
// X-Forwarded-For would let any host claim to be a registered camera.
func extractClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Unmap().String()
	}
	return host
}

func parseTime(raw string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006/01/02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t, nil
		}
	}
	// Try epoch seconds or milliseconds
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if n > 1e12 {
			return time.UnixMilli(n), nil
		}
		return time.Unix(n, 0), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized time format %q", raw)
}
