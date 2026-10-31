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

	provisionPlateColorMap = map[string]string{
		"0": "Desconocido",
		"1": "Amarillo",
		"2": "Azul",
		"3": "Negro",
		"4": "Blanco",
		"5": "Verde",
	}
	provisionVehicleColorMap = map[string]string{
		"0": "Desconocido",
		"1": "Blanco",
		"2": "Gris",
		"3": "Amarillo",
		"4": "Rosa",
		"5": "Rojo",
		"6": "Verde",
		"7": "Azul",
		"8": "Negro",
	}
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

	if event.Plate != "" && !isPlaceholderPlate(event.Plate) {
		event.Plate = NormalizePlate(event.Plate)
		if event.EventType == "" {
			event.EventType = "lpr"
		}
	} else if strings.EqualFold(strings.TrimSpace(event.Plate), "COUNT") || event.EnterCount != nil || event.ExitCount != nil || event.PassCount != nil {
		event.Plate = ""
		if event.EventType == "" {
			event.EventType = "people_counting"
		}
	} else if event.EventType != "" {
		event.Plate = ""
	} else {
		return LPREvent{}, ErrNoPlateFound
	}
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
	activeAlarms := make(map[string]bool)
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := token.(xml.StartElement); ok {
			name := strings.ToLower(se.Name.Local)
			if ev.ChannelID == nil {
				if idAttr := getXMLAttr(se.Attr, "id"); idAttr != "" {
					if n, err := strconv.Atoi(idAttr); err == nil && n >= 0 {
						ev.ChannelID = &n
					}
				}
			}
			switch {
			case isPeopleCountAlarmKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && isXMLBoolTrue(s) {
					activeAlarms["people_counting"] = true
				}
			case isEnterCountKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && s != "" {
					if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
						ev.EnterCount = &n
					}
				}
			case isExitCountKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && s != "" {
					if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
						ev.ExitCount = &n
					}
				}
			case isPassCountKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && s != "" {
					if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
						ev.PassCount = &n
					}
				}
			case isPersonNameKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && s != "" {
					ev.PersonName = &s
				}
			case isFaceScoreKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && s != "" {
					if score, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
						if score > 1.0 {
							score = score / 100.0
						}
						ev.FaceScore = &score
					}
				}
			case isFaceIDKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && s != "" {
					ev.FaceID = &s
				}
			case isRectKey(name):
				if box := parseXMLRect(decoder, se); len(box) == 4 {
					ev.PlateBox = box
				}
			case isLineCrossingKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && isXMLBoolTrue(s) {
					activeAlarms["line_crossing"] = true
				}
			case isIntrusionKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && isXMLBoolTrue(s) {
					activeAlarms["intrusion"] = true
				}
			case isFaceKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && isXMLBoolTrue(s) {
					activeAlarms["face"] = true
				}
			case isHumanKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && isXMLBoolTrue(s) {
					activeAlarms["human"] = true
				}
			case isVehicleAlarmKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && isXMLBoolTrue(s) {
					activeAlarms["vehicle"] = true
				}
			case isTamperKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && isXMLBoolTrue(s) {
					activeAlarms["tamper"] = true
				}
			case isMotionKey(name):
				var s string
				if err := decoder.DecodeElement(&s, &se); err == nil && isXMLBoolTrue(s) {
					activeAlarms["motion"] = true
				}
			case isChannelKey(name):
				var chStr string
				if err := decoder.DecodeElement(&chStr, &se); err == nil && chStr != "" {
					if n, err := strconv.Atoi(strings.TrimSpace(chStr)); err == nil && n >= 0 {
						ev.ChannelID = &n
					}
				}
			case isPlateKey(name):
				var plate string
				if err := decoder.DecodeElement(&plate, &se); err == nil && plate != "" {
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
				countAttr := getXMLAttr(se.Attr, "count")
				if countAttr != "" {
					if c, err := strconv.ParseFloat(countAttr, 64); err == nil {
						c = max(0.001, min(1.0, c/100.0))
						ev.Confidence = &c
					}
				} else {
					var conf float64
					if err := decoder.DecodeElement(&conf, &se); err == nil {
						ev.Confidence = &conf
					}
				}
			case isListStatusKey(name):
				var listStatus string
				if err := decoder.DecodeElement(&listStatus, &se); err == nil && listStatus != "" {
					ev.ListStatus = &listStatus
				}
			case isCropKey(name):
				var img string
				if err := decoder.DecodeElement(&img, &se); err == nil {
					img = strings.TrimSpace(img)
					if img != "" {
						ev.PlateCrop = &img
					}
				}
			case isPanoramaKey(name):
				var img string
				if err := decoder.DecodeElement(&img, &se); err == nil {
					img = strings.TrimSpace(img)
					if img != "" {
						ev.PanoramicImage = &img
					}
				}
			case isColorKey(name):
				countAttr := getXMLAttr(se.Attr, "count")
				if countAttr != "" {
					if strings.Contains(name, "plate") {
						if c, ok := provisionPlateColorMap[countAttr]; ok {
							// Kept if metadata or other field needs it
							_ = c
						}
					} else {
						if c, ok := provisionVehicleColorMap[countAttr]; ok {
							ev.VehicleColor = &c
						}
					}
				} else {
					var s string
					if err := decoder.DecodeElement(&s, &se); err == nil && s != "" {
						ev.VehicleColor = &s
					}
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
	alarmPriority := []string{"line_crossing", "intrusion", "face", "vehicle", "human", "people_counting", "tamper", "motion"}
	for _, p := range alarmPriority {
		if activeAlarms[p] {
			ev.EventType = p
			break
		}
	}
	return ev.Plate != "" || ev.EventType != "", nil
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
			case isPersonNameKey(kLower):
				if val != "" {
					ev.PersonName = &val
				}
			case isFaceIDKey(kLower):
				if val != "" {
					ev.FaceID = &val
				}
			case isFaceScoreKey(kLower):
				if score, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
					if score > 1.0 {
						score = score / 100.0
					}
					ev.FaceScore = &score
				}
			case isEnterCountKey(kLower):
				if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
					ev.EnterCount = &n
				}
			case isExitCountKey(kLower):
				if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
					ev.ExitCount = &n
				}
			case isPassCountKey(kLower):
				if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
					ev.PassCount = &n
				}
			case isCropKey(kLower):
				val = strings.TrimSpace(val)
				if val != "" {
					ev.PlateCrop = &val
				}
			case isPanoramaKey(kLower):
				val = strings.TrimSpace(val)
				if val != "" {
					ev.PanoramicImage = &val
				}
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
			switch {
			case isConfidenceKey(kLower):
				c := val
				ev.Confidence = &c
			case isSpeedKey(kLower):
				s := val
				ev.Speed = &s
			case isFaceScoreKey(kLower):
				score := val
				if score > 1.0 {
					score = score / 100.0
				}
				ev.FaceScore = &score
			case isEnterCountKey(kLower):
				n := int(val)
				ev.EnterCount = &n
			case isExitCountKey(kLower):
				n := int(val)
				ev.ExitCount = &n
			case isPassCountKey(kLower):
				n := int(val)
				ev.PassCount = &n
			}
		case bool:
			if isPeopleCountAlarmKey(kLower) && val {
				ev.EventType = "people_counting"
			}
		case map[string]any:
			if isRectKey(kLower) {
				if box := extractRectFromMap(val); len(box) == 4 {
					ev.PlateBox = box
				}
			} else {
				extractFromMap(val, ev)
			}
		case []any:
			if isRectKey(kLower) {
				if box := extractRectFromSlice(val); len(box) == 4 {
					ev.PlateBox = box
				}
			} else {
				for _, item := range val {
					if itemMap, ok := item.(map[string]any); ok {
						extractFromMap(itemMap, ev)
					}
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
		k == "snap_time" || k == "snaptime" || k == "capture_time" || k == "datetime" ||
		k == "datatime"
}

func isLineCrossingKey(k string) bool {
	return k == "linecrossalarm" || k == "linecross" || k == "linecrossing" ||
		k == "tripwirealarm" || k == "tripwire"
}

func isIntrusionKey(k string) bool {
	return k == "intrusionalarm" || k == "intrusion" || k == "fielddetector" ||
		k == "areaintrusion" || k == "areaintrusionalarm"
}

func isFaceKey(k string) bool {
	return k == "facealarm" || k == "facedetectionalarm" || k == "facematchalarm" ||
		k == "face" || k == "facedetection"
}

func isHumanKey(k string) bool {
	return k == "humanalarm" || k == "peoplealarm" || k == "personalarm" ||
		k == "pedestrianalarm" || k == "human" || k == "person"
}

func isVehicleAlarmKey(k string) bool {
	return k == "vehicealarm" || k == "vehiclealarm" || k == "caralarm" ||
		k == "vehice" || k == "vehicle"
}

func isMotionKey(k string) bool {
	return k == "motionalarm" || k == "motion"
}

func isTamperKey(k string) bool {
	return k == "tamperalarm" || k == "scenedetectionalarm" || k == "tamper"
}

func isChannelKey(k string) bool {
	k = strings.TrimSuffix(k, ".")
	return k == "deviceno" || k == "channelid" || k == "channel_id" || k == "chid" ||
		k == "channelno" || k == "channel_no" || k == "channel"
}

func isXMLBoolTrue(val string) bool {
	v := strings.ToLower(strings.TrimSpace(val))
	return v == "true" || v == "1" || v == "yes" || v == "on"
}

func isCameraKey(k string) bool {
	return k == "camera_id" || k == "cameraid" || k == "device_id" || k == "deviceid" ||
		k == "dev_id" || k == "channel" || k == "cam_id" || k == "cameraname" || k == "camera_name" ||
		k == "devicename" || k == "device_name"
}

func isConfidenceKey(k string) bool {
	return k == "confidence" || k == "accuracy" || k == "score" || k == "plate_confidence" ||
		k == "plateconfidence"
}

func isListStatusKey(k string) bool {
	return k == "list_status" || k == "liststatus" || k == "plate_type" || k == "platetype" ||
		k == "group" || k == "list_type" || k == "listtype" || k == "alarm_type"
}

func isCropKey(k string) bool {
	return k == "plate_crop" || k == "platecrop" || k == "cutout" || k == "close_up" || k == "plate_image" ||
		k == "targetbase64data" || k == "target_base64_data"
}

func isPanoramaKey(k string) bool {
	return k == "panoramic_image" || k == "panoramicimage" || k == "panorama" ||
		k == "scene_image" || k == "sceneimage" || k == "scene_pic" || k == "scenepic" ||
		k == "sourcebase64data" || k == "source_base64_data" ||
		k == "scenebase64data" || k == "scene_base64_data" ||
		k == "backgroundbase64data" || k == "background_base64_data" ||
		k == "bkgbase64data" || k == "bkg_base64_data" ||
		k == "sourceimage" || k == "source_image" || k == "sourcepic" || k == "source_pic" ||
		k == "fullimage" || k == "full_image" || k == "fullpic" || k == "full_pic" ||
		k == "overviewimage" || k == "overview_image" || k == "overviewpic" || k == "overview_pic" ||
		k == "vehicleimage" || k == "vehicle_image" || k == "vehiclepic" || k == "vehicle_pic" ||
		k == "carimage" || k == "car_image" || k == "carpic" || k == "car_pic" ||
		k == "bkgimage" || k == "bkg_image" || k == "bkgpic" || k == "bkg_pic" ||
		k == "originalimage" || k == "original_image" ||
		k == "originalbase64data" || k == "original_base64_data"
}

func isPlaceholderPlate(p string) bool {
	p = strings.ToUpper(strings.TrimSpace(p))
	return p == "COUNT" || p == "NOPLATE" || p == "UNKNOWN" || p == "NONE" || p == "NULL"
}

func isColorKey(k string) bool {
	return k == "color" || k == "vehicle_color" || k == "vehiclecolor" || k == "car_color" ||
		k == "carcolor" || k == "plate_color" || k == "color_name" || k == "vehicecolor" || k == "vehice_color"
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

func isRectKey(k string) bool {
	return k == "rect" || k == "plate_rect" || k == "platerect" || k == "location" ||
		k == "bounding_box" || k == "bbox" || k == "target_rect" || k == "targetrect"
}

func isPersonNameKey(k string) bool {
	return k == "personname" || k == "person_name" || k == "facename" || k == "face_name" ||
		k == "targetname" || k == "target_name"
}

func isFaceScoreKey(k string) bool {
	return k == "facescore" || k == "face_score" || k == "similarity" || k == "matchscore" || k == "match_score"
}

func isFaceIDKey(k string) bool {
	return k == "faceid" || k == "face_id" || k == "personid" || k == "person_id"
}

func isPeopleCountAlarmKey(k string) bool {
	return k == "peoplecountalarm" || k == "peoplecount" || k == "targetcountalarm" ||
		k == "countalarm" || k == "flowcountalarm" || k == "flowcount"
}

func isEnterCountKey(k string) bool {
	return k == "entercount" || k == "enter_count" || k == "incount" || k == "in_count"
}

func isExitCountKey(k string) bool {
	return k == "exitcount" || k == "exit_count" || k == "outcount" || k == "out_count"
}

func isPassCountKey(k string) bool {
	return k == "passcount" || k == "pass_count" || k == "totalcount" || k == "total_count"
}

func parseXMLRect(decoder *xml.Decoder, se xml.StartElement) []float32 {
	var x, y, w, h, left, top, right, bottom float64
	var picW, picH float64
	var hasXY, hasLTRB bool

	for _, a := range se.Attr {
		k := strings.ToLower(a.Name.Local)
		val, err := strconv.ParseFloat(strings.TrimSpace(a.Value), 64)
		if err != nil {
			continue
		}
		switch k {
		case "x":
			x = val
			hasXY = true
		case "y":
			y = val
			hasXY = true
		case "w", "width":
			w = val
			hasXY = true
		case "h", "height":
			h = val
			hasXY = true
		case "left":
			left = val
			hasLTRB = true
		case "top":
			top = val
			hasLTRB = true
		case "right":
			right = val
			hasLTRB = true
		case "bottom":
			bottom = val
			hasLTRB = true
		case "picwidth", "pic_width", "imgwidth", "img_width":
			picW = val
		case "picheight", "pic_height", "imgheight", "img_height":
			picH = val
		}
	}

	if hasXY || hasLTRB {
		return normalizeBox(x, y, w, h, left, top, right, bottom, picW, picH, hasXY, hasLTRB)
	}

	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			tag := strings.ToLower(t.Name.Local)
			var text string
			if err := decoder.DecodeElement(&text, &t); err == nil {
				depth--
				if val, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil {
					switch tag {
					case "x":
						x = val
						hasXY = true
					case "y":
						y = val
						hasXY = true
					case "w", "width":
						w = val
						hasXY = true
					case "h", "height":
						h = val
						hasXY = true
					case "left":
						left = val
						hasLTRB = true
					case "top":
						top = val
						hasLTRB = true
					case "right":
						right = val
						hasLTRB = true
					case "bottom":
						bottom = val
						hasLTRB = true
					case "picwidth", "pic_width", "imgwidth", "img_width":
						picW = val
					case "picheight", "pic_height", "imgheight", "img_height":
						picH = val
					}
				}
			}
		case xml.EndElement:
			depth--
		}
	}

	if hasXY || hasLTRB {
		return normalizeBox(x, y, w, h, left, top, right, bottom, picW, picH, hasXY, hasLTRB)
	}
	return nil
}

