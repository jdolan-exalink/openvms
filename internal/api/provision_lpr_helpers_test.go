package api

import (
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/provision"
)

func agentStatus(cameras ...map[string]any) map[string]any {
	list := make([]any, 0, len(cameras))
	for _, c := range cameras {
		list = append(list, c)
	}
	return map[string]any{"cameras": list}
}

func TestFindProvisionCameraStatusMatchesByRegisteredIDAddressOrName(t *testing.T) {
	id := uuid.New()
	byID := map[string]any{"camera_id": id.String(), "camera_ip": "10.0.15.9", "online": true}
	byIP := map[string]any{"camera_id": "", "camera_ip": "10.0.15.107", "online": true}
	byName := map[string]any{"camera_id": "patio", "camera_ip": "10.0.15.8", "online": false}
	other := map[string]any{"camera_id": "another", "camera_ip": "10.0.15.50"}

	if got := findProvisionCameraStatus(agentStatus(other, byID), id, "10.0.15.107", "remote", "Display"); got["camera_ip"] != "10.0.15.9" {
		t.Fatalf("by registered id: %v", got)
	}
	if got := findProvisionCameraStatus(agentStatus(other, byIP), id, "10.0.15.107", "remote", "Display"); got["camera_ip"] != "10.0.15.107" {
		t.Fatalf("by address: %v", got)
	}
	if got := findProvisionCameraStatus(agentStatus(other, byName), id, "10.0.15.200", "patio", "Display"); got["camera_id"] != "patio" {
		t.Fatalf("by remote name: %v", got)
	}
	if got := findProvisionCameraStatus(agentStatus(other), id, "10.0.15.107", "remote", "Display"); got != nil {
		t.Fatalf("matched a camera that is not there: %v", got)
	}
}

func TestFindProvisionCameraStatusToleratesUnexpectedShapes(t *testing.T) {
	id := uuid.New()
	for name, status := range map[string]map[string]any{
		"nil":           nil,
		"no cameras":    {},
		"wrong type":    {"cameras": "nope"},
		"wrong element": {"cameras": []any{"x", 3, nil}},
	} {
		if got := findProvisionCameraStatus(status, id, "10.0.15.107", "r", "d"); got != nil {
			t.Fatalf("%s: got %v, want nil", name, got)
		}
	}
	// An empty address and empty names must never match an entry that also has them empty.
	blank := agentStatus(map[string]any{"camera_id": "", "camera_ip": ""})
	if got := findProvisionCameraStatus(blank, id, "", "", ""); got != nil {
		t.Fatalf("an empty identity matched %v", got)
	}
}

func TestRegistrationOutcomeNamesWhatWentWrong(t *testing.T) {
	if got := registrationOutcome(nil); !got.Registered || got.Error != "" {
		t.Fatalf("success: %+v", got)
	}
	if got := registrationOutcome(provision.ErrAgentRegistryUnsupported); got.Registered || got.Error != "agent_outdated" {
		t.Fatalf("old agent: %+v", got)
	}
	if got := registrationOutcome(&provision.AgentStatusError{Status: 409, Message: "another camera is registered with that address"}); got.Registered || got.Error != "another camera is registered with that address" {
		t.Fatalf("conflict: %+v", got)
	}
	if got := registrationOutcome(provision.ErrAgentProbeUnavailable); got.Registered || got.Error != "agent_unreachable" {
		t.Fatalf("unreachable: %+v", got)
	}
}

func TestFinishProvisionConfigureOnlyClaimsSuccessWhenTheCameraReportsIn(t *testing.T) {
	id := uuid.New()
	base := func() map[string]any {
		return map[string]any{"success": false, "method": "manual_instructions", "message": "enter these values", "parameters": map[string]any{"Server Port": "7419"}}
	}

	waiting := finishProvisionConfigure(base(), id, registrationResult{Registered: true}, map[string]any{"online": false})
	if waiting["success"] != false || waiting["verified"] != false || waiting["registered"] != true || waiting["camera_id"] != id.String() || waiting["message"] != "enter these values" {
		t.Fatalf("camera not online yet: %v", waiting)
	}
	if _, has := waiting["registration_error"]; has {
		t.Fatalf("registration_error present without an error: %v", waiting)
	}

	unseen := finishProvisionConfigure(base(), id, registrationResult{Registered: true}, nil)
	if unseen["success"] != false || unseen["verified"] != false {
		t.Fatalf("camera never seen: %v", unseen)
	}

	done := finishProvisionConfigure(base(), id, registrationResult{Registered: true}, map[string]any{"online": true})
	if done["success"] != true || done["verified"] != true || done["method"] != "verified" || done["message"] == "enter these values" {
		t.Fatalf("camera reporting: %v", done)
	}

	failed := finishProvisionConfigure(base(), id, registrationResult{Error: "agent_outdated"}, map[string]any{"online": true})
	if failed["registered"] != false || failed["registration_error"] != "agent_outdated" || failed["verified"] != true {
		t.Fatalf("registration failed: %v", failed)
	}
}

func TestTargetHostFromBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://10.1.1.5:8971":        "10.1.1.5",
		"http://frigate.local/":        "frigate.local",
		"https://[2001:db8::5]:8971/x": "2001:db8::5",
		"":                             "",
		"not a url":                    "",
		"http://":                      "",
	} {
		if got := targetHostFromBaseURL(in); got != want {
			t.Fatalf("targetHostFromBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}
