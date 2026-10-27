package provision

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

func TestExecuteOnvifVideoDeviceForwardsTheOperationToTheAgent(t *testing.T) {
	var captured *http.Request
	var sent onvifdiscover.VideoDeviceRequest
	svc := videoEncodingService(func(r *http.Request) (*http.Response, error) {
		captured = r.Clone(r.Context())
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &sent)
		return agentReply(200, `{"success":true,"streams":[{"Token":"p1","Codec":"H264"}]}`)(r)
	})
	res, status, err := svc.ExecuteOnvifVideoDevice(context.Background(), authz.Actor{}, uuid.New(), onvifdiscover.VideoDeviceRequest{
		Endpoint: "http://192.0.2.8/onvif/device_service", Username: "admin", Password: "pw", Op: onvifdiscover.VideoDeviceOpStreams,
	})
	if err != nil || status != 200 || !res.Success || len(res.Streams) != 1 || res.Streams[0].Token != "p1" {
		t.Fatalf("status %d, result %+v, err %v", status, res, err)
	}
	if captured.URL.Path != "/v1/onvif/video-device" || captured.Header.Get("Authorization") != "Bearer agent-token" || sent.Op != "profile_streams" || sent.Password != "pw" {
		t.Fatalf("destination %s, sent %+v", captured.URL, sent)
	}
}

func TestExecuteOnvifVideoDeviceKeepsDomainFailuresAndReadsAMissingRouteAsOldAgent(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		code   string
	}{
		{422, `{"code":"validation_failed","violations":[{"Field":"fps","Code":"fps_out_of_range","Message":"x"}]}`, "validation_failed"},
		{409, `{"code":"not_applied","fields":["fps"]}`, "not_applied"},
		{502, `{"code":"camera_error"}`, "camera_error"},
		{400, `{"code":"bad_request"}`, "bad_request"},
	} {
		svc := videoEncodingService(agentReply(tc.status, tc.body))
		res, status, err := svc.ExecuteOnvifVideoDevice(context.Background(), authz.Actor{}, uuid.New(), onvifdiscover.VideoDeviceRequest{Endpoint: "http://192.0.2.8/x", Op: "profile_streams"})
		if err != nil || status != tc.status || res.Code != tc.code {
			t.Errorf("%d: status %d, result %+v, err %v", tc.status, status, res, err)
		}
	}
	for _, st := range []int{404, 405} {
		svc := videoEncodingService(agentReply(st, "404 page not found"))
		if _, _, err := svc.ExecuteOnvifVideoDevice(context.Background(), authz.Actor{}, uuid.New(), onvifdiscover.VideoDeviceRequest{Endpoint: "http://192.0.2.8/x", Op: "profile_streams"}); !errors.Is(err, ErrAgentOpUnsupported) {
			t.Errorf("%d: err = %v, want ErrAgentOpUnsupported", st, err)
		}
	}
}

func TestExecuteOnvifVideoDeviceAndRebootNeedServerManageAndSecrets(t *testing.T) {
	denied := errors.New("forbidden")
	svc := videoEncodingService(func(*http.Request) (*http.Response, error) {
		t.Fatal("the agent must not be called")
		return nil, nil
	})
	svc.requireServerConfigSecrets = func(context.Context, authz.Actor, uuid.UUID) error { return denied }
	if _, _, err := svc.ExecuteOnvifVideoDevice(context.Background(), authz.Actor{}, uuid.New(), onvifdiscover.VideoDeviceRequest{Endpoint: "http://192.0.2.8/x", Op: "profile_streams"}); !errors.Is(err, denied) {
		t.Errorf("video device: %v", err)
	}
	if err := svc.ExecuteOnvifReboot(context.Background(), authz.Actor{}, uuid.New(), onvifdiscover.RebootRequest{Endpoint: "http://192.0.2.8/x"}); !errors.Is(err, denied) {
		t.Errorf("reboot: %v", err)
	}
}

func TestExecuteOnvifReboot(t *testing.T) {
	var path string
	svc := videoEncodingService(func(r *http.Request) (*http.Response, error) {
		path = r.URL.Path
		return agentReply(200, `{"success":true}`)(r)
	})
	if err := svc.ExecuteOnvifReboot(context.Background(), authz.Actor{}, uuid.New(), onvifdiscover.RebootRequest{Endpoint: "http://192.0.2.8/x"}); err != nil || path != "/v1/onvif/reboot" {
		t.Fatalf("err %v path %s", err, path)
	}
	if err := videoEncodingService(agentReply(502, "camera reboot failed")).ExecuteOnvifReboot(context.Background(), authz.Actor{}, uuid.New(), onvifdiscover.RebootRequest{Endpoint: "http://192.0.2.8/x"}); !errors.Is(err, ErrAgentProbeFailed) {
		t.Errorf("failed reboot: %v", err)
	}
	if err := videoEncodingService(agentReply(404, "")).ExecuteOnvifReboot(context.Background(), authz.Actor{}, uuid.New(), onvifdiscover.RebootRequest{Endpoint: "http://192.0.2.8/x"}); !errors.Is(err, ErrAgentOpUnsupported) {
		t.Errorf("old agent: %v", err)
	}
}
