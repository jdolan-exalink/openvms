package realtime

import "github.com/google/uuid"

// Client-to-server operations.
const (
	OpHello  = "hello"
	OpFilter = "filter"
)

// Server-to-client operations.
const (
	OpResync = "resync"
	OpBatch  = "batch"
)

// ClientFrame is a control frame sent from client to server.
type ClientFrame struct {
	Op          string            `json:"op"`
	V           int               `json:"v,omitempty"`
	Topics      []string          `json:"topics,omitempty"`
	LastEventID map[string]uint64 `json:"last_event_id,omitempty"`
	SiteIDs     []uuid.UUID       `json:"site_ids,omitempty"`
}

// ResyncFrame is sent by server when resume is not possible.
type ResyncFrame struct {
	Op      string   `json:"op"`
	Streams []string `json:"streams"`
}

// BatchFrame aggregates multiple frames queued within 100ms.
type BatchFrame struct {
	Op     string     `json:"op"`
	Frames []Envelope `json:"frames"`
}
