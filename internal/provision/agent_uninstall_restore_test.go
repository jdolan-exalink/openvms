package provision

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type uninstallRecorder struct {
	events   []string
	saved    string
	savedRst bool
	script   string
	scriptOK bool
}

type uninstallFakeConn struct{ rec *uninstallRecorder }

func (c uninstallFakeConn) Run(_ context.Context, cmd string) (string, error) {
	c.rec.events = append(c.rec.events, "ssh-run")
	c.rec.script = cmd
	if c.rec.scriptOK {
		return "openvms_uninstall=ok\n", nil
	}
	return "", errors.New("boom")
}
func (c uninstallFakeConn) WriteSFTPFile(context.Context, string, os.FileMode, []byte) error {
	return nil
}
func (c uninstallFakeConn) Close() error { return nil }

func newUninstallRestoreService(rec *uninstallRecorder, original string, found bool, readErr, saveErr error) *Service {
	rec.scriptOK = true
	return &Service{
		agentUninstallDial: func(context.Context, string, uint16, string, func(context.Context, string) error) (AgentInstallConn, error) {
			rec.events = append(rec.events, "ssh-dial")
			return uninstallFakeConn{rec}, nil
		},
		agentUninstallReadOriginal: func(context.Context, authz.Actor, uuid.UUID) (string, bool, error) {
			rec.events = append(rec.events, "read")
			return original, found, readErr
		},
		agentUninstallSaveConfig: func(_ context.Context, _ authz.Actor, _ uuid.UUID, yaml string) error {
			rec.events = append(rec.events, "save")
			rec.saved = yaml
			return saveErr
		},
		agentInstallJobs: map[uuid.UUID]*agentInstallJob{}, agentInstallByServer: map[uuid.UUID]uuid.UUID{},
	}
}

func runUninstallForTest(svc *Service, in AgentUninstallStartRequest) AgentInstallJob {
	serverID := uuid.New()
	job := &agentInstallJob{serverID: serverID, snapshot: AgentInstallJob{ID: uuid.New()}}
	svc.runServerAgentUninstall(context.Background(), authz.Actor{UserID: uuid.New()}, job, db.ServerAgent{ServerID: serverID, Host: "192.0.2.10"}, in)
	return job.read()
}

func TestUninstallRestoreOriginalPushesTheAgentCopyBeforeAnySSH(t *testing.T) {
	rec := &uninstallRecorder{}
	svc := newUninstallRestoreService(rec, "cameras: {}\n", true, nil, nil)
	snap := runUninstallForTest(svc, AgentUninstallStartRequest{SSHPort: 22, Password: "pw", ConfigAction: ConfigActionRestoreOriginal})
	if snap.Status != "succeeded" {
		t.Fatalf("job = %+v", snap)
	}
	if got := strings.Join(rec.events, ","); got != "read,save,ssh-dial,ssh-run" {
		t.Fatalf("order = %s, want the restore before any SSH", got)
	}
	if rec.saved != "cameras: {}\n" {
		t.Fatalf("saved %q, want exactly the agent's original", rec.saved)
	}
	if strings.Contains(rec.script, "/backup") {
		t.Fatal("the backup must be kept unless discard_backup is set")
	}
}

func TestUninstallRestoreOriginalWithoutBackupFailsClosed(t *testing.T) {
	rec := &uninstallRecorder{}
	svc := newUninstallRestoreService(rec, "", false, nil, nil)
	snap := runUninstallForTest(svc, AgentUninstallStartRequest{SSHPort: 22, Password: "pw", ConfigAction: ConfigActionRestoreOriginal})
	if snap.Status != "failed" || snap.Message != "No original configuration is stored on the agent; nothing was changed" {
		t.Fatalf("job = %+v", snap)
	}
	if got := strings.Join(rec.events, ","); got != "read" {
		t.Fatalf("events = %s, want only the read", got)
	}
}

