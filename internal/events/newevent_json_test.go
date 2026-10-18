package events_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/events"
)

// NewEvent is published to subscribers: recognized names are for rule evaluation only.
func TestNewEventJSONNeverCarriesRecognizedNames(t *testing.T) {
	b, err := json.Marshal(events.NewEvent{SubLabels: []string{"ana_perez"}, GainedSubLabels: []string{"ana_perez"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "ana_perez") {
		t.Fatalf("serialized NewEvent leaks a recognized name: %s", b)
	}
}
