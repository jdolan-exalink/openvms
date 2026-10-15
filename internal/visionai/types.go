package visionai

import (
	"time"

	"github.com/google/uuid"
)

// VisionEvent is a normalized event produced by Frigate, LPR, Alarms, or external analytics.
type VisionEvent struct {
	ID            string         `json:"id"`
	TenantID      uuid.UUID      `json:"tenant_id"`
	SiteID        uuid.UUID      `json:"site_id"`
	ServerID      uuid.UUID      `json:"server_id"`
	CameraID      uuid.UUID      `json:"camera_id"`
	CameraName    string         `json:"camera_name"`
	Type          string         `json:"type"` // "detection", "alarm", "lpr"
	Object        string         `json:"object"`
	Confidence    float64        `json:"confidence"`
	Zone          string         `json:"zone,omitempty"`
	Timestamp     time.Time      `json:"timestamp"`
	StartTime     time.Time      `json:"start_time"`
	EndTime       *time.Time     `json:"end_time,omitempty"`
	Attributes    map[string]any `json:"attributes,omitempty"`
	AlarmSeverity string         `json:"alarm_severity,omitempty"` // "critical", "high", "medium", "low"
	Plate         string         `json:"plate,omitempty"`
	Watchlist     bool           `json:"watchlist,omitempty"`
	Score         float64        `json:"score,omitempty"`
}

// SlotReason details why a camera was selected for a slot, including score breakdown for debug mode.
type SlotReason struct {
	Object     string             `json:"object"`
	Confidence float64            `json:"confidence"`
	Zone       string             `json:"zone,omitempty"`
	Plate      string             `json:"plate,omitempty"`
	Alarm      string             `json:"alarm,omitempty"`
	Attributes map[string]any     `json:"attributes,omitempty"`
	Breakdown  map[string]float64 `json:"breakdown,omitempty"`
}

// SlotAssignment is the state of a single display slot in the Vision AI grid.
type SlotAssignment struct {
	SlotIndex       int        `json:"slot"`
	CameraID        uuid.UUID  `json:"camera_id"`
	CameraName      string     `json:"camera_name"`
	Score           float64    `json:"score"`
	Reason          SlotReason `json:"reason"`
	Pinned          bool       `json:"pinned"`
	AssignedAt      time.Time  `json:"assigned_at"`
	MinDisplayUntil time.Time  `json:"min_display_until"`
	MaxDisplayUntil time.Time  `json:"max_display_until"`
}

// ReplacementMode dictates when an active slot can be superseded.
type ReplacementMode string

const (
	ReplacementImmediate    ReplacementMode = "immediate"
	ReplacementSmooth       ReplacementMode = "smooth"
	ReplacementWaitEventEnd ReplacementMode = "wait_event_end"
	ReplacementPriorityOnly ReplacementMode = "priority_only"
)

// VisionConfig holds all configuration parameters for a Vision AI session or preset.
type VisionConfig struct {
	ID                string          `json:"id"`
	TenantID          uuid.UUID       `json:"tenant_id"`
	Name              string          `json:"name"`
	Mode              string          `json:"mode"`   // "matrix", "focus", "lpr", "alarm"
	Layout            string          `json:"layout"` // "focus", "1", "4", "9", "16", "25"
	Objects           []string        `json:"objects"`
	Sites             []uuid.UUID     `json:"sites,omitempty"`
	Servers           []uuid.UUID     `json:"servers,omitempty"`
	Cameras           []uuid.UUID     `json:"cameras,omitempty"`
	Zones             []string        `json:"zones,omitempty"`
	ReplacementMode   ReplacementMode `json:"replacement_mode"`
	MinDisplaySeconds int             `json:"min_display_seconds"`
	MaxDisplaySeconds int             `json:"max_display_seconds"`
	CooldownSeconds   int             `json:"cooldown_seconds"`
	OverlaysEnabled   bool            `json:"overlays_enabled"`
	IsDefault         bool            `json:"is_default"`
}

// DefaultVisionConfig returns sensible operational defaults for Vision AI.
func DefaultVisionConfig(tenantID uuid.UUID) VisionConfig {
	return VisionConfig{
		ID:                "default",
		TenantID:          tenantID,
		Name:              "Vision AI General",
		Mode:              "matrix",
		Layout:            "4",
		Objects:           []string{"person", "car", "motorcycle", "truck", "license_plate"},
		ReplacementMode:   ReplacementSmooth,
		MinDisplaySeconds: 5,
		MaxDisplaySeconds: 30,
		CooldownSeconds:   10,
		OverlaysEnabled:   true,
		IsDefault:         true,
	}
}
