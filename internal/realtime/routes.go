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
	TypeEventCreated = "event.created"
	TypeServerStatus = "server.status"
	TypeAlarmUpdated = "alarm.updated"
)

// Envelope is what a client receives.
type Envelope struct {
	Type     string          `json:"type"`
	TenantID uuid.UUID       `json:"tenant_id"`
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
	Envelope Envelope
	Scope    Scope
}

// Route maps NATS messages of one subject pattern into feed messages.
type Route struct {
	// Stream is the JetStream stream that holds Subject.
	Stream string
	// Subject is a NATS subject pattern ("*" one token, ">" the rest).
	Subject string
	// Decode turns a raw payload into a Message. It must set the tenant and the scope.
	Decode func(subject string, data []byte) (Message, error)
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
	}
}

// Decode finds the route for subject and decodes data with it.
func Decode(routes []Route, subject string, data []byte) (Message, error) {
	for _, r := range routes {
		if SubjectMatch(r.Subject, subject) {
			return r.Decode(subject, data)
		}
	}
	return Message{}, errNoRoute
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

func decodeEventCreated(_ string, raw []byte) (Message, error) {
	var p newEventPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Message{}, err
	}
	if p.TenantID == uuid.Nil {
		return Message{}, errNoTenant
	}
	if p.CameraID == uuid.Nil {
		return Message{}, errors.New("realtime: event has no camera")
	}
	// Re-marshalling the typed struct is the allow-list: nothing else from the payload leaves.
	data, err := json.Marshal(p)
	if err != nil {
		return Message{}, err
	}
	return Message{
		Envelope: Envelope{Type: TypeEventCreated, TenantID: p.TenantID, Data: data},
		Scope:    Scope{Kind: authz.ScopeCamera, ID: p.CameraID, Permission: authz.EventsView},
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

func decodeServerStatus(_ string, raw []byte) (Message, error) {
	var p statusPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Message{}, err
	}
	if p.TenantID == uuid.Nil {
		return Message{}, errNoTenant
	}
	if p.ServerID == uuid.Nil {
		return Message{}, errors.New("realtime: status has no server")
	}
	// The publisher's free-text error (may name internal hosts) is not part of statusPayload.
	data, err := json.Marshal(p)
	if err != nil {
		return Message{}, err
	}
	return Message{
		Envelope: Envelope{Type: TypeServerStatus, TenantID: p.TenantID, Data: data},
		Scope:    Scope{Kind: authz.ScopeServer, ID: p.ServerID, Permission: authz.ServersView},
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

func decodeAlarmUpdated(_ string, raw []byte) (Message, error) {
	var p alarmPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Message{}, err
	}
	if p.TenantID == uuid.Nil {
		return Message{}, errNoTenant
	}
	if p.CameraID == uuid.Nil {
		return Message{}, errors.New("realtime: alarm has no camera")
	}
	data, err := json.Marshal(p)
	if err != nil {
		return Message{}, err
	}
	return Message{
		Envelope: Envelope{Type: TypeAlarmUpdated, TenantID: p.TenantID, Data: data},
		Scope:    Scope{Kind: authz.ScopeCamera, ID: p.CameraID, Permission: authz.AlarmsView},
	}, nil
}