func extractRectFromMap(m map[string]any) []float32 {
	var x, y, w, h, left, top, right, bottom float64
	var picW, picH float64
	var hasXY, hasLTRB bool

	for k, v := range m {
		kLower := strings.ToLower(k)
		val, ok := toFloat(v)
		if !ok {
			continue
		}
		switch kLower {
		case "x":
			x = val
			hasXY = true
		case "y":
			y = val
			hasXY = true
		case "w", "width":
			w = val
			hasXY = true
		case "h", "height":
			h = val
			hasXY = true
		case "left":
			left = val
			hasLTRB = true
		case "top":
			top = val
			hasLTRB = true
		case "right":
			right = val
			hasLTRB = true
		case "bottom":
			bottom = val
			hasLTRB = true
		case "picwidth", "pic_width", "imgwidth", "img_width":
			picW = val
		case "picheight", "pic_height", "imgheight", "img_height":
			picH = val
		}
	}
	return normalizeBox(x, y, w, h, left, top, right, bottom, picW, picH, hasXY, hasLTRB)
}

func extractRectFromSlice(s []any) []float32 {
	if len(s) != 4 {
		return nil
	}
	var nums [4]float64
	for i, v := range s {
		n, ok := toFloat(v)
		if !ok {
			return nil
		}
		nums[i] = n
	}
	top := nums[0]
	left := nums[1]
	h := nums[2]
	w := nums[3]
	if nums[2] > nums[0] && nums[3] > nums[1] && nums[2] <= 1.0 && nums[3] <= 1.0 {
		h = nums[2] - nums[0]
		w = nums[3] - nums[1]
	}
	return normalizeBox(left, top, w, h, 0, 0, 0, 0, 0, 0, true, false)
}

