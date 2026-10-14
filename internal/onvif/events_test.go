package onvif

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type eventsRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f eventsRoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestEventsClientOperations(t *testing.T) {
	endpoint, err := ParseEndpoint("http://192.168.1.100/onvif/events_service")
	if err != nil {
		t.Fatal(err)
	}

	transport := eventsRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		sBody := string(body)

		if strings.Contains(sBody, "CreatePullPointSubscription") {
			xmlResp := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tev="http://www.onvif.org/ver10/events/wsdl" xmlns:wsa="http://www.w3.org/2005/08/addressing">
				<s:Body>
					<tev:CreatePullPointSubscriptionResponse>
						<tev:SubscriptionReference>
							<wsa:Address>http://192.168.1.100/onvif/events_service/sub_42</wsa:Address>
						</tev:SubscriptionReference>
					</tev:CreatePullPointSubscriptionResponse>
				</s:Body>
			</s:Envelope>`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(xmlResp)),
				Header:     make(http.Header),
			}, nil
		}

		if strings.Contains(sBody, "PullMessages") {
			xmlResp := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tev="http://www.onvif.org/ver10/events/wsdl" xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2">
				<s:Body>
					<tev:PullMessagesResponse>
						<wsnt:NotificationMessage>
							<wsnt:Topic>RuleEngine/LineDetector/Crossed</wsnt:Topic>
							<wsnt:Message UtcTime="2026-10-14T12:05:00Z">
								<tt:Data xmlns:tt="http://www.onvif.org/ver10/schema">
									<tt:SimpleItem Name="State" Value="true"/>
								</tt:Data>
							</wsnt:Message>
						</wsnt:NotificationMessage>
					</tev:PullMessagesResponse>
				</s:Body>
			</s:Envelope>`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(xmlResp)),
				Header:     make(http.Header),
			}, nil
		}

		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(""))}, nil
	})

	client := NewClient(transport, Config{})
	eventsClient := NewEventsClient(client, endpoint)

	sub, err := eventsClient.CreatePullPointSubscription(context.Background(), "PT5M")
	if err != nil {
		t.Fatalf("CreatePullPointSubscription failed: %v", err)
	}
	if sub.SubscriptionAddress != "http://192.168.1.100/onvif/events_service/sub_42" {
		t.Errorf("unexpected subscription address: %s", sub.SubscriptionAddress)
	}

	subEndpoint, _ := ParseEndpoint(sub.SubscriptionAddress)
	messages, err := eventsClient.PullMessages(context.Background(), subEndpoint, 5, 10)
	if err != nil {
		t.Fatalf("PullMessages failed: %v", err)
	}
	if len(messages) != 1 || !strings.Contains(messages[0].Topic, "LineDetector") || messages[0].Data["State"] != "true" {
		t.Errorf("unexpected messages: %+v", messages)
	}
}
