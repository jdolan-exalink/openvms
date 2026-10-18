package rules

import (
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type TriggerType string

const (
	TriggerEvent         TriggerType = "event"
	TriggerCameraOffline TriggerType = "camera_offline"
	TriggerServerOffline TriggerType = "server_offline"
)

// RuleSchedule restricts when a rule matches based on local time of day and day of week.
// DaysOfWeek uses 0=Sunday .. 6=Saturday (matching time.Weekday). Empty DaysOfWeek means every day.
type RuleSchedule struct {
	DaysOfWeek []int  `json:"days_of_week,omitempty"`
	StartTime  string `json:"start_time,omitempty"`
	EndTime    string `json:"end_time,omitempty"`
	Timezone   string `json:"timezone,omitempty"`
}

type Conditions struct {
	CameraIDs      []uuid.UUID `json:"camera_ids,omitempty"`
	CameraGroupIDs []uuid.UUID `json:"camera_group_ids,omitempty"`
	ServerIDs      []uuid.UUID `json:"server_ids,omitempty"`
	SiteIDs        []uuid.UUID `json:"site_ids,omitempty"`
	Labels         []string    `json:"labels,omitempty"`
	Zones          []string    `json:"zones,omitempty"`
	MapZoneIDs     []uuid.UUID `json:"map_zone_ids,omitempty"`
	// FacePersonIDs and FaceWatchlistIDs restrict event rules to recognized registered persons:
	// the event's sub_labels must contain the Frigate name of any listed person or any member of
	// a listed watchlist (see FaceResolver). Both empty means any person.
	FacePersonIDs    []uuid.UUID   `json:"face_person_ids,omitempty"`
	FaceWatchlistIDs []uuid.UUID   `json:"face_watchlist_ids,omitempty"`
	Severities       []string      `json:"severities,omitempty"`
	VehicleTypes     []string      `json:"vehicle_types,omitempty"`
	VehicleColors    []string      `json:"vehicle_colors,omitempty"`
	DurationSeconds  int           `json:"duration_seconds,omitempty"`
	Schedule         *RuleSchedule `json:"schedule,omitempty"`
	CooldownSeconds  *int          `json:"cooldown_seconds,omitempty"`
}

type Actions struct {
	CreateAlarm bool   `json:"create_alarm,omitempty"`
	NotifyInApp bool   `json:"notify_in_app,omitempty"`
	Severity    string `json:"severity,omitempty"`
	// ChannelIDs selects external notification channels (internal/notify). It lives in the
	// actions JSON rather than a join table: rules are always read whole, and delete/validate
	// paths keep the ids consistent (see Service.checkChannels and notify.Service.Delete).
	ChannelIDs []uuid.UUID `json:"channel_ids,omitempty"`
}

type Rule struct {
	ID          uuid.UUID   `json:"id"`
	TenantID    uuid.UUID   `json:"tenant_id"`
	Name        string      `json:"name"`
	TriggerType TriggerType `json:"trigger_type"`
	Conditions  Conditions  `json:"conditions"`
	Actions     Actions     `json:"actions"`
	Enabled     bool        `json:"enabled"`
	Version     int         `json:"version"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type Notification struct {
	ID        uuid.UUID  `json:"id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	UserID    *uuid.UUID `json:"user_id,omitempty"`
	RuleID    *uuid.UUID `json:"rule_id,omitempty"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Link      *string    `json:"link,omitempty"`
	Severity  string     `json:"severity"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type EventContext struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	SiteID         uuid.UUID
	SiteTimezone   string
	ServerID       uuid.UUID
	CameraID       uuid.UUID
	CameraGroupIDs []uuid.UUID
	CameraName     string
	SiteName       string
	Severity       string
	Labels         []string
	// SubLabels are the names Frigate recognized on the event (faces, known plates).
	SubLabels []string
	// FaceUpdate marks a re-evaluation after an already-evaluated event gained recognized names:
	// only rules with person conditions run, so every other rule does not fire a second time.
	FaceUpdate   bool
	Zones        []string
	MapZoneIDs   []uuid.UUID
	Start        time.Time
	VehicleType  string
	VehicleColor string
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func containsAnyID(wanted, candidates []uuid.UUID) bool {
	for _, id := range wanted {
		if containsID(candidates, id) {
			return true
		}
	}
	return false
}

// matchesAny reports whether any wanted value equals (case-insensitively) any of the candidates.
func matchesAny(wanted, candidates []string) bool {
	for _, w := range wanted {
		for _, c := range candidates {
			if strings.EqualFold(w, c) {
				return true
			}
		}
	}
	return false
}

// matchesSite is true when no site filter is set or siteID is one of the selected sites.
func (c Conditions) matchesSite(siteID uuid.UUID) bool {
	return len(c.SiteIDs) == 0 || containsID(c.SiteIDs, siteID)
}

func parseHHMM(s string) (int, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func containsDay(days []int, weekday int) bool {
	if len(days) == 0 {
		return true
	}
	for _, d := range days {
		if d >= 0 && d <= 7 && (d%7) == weekday {
			return true
		}
	}
	return false
}

func resolveScheduleLocation(siteTZ, scheduleTZ string) *time.Location {
	for _, tz := range []string{siteTZ, scheduleTZ} {
		if tz == "" {
			continue
		}
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.UTC
}

// Matches reports whether at satisfies the schedule in the camera's site timezone
// (falling back to s.Timezone and then UTC).
func (s *RuleSchedule) Matches(at time.Time, siteTZ string) bool {
	if s == nil {
		return true
	}
	if len(s.DaysOfWeek) == 0 && s.StartTime == "" && s.EndTime == "" {
		return true
	}
	if at.IsZero() {
		return false
	}
	loc := resolveScheduleLocation(siteTZ, s.Timezone)
	local := at.In(loc)
	weekday := int(local.Weekday())

	if s.StartTime == "" && s.EndTime == "" {
		return containsDay(s.DaysOfWeek, weekday)
	}
	startMin, okStart := parseHHMM(s.StartTime)
	endMin, okEnd := parseHHMM(s.EndTime)
	if !okStart || !okEnd {
		return false
	}
	curMin := local.Hour()*60 + local.Minute()
	if startMin <= endMin {
		if !containsDay(s.DaysOfWeek, weekday) {
			return false
		}
		if endMin == 23*60+59 {
			return curMin >= startMin && curMin <= endMin
		}
		return curMin >= startMin && curMin < endMin
	}
	// Overnight window (StartTime > EndTime, e.g. 22:00 to 06:00).
	if curMin >= startMin {
		return containsDay(s.DaysOfWeek, weekday)
	}
	if curMin < endMin {
		prevWeekday := (weekday + 6) % 7
		return containsDay(s.DaysOfWeek, prevWeekday)
	}
	return false
}

func (c Conditions) MatchesEvent(ev EventContext) bool {
	if len(c.CameraIDs) > 0 && !containsID(c.CameraIDs, ev.CameraID) {
		return false
	}
	if len(c.CameraGroupIDs) > 0 && !containsAnyID(c.CameraGroupIDs, ev.CameraGroupIDs) {
		return false
	}
	if !c.matchesSite(ev.SiteID) {
		return false
	}
	if len(c.Severities) > 0 && !matchesAny(c.Severities, []string{ev.Severity}) {
		return false
	}
	if len(c.Labels) > 0 && !matchesAny(c.Labels, ev.Labels) {
		return false
	}
	if len(c.Zones) > 0 && !matchesAny(c.Zones, ev.Zones) {
		return false
	}
	if len(c.VehicleTypes) > 0 && !matchesAny(c.VehicleTypes, []string{ev.VehicleType}) {
		return false
	}
	if len(c.VehicleColors) > 0 && !matchesAny(c.VehicleColors, []string{ev.VehicleColor}) {
		return false
	}
	if len(c.MapZoneIDs) > 0 && !containsAnyID(c.MapZoneIDs, ev.MapZoneIDs) {
		return false
	}
	if c.Schedule != nil && !c.Schedule.Matches(ev.Start, ev.SiteTimezone) {
		return false
	}
	return true
}

func (c Conditions) MatchesCameraOffline(cameraID, siteID uuid.UUID, duration time.Duration) bool {
	if len(c.CameraIDs) > 0 && !containsID(c.CameraIDs, cameraID) {
		return false
	}
	if !c.matchesSite(siteID) {
		return false
	}
	return c.DurationSeconds <= 0 || duration >= time.Duration(c.DurationSeconds)*time.Second
}

func (c Conditions) MatchesServerOffline(serverID, siteID uuid.UUID, duration time.Duration) bool {
	if len(c.ServerIDs) > 0 && !containsID(c.ServerIDs, serverID) {
		return false
	}
	if !c.matchesSite(siteID) {
		return false
	}
	return c.DurationSeconds <= 0 || duration >= time.Duration(c.DurationSeconds)*time.Second
}