func TestUninstallRestoreOriginalReadErrorFailsClosed(t *testing.T) {
	rec := &uninstallRecorder{}
	svc := newUninstallRestoreService(rec, "", false, errors.New("agent down"), nil)
	snap := runUninstallForTest(svc, AgentUninstallStartRequest{SSHPort: 22, Password: "pw", ConfigAction: ConfigActionRestoreOriginal})
	if snap.Status != "failed" || !strings.Contains(snap.Message, "nothing was changed") || strings.Contains(snap.Message, "agent down") {
		t.Fatalf("job = %+v", snap)
	}
	if got := strings.Join(rec.events, ","); got != "read" {
		t.Fatalf("events = %s", got)
	}
}

func TestUninstallRestoreOriginalSaveErrorNeverRunsSSH(t *testing.T) {
	rec := &uninstallRecorder{}
	svc := newUninstallRestoreService(rec, "a: 1\n", true, nil, errors.New("frigate rejected: secret"))
	snap := runUninstallForTest(svc, AgentUninstallStartRequest{SSHPort: 22, Password: "pw", ConfigAction: ConfigActionRestoreOriginal})
	if snap.Status != "failed" || snap.Message != "The original configuration could not be restored; the agent was not uninstalled" {
		t.Fatalf("job = %+v", snap)
	}
	if got := strings.Join(rec.events, ","); got != "read,save" {
		t.Fatalf("events = %s", got)
	}
}

func TestUninstallRestoreThenSSHFailureStillSaysItRestored(t *testing.T) {
	rec := &uninstallRecorder{}
	svc := newUninstallRestoreService(rec, "a: 1\n", true, nil, nil)
	rec.scriptOK = false
	snap := runUninstallForTest(svc, AgentUninstallStartRequest{SSHPort: 22, Password: "pw", ConfigAction: ConfigActionRestoreOriginal})
	if snap.Status != "failed" || !strings.Contains(snap.Message, "Failed to remove agent files on host") || !strings.Contains(snap.Message, "original Frigate configuration was already restored") {
		t.Fatalf("job = %+v", snap)
	}
}

func TestUninstallKeepIsUnchangedAndNeverTouchesTheBackup(t *testing.T) {
	for _, action := range []string{"", ConfigActionKeep} {
		rec := &uninstallRecorder{}
		svc := newUninstallRestoreService(rec, "a: 1\n", true, nil, nil)
		snap := runUninstallForTest(svc, AgentUninstallStartRequest{SSHPort: 22, Password: "pw", ConfigAction: action})
		if snap.Status != "succeeded" || snap.Message != "Agent uninstalled successfully" {
			t.Fatalf("action %q job = %+v", action, snap)
		}
		if got := strings.Join(rec.events, ","); got != "ssh-dial,ssh-run" {
			t.Fatalf("action %q events = %s, want no backup call and no save", action, got)
		}
		if rec.script != agentUninstallScript(false) {
			t.Fatalf("action %q ran a different script", action)
		}
	}
}

func TestUninstallDiscardBackupRunsTheDiscardingScript(t *testing.T) {
	rec := &uninstallRecorder{}
	svc := newUninstallRestoreService(rec, "", false, nil, nil)
	snap := runUninstallForTest(svc, AgentUninstallStartRequest{SSHPort: 22, Password: "pw", DiscardBackup: true})
	if snap.Status != "succeeded" || rec.script != agentUninstallScript(true) {
		t.Fatalf("job = %+v script=%q", snap, rec.script)
	}
}

func TestAgentUninstallScriptDiscardVariant(t *testing.T) {
	keep, discard := agentUninstallScript(false), agentUninstallScript(true)
	const want = "rm -rf /var/lib/openvms-agent/backup\n"
	if strings.Contains(keep, "backup") {
		t.Fatal("the keep script must not mention the backup")
	}
	i, j := strings.Index(discard, want), strings.Index(discard, "rmdir /var/lib/openvms-agent")
	if i < 0 || j < i {
		t.Fatalf("discard script must remove the backup before the rmdir:\n%s", discard)
	}
	if strings.Replace(discard, want, "", 1) != keep {
		t.Fatal("the discard script must differ from the keep script only by the backup removal")
	}
}

