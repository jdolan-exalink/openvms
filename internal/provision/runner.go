package provision

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/deploy/agent"
	"github.com/jdolan-exalink/openvms/internal/agent"
)

const remoteRoot = "/opt/openvms/agent"

// Exec runs the install over an already open connection.
// password is used only to strip it from text that is stored on the job.
func Exec(ctx context.Context, j *job, password string, conn Conn, binary []byte, register func(context.Context, string, Variant) (uuid.UUID, error)) {
	defer func() { password = "" }()
	fail := func(step string, err error) {
		j.fail(step, err.Error(), password)
	}
	j.setStep(stepConnecting, stateDone, "")

	if err := uploadBundle(ctx, conn); err != nil {
		fail(stepConnecting, err)
		return
	}
	if facts, err := probe(ctx, conn); err == nil && Choose(facts) == VariantCPU {
		j.setWarning("cpu")
	}

	j.setStep(stepPackages, stateRunning, "")
	if err := runStep(ctx, conn, 40*time.Minute, "bash "+remoteRoot+"/install.sh packages"); err != nil {
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
		j.setWarning("cpu")
	} else {
		j.setWarning("")
	}
	j.setStep(stepHardware, stateDone, string(variant))

	j.setStep(stepCompose, stateRunning, string(variant))
	compose := fmt.Sprintf("VARIANT=%s bash %s/install.sh compose", variant, remoteRoot)
	if err := runStep(ctx, conn, 40*time.Minute, compose); err != nil {
		fail(stepCompose, err)
		return
	}
	j.setStep(stepCompose, stateDone, string(variant))

	j.setStep(stepNTP, stateRunning, "")
	if err := runStep(ctx, conn, 3*time.Minute, "bash "+remoteRoot+"/install.sh ntp"); err != nil {
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
	if err := runStep(ctx, conn, 3*time.Minute, agentCmd); err != nil {
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
