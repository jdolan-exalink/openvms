package provision

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"

	agentbundle "github.com/jdolan-exalink/openvms/deploy/agent"
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
	if strings.Contains(cmd, "openvms_preflight=ok") {
		mode := "mounted"
		if strings.Contains(cmd, "OPENVMS_ALLOW_SYSTEM_DISK=1") {
			mode = "system_disk"
		}
		return "openvms_preflight=ok os_id=ubuntu goarch=" + runtime.GOARCH + " recordings=" + mode + "\n", nil
	}
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
	Exec(context.Background(), j, secret, conn, []byte("\x7fELF-test"), false, func(_ context.Context, token string, variant Variant) (uuid.UUID, error) {
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
	Exec(context.Background(), j, "other-secret", conn, []byte("\x7fELF"), false, func(context.Context, string, Variant) (uuid.UUID, error) {
		return uuid.New(), nil
	})
	snap := j.snapshot()
	if snap.Warning != "" || snap.Variant != string(VariantTensorRT) {
		t.Fatalf("%#v", snap)
	}
}

func TestExecSystemDiskDemoRequiresOptInAndWarnsBeforeMutation(t *testing.T) {
	j := newJob(uuid.New(), "192.0.2.14")
	conn := &fakeConn{job: j, facts: "coral=0 nvidia=0 openvino=0\n"}
	Exec(context.Background(), j, "demo-secret", conn, []byte("\x7fELF"), true, func(context.Context, string, Variant) (uuid.UUID, error) { return uuid.New(), nil })
	if got := j.snapshot().Warning; got != "cpu_system_disk" {
		t.Fatalf("system-disk and CPU warnings were not retained: %q", got)
	}
	commands := strings.Join(conn.cmds, "\n")
	for _, want := range []string{"OPENVMS_ALLOW_SYSTEM_DISK=1", "install.sh packages", "install.sh compose"} {
		if !strings.Contains(commands, want) {
			t.Fatalf("demo install did not propagate opt-in to %q", want)
		}
	}
}

type preflightConn struct {
	commands     []string
	writes       int
	preflightErr error
}

func (c *preflightConn) Run(_ context.Context, cmd string) (string, error) {
	c.commands = append(c.commands, cmd)
	if strings.Contains(cmd, "openvms_preflight=ok") {
		if c.preflightErr != nil {
			return "", c.preflightErr
		}
		mode := "mounted"
		if strings.Contains(cmd, "OPENVMS_ALLOW_SYSTEM_DISK=1") {
			mode = "system_disk"
		}
		return "openvms_preflight=ok os_id=ubuntu goarch=" + runtime.GOARCH + " recordings=" + mode + "\n", nil
	}
	if strings.Contains(cmd, "install.sh detect") {
		return "coral=0 nvidia=0 openvino=0\n", nil
	}
	return "active\n", nil
}

func TestParseHostPlatformFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		wantErr bool
	}{
		{name: "supported ubuntu", out: "openvms_preflight=ok os_id=ubuntu goarch=" + runtime.GOARCH + " recordings=mounted"},
		{name: "supported debian", out: "openvms_preflight=ok os_id=debian goarch=" + runtime.GOARCH + " recordings=mounted"},
		{name: "missing proof", out: "os_id=ubuntu goarch=" + runtime.GOARCH, wantErr: true},
		{name: "unsupported distro", out: "openvms_preflight=ok os_id=alpine goarch=" + runtime.GOARCH, wantErr: true},
		{name: "unsupported architecture", out: "openvms_preflight=ok os_id=ubuntu goarch=arm", wantErr: true},
		{name: "mismatched agent architecture", out: mismatchedHostPlatform(), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseHostPlatform(tt.out, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseHostPlatform error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestSystemDiskPreflightRequiresExplicitOptIn(t *testing.T) {
	output := "openvms_preflight=ok os_id=ubuntu goarch=" + runtime.GOARCH + " recordings=system_disk"
	if _, err := parseHostPlatform(output, false); err == nil {
		t.Fatal("system-disk fallback must be rejected without an explicit request")
	}
	platform, err := parseHostPlatform(output, true)
	if err != nil || platform.StorageMode != "system_disk" {
		t.Fatalf("explicit demo opt-in was not accepted: platform=%#v err=%v", platform, err)
	}
}

func TestFrigateInstallUsesRequestedPersistentLayout(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	installer := read("../../deploy/agent/install.sh")
	for _, want := range []string{"FRIGATE=/opt/frigate", "RECORDINGS=/mnt/cctv", "docker-compose.yml", "OPENVMS_CCTV_PATH=${RECORDINGS}", "OPENVMS_DB_PATH=${FRIGATE}/config"} {
		if !strings.Contains(installer, want) {
			t.Errorf("installer must include %q", want)
		}
	}
	for _, path := range []string{"../../apps/edge-agent/main.go", "../agent/sample.go"} {
		source := read(path)
		if !strings.Contains(source, `"/mnt/cctv"`) || !strings.Contains(source, `"/opt/frigate/config"`) {
			t.Errorf("agent defaults in %s must match the installed CCTV and database paths", path)
		}
	}
	for _, variant := range []string{"cpu", "stable", "openvino", "tensorrt"} {
		compose := read("../../deploy/agent/compose/frigate-" + variant + ".yml")
		if !strings.Contains(compose, "/mnt/cctv:/media/frigate") {
			t.Errorf("%s compose must persist recordings at /mnt/cctv", variant)
		}
	}
	preflight := read("../../deploy/agent/preflight.sh")
	for _, guard := range []string{"/opt/frigate", "mountpoint -q /mnt/cctv"} {
		if !strings.Contains(preflight, guard) {
			t.Errorf("preflight must guard %q", guard)
		}
	}
}

func mismatchedHostPlatform() string {
	if runtime.GOARCH == "amd64" {
		return "openvms_preflight=ok os_id=ubuntu goarch=arm64 recordings=mounted"
	}
	return "openvms_preflight=ok os_id=ubuntu goarch=amd64 recordings=mounted"
}

func (c *preflightConn) WriteFile(context.Context, string, os.FileMode, []byte) error {
	c.writes++
	return nil
}
func (c *preflightConn) Close() error { return nil }

func TestExecPreflightsFreshHostBeforeAnyMutation(t *testing.T) {
	j := newJob(uuid.New(), "192.0.2.12")
	conn := &preflightConn{}
	Exec(context.Background(), j, "one-time-secret", conn, []byte("\x7fELF"), false, func(context.Context, string, Variant) (uuid.UUID, error) { return uuid.New(), nil })
	if len(conn.commands) == 0 || !strings.Contains(conn.commands[0], "openvms_preflight=ok") {
		t.Fatalf("host preflight was not the first SSH command: %#v", conn.commands)
	}
	if conn.writes == 0 || j.snapshot().Status != statusSucceeded {
		t.Fatalf("fresh supported host did not proceed after preflight: writes=%d snapshot=%#v", conn.writes, j.snapshot())
	}
}

func TestExecRefusesExistingFrigateBeforeAnyMutation(t *testing.T) {
	j := newJob(uuid.New(), "192.0.2.13")
	conn := &preflightConn{preflightErr: errors.New("existing Frigate detected")}
	Exec(context.Background(), j, "one-time-secret", conn, []byte("\x7fELF"), false, func(context.Context, string, Variant) (uuid.UUID, error) {
		t.Fatal("existing host must not register")
		return uuid.Nil, nil
	})
	if conn.writes != 0 || len(conn.commands) != 1 || j.snapshot().Status != statusFailed {
		t.Fatalf("unsafe host was mutated or did not fail closed: writes=%d commands=%#v snapshot=%#v", conn.writes, conn.commands, j.snapshot())
	}
}

// Containers (LXC) cannot adjust the clock, so chronyd must serve time without
// touching it there, or the NTP step fails on every containerized host.
func TestNTPStepServesTimeWithoutClockControlInContainers(t *testing.T) {
	data, err := os.ReadFile("../../deploy/agent/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	installer := string(data)
	for _, want := range []string{"systemd-detect-virt --container", "-x", "/etc/default/chrony"} {
		if !strings.Contains(installer, want) {
			t.Errorf("ntp step must handle containers: missing %q", want)
		}
	}
}

// Every new host runs its own MQTT broker for Frigate. It is anonymous, so it must
// not be published beyond the host itself.
func TestFrigateInstallDeploysLocalMQTTBroker(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for _, variant := range []string{"cpu", "stable", "openvino", "tensorrt"} {
		compose := read("../../deploy/agent/compose/frigate-" + variant + ".yml")
		for _, want := range []string{"mosquitto:", "eclipse-mosquitto:2", "/opt/frigate/mosquitto/mosquitto.conf:/mosquitto/config/mosquitto.conf:ro", `"127.0.0.1:1883:1883"`, "depends_on: [mosquitto]"} {
			if !strings.Contains(compose, want) {
				t.Errorf("%s compose must include %q", variant, want)
			}
		}
	}
	for _, cfg := range []string{"cpu", "coral", "openvino", "tensorrt"} {
		config := read("../../deploy/agent/config/" + cfg + ".yml")
		if !strings.Contains(config, "mqtt:\n  enabled: true\n  host: mosquitto\n  port: 1883\n") {
			t.Errorf("%s Frigate config must publish to the local broker", cfg)
		}
	}
	if !strings.Contains(read("../../deploy/agent/install.sh"), `cp "${ROOT}/mosquitto.conf" "${FRIGATE}/mosquitto/mosquitto.conf"`) {
		t.Error("installer must place the broker config next to the Frigate compose")
	}
	if _, err := fs.ReadFile(agentbundle.FS, "mosquitto.conf"); err != nil {
		t.Errorf("broker config must ship in the agent bundle: %v", err)
	}
}