// The keep script is pinned byte for byte: it is what the uninstall ran before this option existed.
func TestAgentUninstallScriptKeepIsByteIdenticalToTheOriginal(t *testing.T) {
	const original = `set -e
if systemctl is-active --quiet openvms-agent.service 2>/dev/null; then
  systemctl stop openvms-agent.service
fi
if systemctl is-enabled --quiet openvms-agent.service 2>/dev/null; then
  systemctl disable openvms-agent.service
fi
rm -f /etc/systemd/system/openvms-agent.service
systemctl daemon-reload
rm -f /usr/local/bin/openvms-agent
rm -rf /var/lib/openvms-agent/agent-mtls
rmdir /var/lib/openvms-agent 2>/dev/null || true
rm -f /etc/openvms/agent-enroll.token
rm -rf /etc/openvms
printf 'openvms_uninstall=ok\n'
`
	if agentUninstallScript(false) != original {
		t.Fatal("the keep script changed")
	}
}

func TestAgentUninstallStartRequestValidatesConfigAction(t *testing.T) {
	for _, action := range []string{"", "keep", "restore_original"} {
		if err := validateAgentUninstallStartRequest(AgentUninstallStartRequest{SSHPort: 22, Password: "x", ConfigAction: action}); err != nil {
			t.Errorf("%q: %v", action, err)
		}
	}
	var ve *ValidationError
	if err := validateAgentUninstallStartRequest(AgentUninstallStartRequest{SSHPort: 22, Password: "x", ConfigAction: "wipe"}); !errors.As(err, &ve) {
		t.Fatalf("unknown action err = %v, want ValidationError", err)
	}
}

func TestAgentUninstallStartRequestStillRedactsWithTheNewFields(t *testing.T) {
	req := AgentUninstallStartRequest{SSHPort: 22, Password: "super-secret-root-password", ConfigAction: ConfigActionRestoreOriginal, DiscardBackup: true}
	b, _ := json.Marshal(req)
	for _, s := range []string{req.String(), req.GoString(), string(b)} {
		if strings.Contains(s, "super-secret") {
			t.Fatalf("leaks the password: %s", s)
		}
	}
}

func TestReadAgentOriginalConfig(t *testing.T) {
	h := newBackupHarness(func(string) (int, string) { return 200, backupFullJSON })
	cfg, found, err := h.readAgentOriginalConfig(context.Background(), authz.Actor{}, uuid.New())
	if err != nil || !found || cfg != "cameras: {}\n" {
		t.Fatalf("cfg=%q found=%v err=%v", cfg, found, err)
	}
	if len(h.requests) != 1 || h.requests[0] != "GET /v1/backup/original" {
		t.Fatalf("requests = %v", h.requests)
	}
	none := newBackupHarness(func(string) (int, string) { return http.StatusNotFound, `{"code":"no_backup"}` })
	if _, found, err := none.readAgentOriginalConfig(context.Background(), authz.Actor{}, uuid.New()); err != nil || found {
		t.Fatalf("found=%v err=%v, want not found without error", found, err)
	}
	old := newBackupHarness(func(string) (int, string) { return http.StatusNotFound, `{}` })
	if _, _, err := old.readAgentOriginalConfig(context.Background(), authz.Actor{}, uuid.New()); !errors.Is(err, ErrAgentBackupUnsupported) {
		t.Fatalf("err = %v", err)
	}
	empty := newBackupHarness(func(string) (int, string) { return 200, `{"config":"","sha256":"abc"}` })
	if _, found, _ := empty.readAgentOriginalConfig(context.Background(), authz.Actor{}, uuid.New()); found {
		t.Fatal("an empty config must not count as a restorable original")
	}
}
