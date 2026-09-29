package rules

import (
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

type Conditions struct {
	CameraIDs       []uuid.UUID `json:"camera_ids,omitempty"`
	ServerIDs       []uuid.UUID `json:"server_ids,omitempty"`
	SiteIDs         []uuid.UUID `json:"site_ids,omitempty"`
	Labels          []string    `json:"labels,omitempty"`
	Zones           []string    `json:"zones,omitempty"`
	Severities      []string    `json:"severities,omitempty"`
	DurationSeconds int         `json:"duration_seconds,omitempty"`
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
	ID         uuid.UUID
	TenantID   uuid.UUID
	SiteID     uuid.UUID
	ServerID   uuid.UUID
	CameraID   uuid.UUID
	CameraName string
	SiteName   string
	Severity   string
	Labels     []string
	Zones      []string
	Start      time.Time
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
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

func (c Conditions) MatchesEvent(ev EventContext) bool {
	if len(c.CameraIDs) > 0 && !containsID(c.CameraIDs, ev.CameraID) {
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