func normalizeBox(x, y, w, h, left, top, right, bottom, picW, picH float64, hasXY, hasLTRB bool) []float32 {
	var top0, left0, h0, w0 float64
	if hasLTRB {
		top0 = top
		left0 = left
		h0 = bottom - top
		w0 = right - left
	} else if hasXY {
		top0 = y
		left0 = x
		h0 = h
		w0 = w
	} else {
		return nil
	}

	if h0 <= 0 || w0 <= 0 {
		return nil
	}

	var topNorm, leftNorm, heightNorm, widthNorm float64
	if picW > 0 && picH > 0 {
		topNorm = top0 / picH
		leftNorm = left0 / picW
		heightNorm = h0 / picH
		widthNorm = w0 / picW
	} else if top0 > 1.0 || left0 > 1.0 || h0 > 1.0 || w0 > 1.0 {
		if top0 <= 10000 && left0 <= 10000 && (top0+h0) <= 10000 && (left0+w0) <= 10000 {
			topNorm = top0 / 10000.0
			leftNorm = left0 / 10000.0
			heightNorm = h0 / 10000.0
			widthNorm = w0 / 10000.0
		} else {
			topNorm = top0 / 1080.0
			leftNorm = left0 / 1920.0
			heightNorm = h0 / 1080.0
			widthNorm = w0 / 1920.0
		}
	} else {
		topNorm = top0
		leftNorm = left0
		heightNorm = h0
		widthNorm = w0
	}

	clamp := func(val float64) float64 {
		if val < 0 {
			return 0
		}
		if val > 1.0 {
			return 1.0
		}
		return val
	}

	topNorm = clamp(topNorm)
	leftNorm = clamp(leftNorm)
	heightNorm = clamp(heightNorm)
	widthNorm = clamp(widthNorm)

	if heightNorm <= 0 || widthNorm <= 0 {
		return nil
	}

	return []float32{float32(topNorm), float32(leftNorm), float32(heightNorm), float32(widthNorm)}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i, true
		}
	}
	return 0, false
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
		return canonicalAddr(addr)
	}
	return host
}

// canonicalAddr is the one spelling of an address used to identify cameras: an IPv4-mapped IPv6
// address is its IPv4 form and an IPv6 zone (fe80::1%eth0) is dropped, since the zone names the
// agent's own interface, not the camera.
func canonicalAddr(a netip.Addr) string { return a.Unmap().WithZone("").String() }

func getXMLAttr(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if strings.EqualFold(a.Name.Local, name) {
			return a.Value
		}
	}
	return ""
}

func parseTime(raw string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006/01/02 15:04:05",
		"02/01/2006 15:04:05",
		"02-01-2006 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t, nil
		}
	}
	// Try epoch seconds, milliseconds, or microseconds
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if n > 1e14 {
			return time.UnixMicro(n), nil
		}
		if n > 1e11 {
			return time.UnixMilli(n), nil
		}
		return time.Unix(n, 0), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized time format %q", raw)
}
