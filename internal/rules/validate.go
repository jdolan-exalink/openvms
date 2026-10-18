package rules

import (
	"fmt"
	"time"
)

// MaxCooldownSeconds bounds Conditions.CooldownSeconds (24 hours).
const MaxCooldownSeconds = 86400

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// ValidateConditions rejects condition combinations that the trigger type never evaluates
// (they would silently match everything or nothing) and malformed schedule/cooldown values.
func ValidateConditions(trigger TriggerType, c Conditions) error {
	if c.DurationSeconds < 0 {
		return invalid("conditions.duration_seconds must not be negative")
	}
	switch trigger {
	case TriggerEvent:
		if len(c.ServerIDs) > 0 {
			return invalid("conditions.server_ids is not valid for event rules; use camera_ids, camera_group_ids or site_ids")
		}
		if c.DurationSeconds > 0 {
			return invalid("conditions.duration_seconds is only valid for camera_offline and server_offline rules")
		}
	case TriggerCameraOffline, TriggerServerOffline:
		if err := rejectEventOnly(trigger, c); err != nil {
			return err
		}
		if trigger == TriggerCameraOffline && len(c.ServerIDs) > 0 {
			return invalid("conditions.server_ids is not valid for camera_offline rules")
		}
		if trigger == TriggerServerOffline && len(c.CameraIDs) > 0 {
			return invalid("conditions.camera_ids is not valid for server_offline rules")
		}
		if trigger == TriggerServerOffline && len(c.CameraGroupIDs) > 0 {
			return invalid("conditions.camera_group_ids is not valid for server_offline rules")
		}
	}
	if len(c.FacePersonIDs) > MaxFaceRefs {
		return invalid("conditions.face_person_ids must not exceed %d entries", MaxFaceRefs)
	}
	if len(c.FaceWatchlistIDs) > MaxFaceRefs {
		return invalid("conditions.face_watchlist_ids must not exceed %d entries", MaxFaceRefs)
	}
	if c.CooldownSeconds != nil && (*c.CooldownSeconds < 0 || *c.CooldownSeconds > MaxCooldownSeconds) {
		return invalid("conditions.cooldown_seconds must be between 0 and %d", MaxCooldownSeconds)
	}
	return validateSchedule(c.Schedule)
}

// rejectEventOnly refuses the fields that only event rules evaluate.
func rejectEventOnly(trigger TriggerType, c Conditions) error {
	set := map[string]bool{
		"labels":             len(c.Labels) > 0,
		"zones":              len(c.Zones) > 0,
		"map_zone_ids":       len(c.MapZoneIDs) > 0,
		"severities":         len(c.Severities) > 0,
		"vehicle_types":      len(c.VehicleTypes) > 0,
		"vehicle_colors":     len(c.VehicleColors) > 0,
		"schedule":           c.Schedule != nil,
		"cooldown_seconds":   c.CooldownSeconds != nil,
		"face_person_ids":    len(c.FacePersonIDs) > 0,
		"face_watchlist_ids": len(c.FaceWatchlistIDs) > 0,
		"camera_group_ids":   len(c.CameraGroupIDs) > 0 && trigger == TriggerCameraOffline,
	}
	// Deterministic order for stable messages.
	for _, k := range []string{"labels", "zones", "map_zone_ids", "severities", "vehicle_types", "vehicle_colors", "schedule", "cooldown_seconds", "face_person_ids", "face_watchlist_ids", "camera_group_ids"} {
		if set[k] {
			return invalid("conditions.%s is only valid for event rules", k)
		}
	}
	return nil
}

func validateSchedule(s *RuleSchedule) error {
	if s == nil {
		return nil
	}
	for _, d := range s.DaysOfWeek {
		if d < 0 || d > 6 {
			return invalid("conditions.schedule.days_of_week values must be 0 (Sunday) to 6 (Saturday), got %d", d)
		}
	}
	if (s.StartTime == "") != (s.EndTime == "") {
		return invalid("conditions.schedule.start_time and end_time must be set together")
	}
	if s.StartTime != "" {
		if _, ok := parseHHMM(s.StartTime); !ok {
			return invalid("conditions.schedule.start_time must be HH:MM (00:00-23:59), got %q", s.StartTime)
		}
		if _, ok := parseHHMM(s.EndTime); !ok {
			return invalid("conditions.schedule.end_time must be HH:MM (00:00-23:59), got %q", s.EndTime)
		}
	}
	if s.Timezone != "" {
		if _, err := time.LoadLocation(s.Timezone); err != nil {
			return invalid("conditions.schedule.timezone %q is not a known IANA timezone", s.Timezone)
		}
	}
	return nil
}
