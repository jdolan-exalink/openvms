package provision

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

var (
	binaryOnce sync.Once
	binaryData []byte
	binaryErr  error
)

// LoadBinary returns the edge-agent executable to copy onto the new host.
func LoadBinary() ([]byte, error) {
	binaryOnce.Do(func() {
		binaryData, binaryErr = loadBinary()
	})
	return binaryData, binaryErr
}

func loadBinary() ([]byte, error) {
	for _, path := range []string{"/opt/openvms/edge-agent", "bin/edge-agent"} {
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 4 && string(b[:4]) == "\x7fELF" {
			return b, nil
		}
	}
	if path := os.Getenv("OPENVMS_AGENT_BINARY"); path != "" {
		b, err := os.ReadFile(path) //nolint:gosec // operator-selected agent binary
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	return buildBinary()
}

func buildBinary() ([]byte, error) {
	root, err := moduleRoot()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "openvms-agent-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "openvms-agent")
	cmd := exec.Command("go", "build", "-trimpath", "-o", out, "./apps/edge-agent")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("build edge agent: %w: %s", err, tail(string(output), 400))
	}
	return os.ReadFile(out)
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}
