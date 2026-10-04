package provision

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/deploy/agent"
	"github.com/jdolan-exalink/openvms/internal/agent"
)

const remoteRoot = "/opt/openvms/agent"

// Exec runs the install over an already open connection.
// password is used only to strip it from text that is stored on the job.
func Exec(ctx context.Context, j *job, password string, conn Conn, binary []byte, allowSystemDisk bool, register func(context.Context, string, Variant) (uuid.UUID, error)) {
	defer func() { password = "" }()
	fail := func(step string, err error) {
		j.fail(step, err.Error(), password)
	}
	platform, err := preflightHost(ctx, conn, allowSystemDisk)
	if err != nil {
		fail(stepConnecting, err)
		return
	}
	if platform.StorageMode == "system_disk" {
		j.addWarning("system_disk")
	}
	j.setStep(stepConnecting, stateDone, "")

	if err := uploadBundle(ctx, conn); err != nil {
		fail(stepConnecting, err)
		return
	}
	if facts, err := probe(ctx, conn); err == nil && Choose(facts) == VariantCPU {
		j.addWarning("cpu")
	}

	j.setStep(stepPackages, stateRunning, "")
	if _, err := runStep(ctx, conn, 40*time.Minute, installerCommand(allowSystemDisk, "packages")); err != nil {
		fail(stepPackages, err)
		return
	}
	j.setStep(stepPackages, stateDone, "")

	j.setStep(stepHardware, stateRunning, "")
	facts, err := probe(ctx, conn)
	if err != nil {
		fail(stepHardware, err)
		return
	}
	variant := Choose(facts)
	j.setVariant(variant)
	if variant == VariantCPU {
		j.addWarning("cpu")
	} else {
		// Preserve the storage-mode warning if the operator opted into the demo fallback.
	}
	j.setStep(stepHardware, stateDone, string(variant))

	j.setStep(stepCompose, stateRunning, string(variant))
	compose := fmt.Sprintf("%sVARIANT=%s bash %s/install.sh compose", systemDiskEnv(allowSystemDisk), variant, remoteRoot)
	if _, err := runStep(ctx, conn, 40*time.Minute, compose); err != nil {
		fail(stepCompose, err)
		return
	}
	j.setStep(stepCompose, stateDone, string(variant))

	j.setStep(stepNTP, stateRunning, "")
	if _, err := runStep(ctx, conn, 3*time.Minute, "bash "+remoteRoot+"/install.sh ntp"); err != nil {
		fail(stepNTP, err)
		return
	}
	j.setStep(stepNTP, stateDone, "")

	j.setStep(stepAgent, stateRunning, "")
	token, err := newToken()
	if err != nil {
		fail(stepAgent, err)
		return
	}
	if err := conn.WriteFile(ctx, "/usr/local/bin/openvms-agent", 0o755, binary); err != nil {
		fail(stepAgent, err)
		return
	}
	if err := conn.WriteFile(ctx, "/etc/openvms/agent.token", 0o600, []byte(token+"\n")); err != nil {
		fail(stepAgent, err)
		return
	}
	agentCmd := fmt.Sprintf("VARIANT=%s bash %s/install.sh agent", variant, remoteRoot)
	if _, err := runStep(ctx, conn, 3*time.Minute, agentCmd); err != nil {
		fail(stepAgent, err)
		return
	}
	j.setStep(stepAgent, stateDone, agent.Version)

	j.setStep(stepRegister, stateRunning, "")
	serverID, err := register(ctx, token, variant)
	if err != nil {
		fail(stepRegister, err)
		return
	}
	j.setServer(serverID)
	j.setStep(stepRegister, stateDone, "")
	j.succeed()
	token = ""
}

type hostPlatform struct {
	OSID        string
	GOARCH      string
	StorageMode string
}

func preflightHost(ctx context.Context, conn Conn, allowSystemDisk bool) (hostPlatform, error) {
	script, err := fs.ReadFile(agentbundle.FS, "preflight.sh")
	if err != nil {
		return hostPlatform{}, fmt.Errorf("read host preflight: %w", err)
	}
	cmd := fmt.Sprintf("%sOPENVMS_EXPECTED_GOARCH=%s bash -c %s", systemDiskEnv(allowSystemDisk), runtime.GOARCH, quote(string(script)))
	out, err := runStep(ctx, conn, time.Minute, cmd)
	if err != nil {
		return hostPlatform{}, err
	}
	return parseHostPlatform(out, allowSystemDisk)
}

func parseHostPlatform(out string, allowSystemDisk bool) (hostPlatform, error) {
	fields := make(map[string]string)
	for _, token := range strings.Fields(out) {
		key, value, ok := strings.Cut(token, "=")
		if ok {
			fields[key] = value
		}
	}
	if fields["openvms_preflight"] != "ok" {
		return hostPlatform{}, fmt.Errorf("host preflight did not confirm a supported fresh host")
	}
	platform := hostPlatform{OSID: fields["os_id"], GOARCH: fields["goarch"], StorageMode: fields["recordings"]}
	if platform.StorageMode != "mounted" && platform.StorageMode != "system_disk" {
		return hostPlatform{}, fmt.Errorf("host preflight did not verify the recordings storage mode")
	}
	if platform.StorageMode == "system_disk" && !allowSystemDisk {
		return hostPlatform{}, fmt.Errorf("system-disk recordings require explicit demo opt-in")
	}
	if platform.OSID != "ubuntu" && platform.OSID != "debian" {
		return hostPlatform{}, fmt.Errorf("unsupported Linux distribution")
	}
	if platform.GOARCH != "amd64" && platform.GOARCH != "arm64" {
		return hostPlatform{}, fmt.Errorf("unsupported host architecture")
	}
	if platform.GOARCH != runtime.GOARCH {
		return hostPlatform{}, fmt.Errorf("unsupported host architecture: OpenVMS agent build is %s, host is %s", runtime.GOARCH, platform.GOARCH)
	}
	return platform, nil
}

func systemDiskEnv(allow bool) string {
	if allow {
		return "OPENVMS_ALLOW_SYSTEM_DISK=1 "
	}
	return ""
}

func installerCommand(allow bool, subcommand string) string {
	return systemDiskEnv(allow) + "bash " + remoteRoot + "/install.sh " + subcommand
}

func uploadBundle(ctx context.Context, conn Conn) error {
	return fs.WalkDir(agentbundle.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(agentbundle.FS, path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(path, ".sh") {
			mode = 0o755
		}
		return conn.WriteFile(ctx, remoteRoot+"/"+path, mode, data)
	})
}

func probe(ctx context.Context, conn Conn) (Facts, error) {
	out, err := runStep(ctx, conn, 30*time.Second, "bash "+remoteRoot+"/install.sh detect")
	if err != nil {
		return Facts{}, err
	}
	return ParseFacts(out)
}

func runStep(ctx context.Context, conn Conn, limit time.Duration, cmd string) (string, error) {
	stepCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	return conn.Run(stepCtx, cmd)
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
