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

	"github.com/jdolan-exalink/openvms/internal/authz"
)

const registryJSON = `{"enforce":true,"cameras":[{"camera_id":"cam-a","ip":"10.0.15.107"}],"unregistered_seen":[{"ip":"10.0.15.200","first_seen":"2026-10-24T10:00:00Z","last_seen":"2026-10-24T10:05:00Z","requests":3,"last_path":"/Vendor/Event","rejected":true}]}`

func TestRegisterProvisionLPRCameraSendsTheCameraAndItsAddressToTheAgent(t *testing.T) {
	var seen *http.Request
	var sent LPRCamera
	svc := videoEncodingService(func(r *http.Request) (*http.Response, error) {
		seen = r.Clone(r.Context())
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &sent)
		return agentReply(200, registryJSON)(r)
	})
	cameraID := uuid.New()
	reg, err := svc.RegisterProvisionLPRCamera(context.Background(), authz.Actor{}, uuid.New(), cameraID, "10.0.15.107")
	if err != nil {
		t.Fatal(err)
	}
	if seen.Method != http.MethodPost || seen.URL.Path != "/v1/provision-lpr/registry/cameras" || seen.Header.Get("Authorization") != "Bearer agent-token" || seen.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("request = %s %s headers %v", seen.Method, seen.URL.Path, seen.Header)
	}
	if sent.CameraID != cameraID.String() || sent.IP != "10.0.15.107" {
		t.Fatalf("the agent got %+v", sent)
	}
	if !reg.Enforce || len(reg.Cameras) != 1 || len(reg.Unregistered) != 1 || !reg.Unregistered[0].Rejected || reg.Unregistered[0].Requests != 3 {
		t.Fatalf("registry = %+v", reg)
	}
}

func TestLPRRegistryRoutesUseTheRightMethodAndPath(t *testing.T) {
	cameraID := uuid.New()
	for name, tc := range map[string]struct {
		call         func(*Service) error
		method, path string
		wantBodyJSON string
		answerStatus int
		answerBody   string
	}{
		"unregister": {
			call: func(s *Service) error {
				return s.UnregisterProvisionLPRCamera(context.Background(), authz.Actor{}, uuid.New(), cameraID)
			},
			method: http.MethodDelete, path: "/v1/provision-lpr/registry/cameras/" + cameraID.String(), answerStatus: 204,
		},
		"enforce on": {
			call: func(s *Service) error {
				_, err := s.SetProvisionLPREnforce(context.Background(), authz.Actor{}, uuid.New(), true)
				return err
			},
			method: http.MethodPut, path: "/v1/provision-lpr/registry/enforce", wantBodyJSON: `{"enforce":true}`, answerStatus: 200, answerBody: registryJSON,
		},
		"read": {
			call: func(s *Service) error {
				_, err := s.GetProvisionLPRRegistry(context.Background(), authz.Actor{}, uuid.New())
				return err
			},
			method: http.MethodGet, path: "/v1/provision-lpr/registry", answerStatus: 200, answerBody: registryJSON,
		},
	} {
		var seen *http.Request
		var body string
		svc := videoEncodingService(func(r *http.Request) (*http.Response, error) {
			seen = r.Clone(r.Context())
			if r.Body != nil {
				b, _ := io.ReadAll(r.Body)
				body = string(b)
			}
			return agentReply(tc.answerStatus, tc.answerBody)(r)
		})
		if err := tc.call(svc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if seen.Method != tc.method || seen.URL.Path != tc.path || seen.Header.Get("Authorization") != "Bearer agent-token" {
			t.Fatalf("%s: request = %s %s headers %v", name, seen.Method, seen.URL.Path, seen.Header)
		}
		if body != tc.wantBodyJSON {
			t.Fatalf("%s: body = %q, want %q", name, body, tc.wantBodyJSON)
		}
	}
}

func TestLPRRegistryTellsAnOldAgentFromAFailure(t *testing.T) {
	old := videoEncodingService(agentReply(404, "404 page not found"))
	if _, err := old.GetProvisionLPRRegistry(context.Background(), authz.Actor{}, uuid.New()); !errors.Is(err, ErrAgentRegistryUnsupported) {
		t.Fatalf("err = %v, want ErrAgentRegistryUnsupported for an agent without the routes", err)
	}

	conflict := videoEncodingService(agentReply(409, "another camera is registered with that address\n"))
	_, err := conflict.RegisterProvisionLPRCamera(context.Background(), authz.Actor{}, uuid.New(), uuid.New(), "10.0.15.107")
	var statusErr *AgentStatusError
	if !errors.As(err, &statusErr) || statusErr.Status != 409 || !strings.Contains(statusErr.Message, "another camera") {
		t.Fatalf("err = %v, want the agent's 409 and what it said", err)
	}

	garbled := videoEncodingService(agentReply(200, "not json"))
	if _, err := garbled.GetProvisionLPRRegistry(context.Background(), authz.Actor{}, uuid.New()); !errors.Is(err, ErrAgentProbeFailed) {
		t.Fatalf("err = %v, want ErrAgentProbeFailed for an unreadable answer", err)
	}
}

func TestLPRRegistryChecksPermissionBeforeCallingTheAgent(t *testing.T) {
	called := false
	svc := videoEncodingService(func(r *http.Request) (*http.Response, error) {
		called = true
		return agentReply(200, registryJSON)(r)
	})
	denied := errors.New("forbidden")
	svc.requireServerManage = func(context.Context, authz.Actor, uuid.UUID) error { return denied }
	if _, err := svc.SetProvisionLPREnforce(context.Background(), authz.Actor{}, uuid.New(), true); !errors.Is(err, denied) {
		t.Fatalf("err = %v, want the permission error", err)
	}
	if called {
		t.Fatal("the agent was called although the actor may not manage the server")
	}
}
