package provision

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type fakeConn struct {
	job               *job
	cmds              []string
	files             map[string][]byte
	modes             map[string]os.FileMode
	facts             string
	warningAtPackages string
	warningAtCompose  string
}

func (f *fakeConn) Run(_ context.Context, cmd string) (string, error) {
	f.cmds = append(f.cmds, cmd)
	if strings.Contains(cmd, "install.sh packages") {
		f.warningAtPackages = f.job.warning()
	}
	if strings.Contains(cmd, "install.sh compose") {
		f.warningAtCompose = f.job.warning()
	}
	if strings.Contains(cmd, "install.sh detect") {
		return f.facts, nil
	}
	return "active\n", nil
}

func (f *fakeConn) WriteFile(_ context.Context, path string, mode os.FileMode, data []byte) error {
	if f.files == nil {
		f.files = map[string][]byte{}
		f.modes = map[string]os.FileMode{}
	}
	f.files[path] = append([]byte(nil), data...)
	f.modes[path] = mode
	return nil
}

func (f *fakeConn) Close() error { return nil }

func TestExecCPUWarnsBeforeComposeAndNeverRecordsThePassword(t *testing.T) {
	const secret = "session-secret"
	j := newJob(uuid.New(), "192.0.2.10")
	conn := &fakeConn{job: j, facts: "coral=0 nvidia=0 openvino=0\n"}
	var registered Variant
	Exec(context.Background(), j, secret, conn, []byte("\x7fELF-test"), func(_ context.Context, token string, variant Variant) (uuid.UUID, error) {
		if token == "" || token == secret {
			t.Fatalf("token was empty or equal to the ssh secret")
		}
		registered = variant
		return uuid.MustParse("11111111-1111-1111-1111-111111111111"), nil
	})
	snap := j.snapshot()
	if snap.Status != statusSucceeded || snap.Warning != "cpu" || snap.Variant != string(VariantCPU) {
		t.Fatalf("%#v", snap)
	}
	if conn.warningAtPackages != "cpu" || conn.warningAtCompose != "cpu" {
		t.Fatalf("warning was not visible before compose: packages=%q compose=%q", conn.warningAtPackages, conn.warningAtCompose)
	}
	if registered != VariantCPU {
		t.Fatalf("registered %s", registered)
	}
	if conn.modes["/etc/openvms/agent.token"] != 0o600 {
		t.Fatalf("token mode %o", conn.modes["/etc/openvms/agent.token"])
	}
	blob, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), secret) || strings.Contains(string(blob), "password") {
		t.Fatal("job snapshot contains the secret or a password field")
	}
	if strings.Contains(strings.Join(conn.cmds, "\n"), secret) {
		t.Fatal("ssh command contains the secret")
	}
	if strings.Contains(string(conn.files["/etc/openvms/agent.token"]), secret) {
		t.Fatal("agent token file contains the ssh secret")
	}
	joined := strings.Join(conn.cmds, "\n")
	if !strings.Contains(joined, "VARIANT=cpu bash /opt/openvms/agent/install.sh compose") {
		t.Fatalf("compose command missing: %s", joined)
	}
	if !strings.Contains(joined, "install.sh ntp") || !strings.Contains(joined, "install.sh agent") {
		t.Fatalf("ntp or agent step missing: %s", joined)
	}
}

func TestExecNVIDIADoesNotWarn(t *testing.T) {
	j := newJob(uuid.New(), "192.0.2.11")
	conn := &fakeConn{job: j, facts: "coral=0 nvidia=1 openvino=1\n"}
	Exec(context.Background(), j, "other-secret", conn, []byte("\x7fELF"), func(context.Context, string, Variant) (uuid.UUID, error) {
		return uuid.New(), nil
	})
	snap := j.snapshot()
	if snap.Warning != "" || snap.Variant != string(VariantTensorRT) {
		t.Fatalf("%#v", snap)
	}
}
