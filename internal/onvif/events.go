package onvif

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	eventsNamespace = "http://www.onvif.org/ver10/events/wsdl"
	wsntNamespace   = "http://docs.oasis-open.org/wsn/b-2"
)

// EventsClient provides ONVIF WS-BaseNotification and PullPoint event operations.
type EventsClient struct {
	client   *Client
	endpoint Endpoint
}

func NewEventsClient(client *Client, endpoint Endpoint) *EventsClient {
	return &EventsClient{client: client, endpoint: endpoint}
}

type EventProperties struct {
	TopicSet []string `json:"topic_set"`
}

type PullPointSubscription struct {
	SubscriptionAddress string    `json:"subscription_address"`
	CurrentTime         time.Time `json:"current_time"`
	TerminationTime     time.Time `json:"termination_time"`
}

type EventNotification struct {
	Topic     string            `json:"topic"`
	Timestamp time.Time         `json:"timestamp"`
	Data      map[string]string `json:"data"`
}

func (e *EventsClient) GetEventProperties(ctx context.Context) (EventProperties, error) {
	data, err := e.call(ctx, e.endpoint, "GetEventProperties", "", ReadOnly)
	if err != nil {
		return EventProperties{}, err
	}
	var props EventProperties
	// Collect all topic elements inside TopicSet
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		if start, ok := token.(xml.StartElement); ok {
			if strings.EqualFold(start.Name.Local, "Topic") || strings.Contains(strings.ToLower(start.Name.Local), "motion") || strings.Contains(strings.ToLower(start.Name.Local), "line") {
				props.TopicSet = append(props.TopicSet, start.Name.Local)
			}
		}
	}
	return props, nil
}

func (e *EventsClient) CreatePullPointSubscription(ctx context.Context, initialTermination string) (PullPointSubscription, error) {
	if initialTermination == "" {
		initialTermination = "PT10M"
	}
	content := `<tev:InitialTerminationTime>` + initialTermination + `</tev:InitialTerminationTime>`
	data, err := e.call(ctx, e.endpoint, "CreatePullPointSubscription", content, Mutating)
	if err != nil {
		return PullPointSubscription{}, err
	}

	var response struct {
		Address string `xml:"SubscriptionReference>Address"`
	}
	if err := decodeEventsOperation(data, "CreatePullPointSubscriptionResponse", &response); err != nil {
		return PullPointSubscription{}, &CallError{Status: StatusFailed, Code: "invalid_pullpoint_subscription"}
	}

	return PullPointSubscription{
		SubscriptionAddress: response.Address,
	}, nil
}

func (e *EventsClient) PullMessages(ctx context.Context, subEndpoint Endpoint, timeoutSeconds, messageLimit int) ([]EventNotification, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 5
	}
	if messageLimit <= 0 {
		messageLimit = 10
	}
	content := fmt.Sprintf(`<tev:Timeout>PT%dS</tev:Timeout><tev:MessageLimit>%d</tev:MessageLimit>`, timeoutSeconds, messageLimit)
	data, err := e.call(ctx, subEndpoint, "PullMessages", content, Mutating)
	if err != nil {
		return nil, err
	}

	var notifs []EventNotification
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "NotificationMessage" {
			continue
		}
		var msg struct {
			Topic   string `xml:"Topic"`
			Message struct {
				UtcTime time.Time `xml:"UtcTime,attr"`
				Data    struct {
					SimpleItems []struct {
						Name  string `xml:"Name,attr"`
						Value string `xml:"Value,attr"`
					} `xml:"SimpleItem"`
				} `xml:"Data"`
			} `xml:"Message"`
		}
		if err := decoder.DecodeElement(&msg, &start); err == nil {
			dataMap := make(map[string]string)
			for _, item := range msg.Message.Data.SimpleItems {
				dataMap[item.Name] = item.Value
			}
			notifs = append(notifs, EventNotification{
				Topic:     msg.Topic,
				Timestamp: msg.Message.UtcTime,
				Data:      dataMap,
			})
		}
	}

	return notifs, nil
}

func decodeEventsOperation(data []byte, name string, target any) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("missing SOAP operation %s", name)
			}
			return err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Space != eventsNamespace || start.Name.Local != name {
			continue
		}
		return decoder.DecodeElement(target, &start)
	}
}

func (e *EventsClient) call(ctx context.Context, ep Endpoint, action, content string, op Operation) ([]byte, error) {
	body := `<tev:` + action + ` xmlns:tev="` + eventsNamespace + `">` + content + `</tev:` + action + `>`
	return e.client.Call(ctx, ep, eventsNamespace+"/"+action, body, op)
}
