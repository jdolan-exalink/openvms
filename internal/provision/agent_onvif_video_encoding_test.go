package provision

import (
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

func videoEncodingService(transport roundTripFunc) *Service {
	return &Service{
		requireServerManage:        func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		requireServerConfigSecrets: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		loadProbeAgent: func(context.Context, authz.Actor, uuid.UUID) (string, int32, string, AgentTLSConfig, error) {
			return "192.0.2.10", 7419, "agent-token", AgentTLSConfig{SecurePort: 7419, TrustMode: AgentTLSTrustPlaintext}, nil
		},
		probeRoundTripper: transport,
	}
}

func agentReply(status int, body string) roundTripFunc {
	return func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}
}

func TestExecuteOnvifVideoEncodingForwardsTheRequestToTheAgent(t *testing.T) {
	var captured *http.Request
	var sent onvifdiscover.VideoEncodingRequest
	svc := videoEncodingService(func(r *http.Request) (*http.Response, error) {
		captured = r.Clone(r.Context())
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &sent)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"success":true,"supported":true,"previous_encoding":"H265","encoding":"H264"}`))}, nil
	})
	res, status, err := svc.ExecuteOnvifVideoEncoding(context.Background(), authz.Actor{}, uuid.New(), onvifdiscover.VideoEncodingRequest{
		Endpoint: "http://192.0.2.8/onvif/device_service", Username: "admin", Password: "pw", Action: "set", ConfigToken: "enc_main", Encoding: "H264",
	})
	if err != nil || status != 200 || !res.Success || res.PreviousEncoding != "H265" || res.Encoding != "H264" {
		t.Fatalf("status %d, result %+v, err %v", status, res, err)
	}
	if captured.URL.Host != "192.0.2.10:7419" || captured.URL.Path != "/v1/onvif/video-encoding" || captured.Header.Get("Authorization") != "Bearer agent-token" {
		t.Fatalf("destination %s, headers %v", captured.URL, captured.Header)
	}
	if sent.Action != "set" || sent.ConfigToken != "enc_main" || sent.Encoding != "H264" || sent.Username != "admin" || sent.Password != "pw" {
		t.Fatalf("the agent got %+v", sent)
	}
}

func TestExecuteOnvifVideoEncodingKeepsTheAgentsAnswerToADomainFailure(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		code   string
	}{
		{422, `{"success":false,"supported":true,"code":"encoding_unsupported"}`, "encoding_unsupported"},
		{409, `{"success":false,"supported":true,"code":"encoding_unchanged"}`, "encoding_unchanged"},
		{404, `{"success":false,"supported":true,"code":"configuration_not_found"}`, "configuration_not_found"},
		{502, `{"success":false,"supported":true,"code":"not_applied","previous_encoding":"H265"}`, "not_applied"},
		{400, `{"success":false,"code":"encoding_must_be_h264_or_h265"}`, "encoding_must_be_h264_or_h265"},
	} {
		svc := videoEncodingService(agentReply(tc.status, tc.body))
		res, status, err := svc.ExecuteOnvifVideoEncoding(context.Background(), authz.Actor{}, uuid.New(), onvifdiscover.VideoEncodingRequest{Endpoint: "http://192.0.2.8/onvif/device_service", Action: "set", ConfigToken: "t", Encoding: "H264"})
		if err != nil || status != tc.status || res.Success || res.Code != tc.code {
			t.Errorf("%d: status %d, result %+v, err %v", tc.status, status, res, err)
		}
	}
}

func TestExecuteOnvifVideoEncodingFailsWhenTheAgentCannotBeTrusted(t *testing.T) {
	req := onvifdiscover.VideoEncodingRequest{Endpoint: "http://192.0.2.8/onvif/device_service", Action: "options"}
	t.Run("unreachable agent", func(t *testing.T) {
		svc := videoEncodingService(func(*http.Request) (*http.Response, error) { return nil, errors.New("dial tcp: refused") })
		if _, _, err := svc.ExecuteOnvifVideoEncoding(context.Background(), authz.Actor{}, uuid.New(), req); !errors.Is(err, ErrAgentProbeUnavailable) {
			t.Fatalf("err = %v, want ErrAgentProbeUnavailable", err)
		}
	})
	t.Run("agent rejects our token", func(t *testing.T) {
		svc := videoEncodingService(agentReply(401, "unauthorized"))
		if _, _, err := svc.ExecuteOnvifVideoEncoding(context.Background(), authz.Actor{}, uuid.New(), req); !errors.Is(err, ErrAgentProbeFailed) {
			t.Fatalf("err = %v, want ErrAgentProbeFailed", err)
		}
	})
	t.Run("garbage answer", func(t *testing.T) {
		svc := videoEncodingService(agentReply(200, "<html>nope</html>"))
		if _, _, err := svc.ExecuteOnvifVideoEncoding(context.Background(), authz.Actor{}, uuid.New(), req); !errors.Is(err, ErrAgentProbeFailed) {
			t.Fatalf("err = %v, want ErrAgentProbeFailed", err)
		}
	})
}

func TestExecuteOnvifVideoEncodingNeedsServerManageAndConfigSecrets(t *testing.T) {
	denied := errors.New("forbidden")
	for name, mutate := range map[string]func(*Service){
		"manage": func(s *Service) {
			s.requireServerManage = func(context.Context, authz.Actor, uuid.UUID) error { return denied }
		},
		"secrets": func(s *Service) {
			s.requireServerConfigSecrets = func(context.Context, authz.Actor, uuid.UUID) error { return denied }
		},
	} {
		called := false
		svc := videoEncodingService(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("must not be called")
		})
		mutate(svc)
		_, _, err := svc.ExecuteOnvifVideoEncoding(context.Background(), authz.Actor{}, uuid.New(), onvifdiscover.VideoEncodingRequest{Endpoint: "http://192.0.2.8/onvif/device_service", Action: "set", ConfigToken: "t", Encoding: "H264"})
		if !errors.Is(err, denied) || called {
			t.Errorf("%s: err = %v, transport called = %v", name, err, called)
		}
	}
}
