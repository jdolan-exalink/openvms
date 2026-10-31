package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

const (
	provisionEventsPath = "/v1/provision-lpr/events"
	// maxProvisionEventsBody bounds one page from the agent: up to 25 events with base64 images.
	maxProvisionEventsBody = 32 << 20
)

// ErrAgentEventsUnsupported means the edge agent answered 404 to the event queue routes, which is
// what an agent older than the durable queue does.
var ErrAgentEventsUnsupported = errors.New("the agent does not support the Provision-ISR event queue; update the agent")

// LPRAgentEvent is the part of an agent's normalized LPR event that central stores.
type LPRAgentEvent struct {
	EventType      string    `json:"event_type,omitempty"`
	CameraID       string    `json:"camera_id"`
	CameraIP       string    `json:"camera_ip,omitempty"`
	ChannelID      *int      `json:"channel_id,omitempty"`
	Plate          string    `json:"plate"`
	EventTime      time.Time `json:"event_time"`
	ReceivedTime   time.Time `json:"received_time"`
	Confidence     *float64  `json:"confidence,omitempty"`
	ListStatus     *string   `json:"list_status,omitempty"`
	VehicleColor   *string   `json:"vehicle_color,omitempty"`
	VehicleBrand   *string   `json:"vehicle_brand,omitempty"`
	VehicleModel   *string   `json:"vehicle_model,omitempty"`
	VehicleType    *string   `json:"vehicle_type,omitempty"`
	PlateCrop      *string   `json:"plate_crop,omitempty"`
	PanoramicImage *string   `json:"panoramic_image,omitempty"`

	PlateBox []float32 `json:"plate_box,omitempty"`

	PersonName *string  `json:"person_name,omitempty"`
	FaceScore  *float64 `json:"face_score,omitempty"`
	FaceID     *string  `json:"face_id,omitempty"`

	EnterCount *int `json:"enter_count,omitempty"`
	ExitCount  *int `json:"exit_count,omitempty"`
	PassCount  *int `json:"pass_count,omitempty"`
}

// LPRQueuedEvent is one event waiting in an agent's queue. Seq is the agent's cursor; ID is random,
// assigned once, and identifies the event for idempotent storage.
type LPRQueuedEvent struct {
	Seq   uint64        `json:"seq"`
	ID    string        `json:"id"`
	Event LPRAgentEvent `json:"event"`
}

// LPREventsPage is one answer of the agent's event queue.
type LPREventsPage struct {
	Events  []LPRQueuedEvent `json:"events"`
	LastSeq uint64           `json:"last_seq"`
	Depth   int              `json:"depth"`
	Lost    uint64           `json:"lost"`
	// Epoch identifies the agent's queue; when it changes, the queue was recreated and its sequences
	// start again.
	Epoch string `json:"epoch"`
}

// PullProvisionLPREvents reads the next page of queued events, those with a sequence greater than
// after; the agent decides the page size. It is for background jobs: it runs as the system actor and
// does not check a user's rights. The sequence travels in the path because the agent client refuses
// query strings.
func (s *Service) PullProvisionLPREvents(ctx context.Context, serverID uuid.UUID, after uint64) (LPREventsPage, error) {
	data, err := s.systemAgentCall(ctx, serverID, http.MethodGet, provisionEventsPath+"/after/"+strconv.FormatUint(after, 10), nil)
	if err != nil {
		return LPREventsPage{}, err
	}
	var page LPREventsPage
	if err := json.Unmarshal(data, &page); err != nil {
		return LPREventsPage{}, fmt.Errorf("%w: the agent's event page is not JSON", ErrAgentProbeFailed)
	}
	return page, nil
}

// AckProvisionLPREvents tells the agent that every event up to and including through is stored.
func (s *Service) AckProvisionLPREvents(ctx context.Context, serverID uuid.UUID, through uint64) error {
	body, _ := json.Marshal(map[string]uint64{"through": through})
	_, err := s.systemAgentCall(ctx, serverID, http.MethodPost, provisionEventsPath+"/ack", body)
	return err
}

func (s *Service) systemAgentCall(ctx context.Context, serverID uuid.UUID, method, path string, body []byte) ([]byte, error) {
	client, token, err := s.getAgentClientAndToken(ctx, authz.Actor{System: true}, serverID)
	if err != nil {
		return nil, err
	}
	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	var payload io.Reader
	if body != nil {
		payload = bytes.NewReader(body)
		headers.Set("Content-Type", "application/json")
	}
	ctx, cancel := context.WithTimeout(ctx, onvifProbeTimeout)
	defer cancel()
	response, err := client.Do(ctx, method, path, headers, payload)
	if err != nil {
		return nil, ErrAgentProbeUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
		data, err := io.ReadAll(io.LimitReader(response.Body, maxProvisionEventsBody+1))
		if err != nil || len(data) > maxProvisionEventsBody {
			return nil, ErrAgentProbeFailed
		}
		return data, nil
	case http.StatusNotFound:
		return nil, ErrAgentEventsUnsupported
	default:
		b, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return nil, &AgentStatusError{Status: response.StatusCode, Message: strings.TrimSpace(string(b))}
	}
}
