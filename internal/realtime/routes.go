package realtime

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

// Message types of the feed. Add new ones here together with a Route.
const (
	TypeEventCreated        = "event.created"
	TypeServerStatus        = "server.status"
	TypeAlarmUpdated        = "alarm.updated"
	TypeNotificationCreated = "notification.created"
	TypeCameraStatusChanged = "camera.status_changed"
	TypeObjectDetected      = "object.detected"
	TypeServerStatusChanged = "server.status_changed"
	TypeAlarmCreated        = "alarm.created"
	TypeAlarmAcknowledged   = "alarm.acknowledged"
)

// Envelope is what a client receives.
type Envelope struct {
	V        int             `json:"v,omitempty"`
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type"`
	TS       *time.Time      `json:"ts,omitempty"`
	TenantID uuid.UUID       `json:"tenant_id"`
	SiteID   *uuid.UUID      `json:"site_id,omitempty"`
	CameraID *uuid.UUID      `json:"camera_id,omitempty"`
	ServerID *uuid.UUID      `json:"server_id,omitempty"`
	Data     json.RawMessage `json:"data"`
}

// Scope names the resource a message is about and the permission needed to see it.
type Scope struct {
	Kind       authz.ScopeType
	ID         uuid.UUID
	Permission authz.Permission
}

// Message is an Envelope plus the Scope it is authorized against (never sent to clients).
type Message struct {
	Stream   string
	Seq      uint64
	Envelope Envelope
	Scope    Scope
}

// Route maps NATS messages of one subject pattern into feed messages.
type Route struct {
	// Stream is the JetStream stream that holds Subject.
	Stream string
	// Subject is a NATS subject pattern ("*" one token, ">" the rest).
	Subject string
	// Decode turns a raw payload into Messages. It must set the tenant and the scope.
	Decode func(subject string, data []byte) ([]Message, error)
}

var (
	errNoRoute  = errors.New("realtime: no route for subject")
	errNoTenant = errors.New("realtime: message has no tenant")
)

// DefaultRoutes covers what the worker publishes today.
func DefaultRoutes() []Route {
	return []Route{
		{Stream: "FRIGATE", Subject: "frigate.event.new.*", Decode: decodeEventCreated},
		{Stream: "PLATFORM", Subject: "server.*", Decode: decodeServerStatus},
		{Stream: "PLATFORM", Subject: "alarm.*.*", Decode: decodeAlarmUpdated},
		{Stream: "PLATFORM", Subject: "notification.created.*", Decode: decodeNotificationCreated},
		{Stream: "PLATFORM", Subject: "camera.status.*", Decode: decodeCameraStatus},
	}
}

// Decode finds the route for subject and returns the first decoded message.
func Decode(routes []Route, subject string, data []byte) (Message, error) {
	msgs, err := DecodeAll(routes, subject, data)
	if err != nil {
		return Message{}, err
	}
	if len(msgs) == 0 {
		return Message{}, errNoRoute
	}
	return msgs[0], nil
}

// DecodeAll finds all routes matching subject and decodes all messages.
func DecodeAll(routes []Route, subject string, data []byte) ([]Message, error) {
	var out []Message
	matched := false
	for _, r := range routes {
		if SubjectMatch(r.Subject, subject) {
			matched = true
			msgs, err := r.Decode(subject, data)
			if err != nil {
				return nil, err
			}
			out = append(out, msgs...)
		}
	}
	if !matched {
		return nil, errNoRoute
	}
	return out, nil
}

// SubjectMatch implements NATS subject matching for the wildcards used by Routes.
func SubjectMatch(pattern, subject string) bool {
	p, s := strings.Split(pattern, "."), strings.Split(subject, ".")
	for i, tok := range p {
		if tok == ">" {
			return len(s) > i
		}
		if i >= len(s) || (tok != "*" && tok != s[i]) {
			return false
		}
	}
	return len(p) == len(s)
}

// newEventPayload is the shape events.Syncer publishes on frigate.event.new.<tenant>.
type newEventPayload struct {
	ID       uuid.UUID `json:"id"`
	TenantID uuid.UUID `json:"tenant_id"`
	SiteID   uuid.UUID `json:"site_id"`
	ServerID uuid.UUID `json:"server_id"`
	CameraID uuid.UUID `json:"camera_id"`
	Severity string    `json:"severity"`
	Labels   []string  `json:"labels"`
	Start    time.Time `json:"start_time"`
}

