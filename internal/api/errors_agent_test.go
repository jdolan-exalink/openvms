package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/provision"
)

// An unreachable agent is answered with its cause, so the operator learns whether to look at a
// firewall, a closed port or a blocked agent instead of a bare "could not reach".
func TestUnreachableAgentErrorCarriesTheDiagnosis(t *testing.T) {
	err := fmt.Errorf("connect: %w", &provision.AgentUnreachableError{Detail: "192.168.70.162:7419 no responde a nivel de red: firewall"})
	status, code, msg := statusFor(err)
	if status != http.StatusBadGateway || code != "agent_unreachable" {
		t.Fatalf("status %d code %q", status, code)
	}
	if !strings.Contains(msg, "OpenVMS could not reach the agent at that address") || !strings.Contains(msg, "firewall") {
		t.Fatalf("message %q lost the cause", msg)
	}
}

func TestUnreachableAgentWithoutDetailKeepsTheShortMessage(t *testing.T) {
	_, _, msg := statusFor(provision.ErrAgentConnectUnreachable)
	if msg != "OpenVMS could not reach the agent at that address" {
		t.Fatalf("message %q", msg)
	}
	if _, _, m := statusFor(errors.New("other")); strings.Contains(m, "reach the agent") {
		t.Fatalf("unrelated errors must not be mapped here: %q", m)
	}
}
