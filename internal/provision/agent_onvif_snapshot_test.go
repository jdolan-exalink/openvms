package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

func TestExecuteOnvifSnapshotReturnsOnlyJPEGBytes(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0xd9}
	var captured *http.Request
	var sent onvifdiscover.SnapshotRequest
	svc := &Service{
		requireServerManage:        func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		requireServerConfigSecrets: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		loadProbeAgent: func(context.Context, authz.Actor, uuid.UUID) (string, int32, string, AgentTLSConfig, error) {
			return "192.0.2.10", 7419, "agent-token", AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, nil
		},
		probeRoundTripper: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			captured = r.Clone(r.Context())
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &sent)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(bytes.NewReader(jpeg))}, nil
		}),
	}
	got, err := svc.ExecuteOnvifSnapshot(context.Background(), authz.Actor{}, uuid.New(), onvifdiscover.SnapshotRequest{
		Endpoint: "http://192.0.2.8/onvif/device_service", Username: "camera", Password: "pw", ProfileToken: "main",
	})
	if err != nil || !bytes.Equal(got, jpeg) {
		t.Fatalf("snapshot = %x, err = %v", got, err)
	}
	if captured == nil || captured.URL.Path != "/v1/onvif/snapshot" || captured.Header.Get("Authorization") != "Bearer agent-token" {
		t.Fatalf("unexpected agent request: %#v", captured)
	}
	if sent.ProfileToken != "main" || sent.Username != "camera" || sent.Password != "pw" {
		t.Fatalf("agent received request %+v", sent)
	}
}

func TestExecuteOnvifSnapshotRejectsInvalidAgentResponseAndAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		body        string
		deny        bool
	}{
		{name: "agent unauthorized", status: http.StatusUnauthorized, body: "unauthorized"},
		{name: "wrong content type", status: http.StatusOK, contentType: "text/plain", body: "fake"},
		{name: "oversized body", status: http.StatusOK, contentType: "image/jpeg", body: strings.Repeat("x", maxOnvifSnapshotResponseBytes+1)},
		{name: "manage denied", deny: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			svc := &Service{
				requireServerManage: func(context.Context, authz.Actor, uuid.UUID) error {
					if tc.deny {
						return errors.New("denied")
					}
					return nil
				},
				requireServerConfigSecrets: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
				loadProbeAgent: func(context.Context, authz.Actor, uuid.UUID) (string, int32, string, AgentTLSConfig, error) {
					return "192.0.2.10", 7419, "agent-token", AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}, nil
				},
				probeRoundTripper: roundTripFunc(func(*http.Request) (*http.Response, error) {
					called = true
					return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
				}),
			}
			_, err := svc.ExecuteOnvifSnapshot(context.Background(), authz.Actor{}, uuid.New(), onvifdiscover.SnapshotRequest{Endpoint: "http://192.0.2.8/device", ProfileToken: "main"})
			if err == nil || (tc.deny && called) {
				t.Fatalf("err = %v, agent called = %v", err, called)
			}
		})
	}
}
