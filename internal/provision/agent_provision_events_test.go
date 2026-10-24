package provision

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

const eventsPageJSON = `{"events":[{"seq":6,"id":"a1b2c3","event":{"camera_id":"11111111-1111-1111-1111-111111111111","plate":"AF123BC","event_time":"2026-10-24T12:00:00Z","received_time":"2026-10-24T12:00:01Z","confidence":0.93}}],"last_seq":9,"depth":4,"lost":2,"epoch":"e1f2"}`

func TestPullProvisionLPREventsAsksTheAgentForTheEventsAfterTheCursor(t *testing.T) {
	var seen *http.Request
	svc := videoEncodingService(func(r *http.Request) (*http.Response, error) {
		seen = r.Clone(r.Context())
		return agentReply(200, eventsPageJSON)(r)
	})
	page, err := svc.PullProvisionLPREvents(context.Background(), uuid.New(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if seen.Method != http.MethodGet || seen.URL.Path != "/v1/provision-lpr/events/after/5" || seen.URL.RawQuery != "" {
		t.Fatalf("request = %s %s", seen.Method, seen.URL.String())
	}
	if seen.Header.Get("Authorization") != "Bearer agent-token" {
		t.Fatalf("the agent token was not sent: %v", seen.Header)
	}
	if len(page.Events) != 1 || page.Events[0].Seq != 6 || page.Events[0].ID != "a1b2c3" || page.Events[0].Event.Plate != "AF123BC" ||
		page.Events[0].Event.CameraID != "11111111-1111-1111-1111-111111111111" || page.Events[0].Event.Confidence == nil || *page.Events[0].Event.Confidence != 0.93 {
		t.Fatalf("events = %+v", page.Events)
	}
	if page.LastSeq != 9 || page.Depth != 4 || page.Lost != 2 || page.Epoch != "e1f2" {
		t.Fatalf("page = %+v", page)
	}
}

func TestAckProvisionLPREventsSendsTheSequenceStored(t *testing.T) {
	var seen *http.Request
	var body string
	svc := videoEncodingService(func(r *http.Request) (*http.Response, error) {
		seen = r.Clone(r.Context())
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		return agentReply(200, `{"removed":3,"depth":1,"last_seq":9}`)(r)
	})
	if err := svc.AckProvisionLPREvents(context.Background(), uuid.New(), 8); err != nil {
		t.Fatal(err)
	}
	if seen.Method != http.MethodPost || seen.URL.Path != "/v1/provision-lpr/events/ack" || body != `{"through":8}` || seen.Header.Get("Authorization") != "Bearer agent-token" {
		t.Fatalf("request = %s %s %q %v", seen.Method, seen.URL.Path, body, seen.Header)
	}
}

func TestProvisionLPREventsMapAgentFailuresToTypedErrors(t *testing.T) {
	old := videoEncodingService(agentReply(404, "not found"))
	if _, err := old.PullProvisionLPREvents(context.Background(), uuid.New(), 0); !errors.Is(err, ErrAgentEventsUnsupported) {
		t.Fatalf("404 = %v, want ErrAgentEventsUnsupported", err)
	}
	if err := old.AckProvisionLPREvents(context.Background(), uuid.New(), 1); !errors.Is(err, ErrAgentEventsUnsupported) {
		t.Fatalf("ack 404 = %v, want ErrAgentEventsUnsupported", err)
	}
	denied := videoEncodingService(agentReply(401, "unauthorized"))
	_, err := denied.PullProvisionLPREvents(context.Background(), uuid.New(), 0)
	var statusErr *AgentStatusError
	if !errors.As(err, &statusErr) || statusErr.Status != 401 {
		t.Fatalf("401 = %v, want an AgentStatusError", err)
	}
	garbled := videoEncodingService(agentReply(200, "<html>"))
	if _, err := garbled.PullProvisionLPREvents(context.Background(), uuid.New(), 0); err == nil {
		t.Fatal("a body that is not the agent's JSON must be an error, not an empty page")
	}
}