func uuidPtr(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func decodeEventCreated(_ string, raw []byte) ([]Message, error) {
	var p newEventPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.TenantID == uuid.Nil {
		return nil, errNoTenant
	}
	if p.CameraID == uuid.Nil {
		return nil, errors.New("realtime: event has no camera")
	}
	// Re-marshalling the typed struct is the allow-list: nothing else from the payload leaves.
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return []Message{
		{
			Envelope: Envelope{
				V:        2,
				Type:     TypeEventCreated,
				TS:       timePtr(p.Start),
				TenantID: p.TenantID,
				SiteID:   uuidPtr(p.SiteID),
				CameraID: uuidPtr(p.CameraID),
				ServerID: uuidPtr(p.ServerID),
				Data:     data,
			},
			Scope: Scope{Kind: authz.ScopeCamera, ID: p.CameraID, Permission: authz.EventsView},
		},
	}, nil
}

// statusPayload is the shape inventory.HealthPoller publishes on server.<status>.
type statusPayload struct {
	ServerID uuid.UUID `json:"server_id"`
	TenantID uuid.UUID `json:"tenant_id"`
	SiteID   uuid.UUID `json:"site_id"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	At       time.Time `json:"at"`
}

func decodeServerStatus(_ string, raw []byte) ([]Message, error) {
	var p statusPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.TenantID == uuid.Nil {
		return nil, errNoTenant
	}
	if p.ServerID == uuid.Nil {
		return nil, errors.New("realtime: status has no server")
	}
	// The publisher's free-text error (may name internal hosts) is not part of statusPayload.
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return []Message{
		{
			Envelope: Envelope{
				V:        2,
				Type:     TypeServerStatus,
				TS:       timePtr(p.At),
				TenantID: p.TenantID,
				SiteID:   uuidPtr(p.SiteID),
				ServerID: uuidPtr(p.ServerID),
				Data:     data,
			},
			Scope: Scope{Kind: authz.ScopeServer, ID: p.ServerID, Permission: authz.ServersView},
		},
	}, nil
}

// alarmPayload is the shape alarms.Service publishes on alarm.<action>.<tenant>.
type alarmPayload struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	SiteID    uuid.UUID `json:"site_id"`
	CameraID  uuid.UUID `json:"camera_id"`
	EventID   uuid.UUID `json:"event_id"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

func decodeAlarmUpdated(_ string, raw []byte) ([]Message, error) {
	var p alarmPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.TenantID == uuid.Nil {
		return nil, errNoTenant
	}
	if p.CameraID == uuid.Nil {
		return nil, errors.New("realtime: alarm has no camera")
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return []Message{
		{
			Envelope: Envelope{
				V:        2,
				Type:     TypeAlarmUpdated,
				TS:       timePtr(p.UpdatedAt),
				TenantID: p.TenantID,
				SiteID:   uuidPtr(p.SiteID),
				CameraID: uuidPtr(p.CameraID),
				Data:     data,
			},
			Scope: Scope{Kind: authz.ScopeCamera, ID: p.CameraID, Permission: authz.AlarmsView},
		},
	}, nil
}

// notificationPayload is the shape rules.Service publishes on notification.created.<tenant>.
type notificationPayload struct {
	ID        uuid.UUID  `json:"id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	UserID    *uuid.UUID `json:"user_id,omitempty"`
	RuleID    *uuid.UUID `json:"rule_id,omitempty"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Link      *string    `json:"link,omitempty"`
	Severity  string     `json:"severity"`
	CreatedAt time.Time  `json:"created_at"`
}

func decodeNotificationCreated(_ string, raw []byte) ([]Message, error) {
	var p notificationPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.TenantID == uuid.Nil {
		return nil, errNoTenant
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return []Message{
		{
			Envelope: Envelope{
				V:        2,
				Type:     TypeNotificationCreated,
				TS:       timePtr(p.CreatedAt),
				TenantID: p.TenantID,
				Data:     data,
			},
			Scope: Scope{Kind: authz.ScopeTenant, ID: p.TenantID},
		},
	}, nil
}

// cameraStatusPayload is the shape inventory.HealthPoller publishes on camera.status.<tenant>.
type cameraStatusPayload struct {
	CameraID uuid.UUID `json:"camera_id"`
	ServerID uuid.UUID `json:"server_id"`
	TenantID uuid.UUID `json:"tenant_id"`
	SiteID   uuid.UUID `json:"site_id"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	At       time.Time `json:"at"`
}

func decodeCameraStatus(_ string, raw []byte) ([]Message, error) {
	var p cameraStatusPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.TenantID == uuid.Nil {
		return nil, errNoTenant
	}
	if p.CameraID == uuid.Nil {
		return nil, errors.New("realtime: camera status has no camera")
	}
	data, err := json.Marshal(map[string]string{
		"from": p.From,
		"to":   p.To,
	})
	if err != nil {
		return nil, err
	}
	return []Message{
		{
			Envelope: Envelope{
				V:        2,
				Type:     TypeCameraStatusChanged,
				TS:       timePtr(p.At),
				TenantID: p.TenantID,
				SiteID:   uuidPtr(p.SiteID),
				CameraID: uuidPtr(p.CameraID),
				ServerID: uuidPtr(p.ServerID),
				Data:     data,
			},
			Scope: Scope{Kind: authz.ScopeCamera, ID: p.CameraID, Permission: authz.CamerasView},
		},
	}, nil
}
