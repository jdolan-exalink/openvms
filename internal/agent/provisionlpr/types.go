package provisionlpr

import (
	"strings"
	"time"
)

// LPREvent is the normalized representation of a license plate reading from
// Provision-ISR cameras according to the plugin specification.
type LPREvent struct {
	EventType string `json:"event_type"` // "lpr"
	Source    string `json:"source"`     // "provision_isr"
	CameraID  string `json:"camera_id"`
	// ChannelID identifies the channel index on multi-channel NVRs (e.g. 1, 2, ...).
	ChannelID *int `json:"channel_id,omitempty"`
	// ReportedCameraID is the camera ID the payload itself carried when it differs from the
	// registered one; the original is kept for audit, never used to identify the camera.
	ReportedCameraID *string   `json:"reported_camera_id,omitempty"`
	CameraIP         string    `json:"camera_ip"`
	Plate            string    `json:"plate"`
	EventTime        time.Time `json:"event_time"`
	ReceivedTime     time.Time `json:"received_time"`
	CountryOrRegion  *string   `json:"country_or_region,omitempty"`
	Confidence       *float64  `json:"confidence,omitempty"`
	ListStatus       *string   `json:"list_status,omitempty"`
	PanoramicImage   *string   `json:"panoramic_image,omitempty"`
	PlateCrop        *string   `json:"plate_crop,omitempty"`
	RawEndpoint      *string   `json:"raw_endpoint,omitempty"`
	RawPayloadRef    *string   `json:"raw_payload_ref,omitempty"`
	VehicleColor     *string   `json:"vehicle_color,omitempty"`
	VehicleBrand     *string   `json:"vehicle_brand,omitempty"`
	VehicleModel     *string   `json:"vehicle_model,omitempty"`
	VehicleType      *string   `json:"vehicle_type,omitempty"`
	Speed            *float64  `json:"speed,omitempty"`
	Direction        *string   `json:"direction,omitempty"`
}

// CameraStatus tracks the health and heartbeat of a Provision-ISR camera.
type CameraStatus struct {
	CameraID       string     `json:"camera_id"`
	CameraIP       string     `json:"camera_ip"`
	Online         bool       `json:"online"`
	LastSeen       time.Time  `json:"last_seen"`
	KeepaliveCount int64      `json:"keepalive_count"`
	EventCount     int64      `json:"event_count"`
	LastPlate      string     `json:"last_plate,omitempty"`
	LastEventTime  *time.Time `json:"last_event_time,omitempty"`
	RecentEvents   []LPREvent `json:"recent_events,omitempty"`
}

// Config defines operational parameters for the Provision-ISR LPR receiver.
type Config struct {
	HeartbeatTimeout    time.Duration `json:"heartbeat_timeout"`
	DeduplicationWindow time.Duration `json:"deduplication_window"`
	MaxRequestBodyBytes int64         `json:"max_request_body_bytes"`
}

// DefaultConfig provides recommended defaults from the technical manual:
// Heartbeat timeout 60s (2-3x camera interval), dedup window 15s (5-120s range), max 10MB body.
func DefaultConfig() Config {
	return Config{
		HeartbeatTimeout:    60 * time.Second,
		DeduplicationWindow: 15 * time.Second,
		MaxRequestBodyBytes: 10 << 20, // 10 MiB for multipart images
	}
}

// NormalizePlate sanitizes plate strings by trimming whitespace, uppercasing,
// and removing punctuation and invalid characters.
func NormalizePlate(raw string) string {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	var sb strings.Builder
	for _, r := range raw {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
