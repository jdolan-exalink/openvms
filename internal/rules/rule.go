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
	Labels          []string    `json:"labels,omitempty"`
	Zones           []string    `json:"zones,omitempty"`
	Severities      []string    `json:"severities,omitempty"`
	DurationSeconds int         `json:"duration_seconds,omitempty"`
}

type Actions struct {
	CreateAlarm bool   `json:"create_alarm,omitempty"`
	NotifyInApp bool   `json:"notify_in_app,omitempty"`
	Severity    string `json:"severity,omitempty"`
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

func (c Conditions) MatchesEvent(ev EventContext) bool {
	if len(c.CameraIDs) > 0 {
		matched := false
		for _, id := range c.CameraIDs {
			if id == ev.CameraID {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if len(c.Severities) > 0 {
		matched := false
		for _, s := range c.Severities {
			if strings.EqualFold(s, ev.Severity) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if len(c.Labels) > 0 {
		matched := false
		for _, l := range c.Labels {
			for _, evLabel := range ev.Labels {
				if strings.EqualFold(l, evLabel) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			return false
		}
	}

	if len(c.Zones) > 0 {
		matched := false
		for _, z := range c.Zones {
			for _, evZone := range ev.Zones {
				if strings.EqualFold(z, evZone) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

func (c Conditions) MatchesCameraOffline(cameraID uuid.UUID, duration time.Duration) bool {
	if len(c.CameraIDs) > 0 {
		matched := false
		for _, id := range c.CameraIDs {
			if id == cameraID {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if c.DurationSeconds > 0 && duration < time.Duration(c.DurationSeconds)*time.Second {
		return false
	}
	return true
}

func (c Conditions) MatchesServerOffline(serverID uuid.UUID, duration time.Duration) bool {
	if len(c.ServerIDs) > 0 {
		matched := false
		for _, id := range c.ServerIDs {
			if id == serverID {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if c.DurationSeconds > 0 && duration < time.Duration(c.DurationSeconds)*time.Second {
		return false
	}
	return true
}
