package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The version the control plane expects and the one the agent reports both come from
// deploy/agent/VERSION: there is no second place to forget (it used to be a separate string that
// fell behind the shipped release).
func TestVersionIsTheShippedAgentVersion(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "deploy", "agent", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.TrimSpace(string(data)); Version != want {
		t.Fatalf("agent.Version = %q, deploy/agent/VERSION = %q", Version, want)
	}
}
