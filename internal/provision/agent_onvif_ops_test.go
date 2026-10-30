package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeAgentProbeDoer struct {
	statusCode int
	body       []byte
	err        error
}

func (f *fakeAgentProbeDoer) Do(_ context.Context, _ string, _ string, _ http.Header, _ io.Reader) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.statusCode,
		Body:       io.NopCloser(bytes.NewReader(f.body)),
	}, nil
}

func TestGetAgentJSONWithLimitAcceptsLargePayloads(t *testing.T) {
	// Create a large JSON payload > 64KB (e.g. 120KB) containing base64 images
	bigImage := strings.Repeat("A", 100*1024)
	payload := map[string]any{
		"cameras": []any{
			map[string]any{
				"camera_id": "9217f465-79b5-47d8-bb4e-c5484f7251ca",
				"camera_ip": "10.0.15.107",
				"online":    true,
				"recent_events": []any{
					map[string]any{
						"plate":      "C00316",
						"plate_crop": bigImage,
					},
				},
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if len(raw) <= int(maxOnvifProbeResponseBytes) {
		t.Fatalf("payload size %d must be > %d", len(raw), maxOnvifProbeResponseBytes)
	}

	doer := &fakeAgentProbeDoer{
		statusCode: http.StatusOK,
		body:       raw,
	}

	// With default getAgentJSON (64KB limit), it must fail
	var outSmall map[string]any
	if err := getAgentJSON(context.Background(), doer, "test-token", "/v1/provision-lpr/status", &outSmall); err == nil {
		t.Fatalf("getAgentJSON expected to fail on payload size %d > 64KB", len(raw))
	}

	// With getAgentJSONWithLimit and maxProvisionLPRStatusBytes, it must succeed
	var outLarge map[string]any
	if err := getAgentJSONWithLimit(context.Background(), doer, "test-token", "/v1/provision-lpr/status", maxProvisionLPRStatusBytes, &outLarge); err != nil {
		t.Fatalf("getAgentJSONWithLimit failed with large limit: %v", err)
	}
	if len(outLarge["cameras"].([]any)) != 1 {
		t.Fatalf("expected 1 camera in status, got %v", outLarge)
	}
}
