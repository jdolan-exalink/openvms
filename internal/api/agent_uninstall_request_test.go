package api

import (
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

func TestAgentUninstallStartRequestDefaults(t *testing.T) {
	pw := "pw"
	got := agentUninstallStartRequest(&gen.ServerAgentUninstallRequest{SshPort: 22, SshPassword: &pw})
	if got.SSHPort != 22 || got.Password != "pw" || got.ConfigAction != "keep" || got.DiscardBackup {
		t.Fatalf("defaults = %#v, want keep and no discard", got)
	}
	action, discard := gen.ServerAgentUninstallRequestConfigAction("restore_original"), true
	got = agentUninstallStartRequest(&gen.ServerAgentUninstallRequest{SshPort: 2222, SshPassword: &pw, ConfigAction: &action, DiscardBackup: &discard})
	if got.ConfigAction != "restore_original" || !got.DiscardBackup || got.SSHPort != 2222 {
		t.Fatalf("mapped = %#v", got)
	}
	bogus := gen.ServerAgentUninstallRequestConfigAction("wipe")
	if got = agentUninstallStartRequest(&gen.ServerAgentUninstallRequest{SshPort: 22, SshPassword: &pw, ConfigAction: &bogus}); got.ConfigAction != "wipe" {
		t.Fatalf("an unknown action must reach the service validation, got %q", got.ConfigAction)
	}
}
