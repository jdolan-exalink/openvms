package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/provision"
)

func TestAgentBackupUnsupportedIsAConflictWithItsOwnCode(t *testing.T) {
	status, code, msg := statusFor(fmt.Errorf("ensure: %w", provision.ErrAgentBackupUnsupported))
	if status != http.StatusConflict || code != "agent_backup_unsupported" || msg != "update the agent to enable backups" {
		t.Fatalf("statusFor = %d %s %q", status, code, msg)
	}
}

func TestServerAgentBackupCarriesOnlyMetadata(t *testing.T) {
	none := serverAgentBackup(provision.AgentBackup{})
	if none.Exists || none.SavedAt != nil {
		t.Fatalf("no backup = %+v", none)
	}
	at := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	got := serverAgentBackup(provision.AgentBackup{Exists: true, SavedAt: at, FrigateVersion: "0.16.1", RegisteredBy: "OpenVMS", SHA256: "abc", Size: 42})
	if !got.Exists || got.SavedAt == nil || !got.SavedAt.Equal(at) || got.FrigateVersion != "0.16.1" || got.RegisteredBy != "OpenVMS" || got.Sha256 != "abc" || got.Size != 42 {
		t.Fatalf("backup = %+v", got)
	}
}
