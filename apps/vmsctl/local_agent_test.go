package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/provision"
)

func TestLocalAgentSessionQueryUsesConfiguredIdleLimit(t *testing.T) {
	token := strings.Repeat("s", 48)
	for _, idle := range []time.Duration{5 * time.Minute, 4 * time.Hour} {
		params := localAgentSessionQuery(token, idle)
		if string(params.TokenHash) != string(identity.HashToken(token)) || params.IdleSeconds != idle.Seconds() {
			t.Errorf("session query for idle=%s = %#v", idle, params)
		}
	}
}

func TestLocalAgentFilesAreNotReadBeforeAuthorization(t *testing.T) {
	denied := errors.New("forbidden")
	registerCalls := 0
	var output strings.Builder
	err := registerLocalAgentFromFiles(context.Background(), provision.LocalAgentRegistration{
		ServerID: uuid.New(), Host: "192.0.2.25", HTTPPort: 7419, SecurePort: 7443,
	}, filepath.Join(t.TempDir(), "missing-token"), filepath.Join(t.TempDir(), "missing-ca"),
		func() error { return denied }, func(provision.LocalAgentRegistration, string) error { registerCalls++; return nil }, &output)
	if !errors.Is(err, denied) {
		t.Fatalf("registration error = %v, want authorization denial", err)
	}
	if registerCalls != 0 || output.Len() != 0 {
		t.Fatalf("denied registration continued: registerCalls=%d output=%q", registerCalls, output.String())
	}
}

func TestLocalAgentFilesKeepCredentialsOutOfOutput(t *testing.T) {
	root := t.TempDir()
	tokenPath := filepath.Join(root, "agent.token")
	caPath := filepath.Join(root, "tls.crt")
	token := strings.Repeat("a", 64)
	ca := []byte("PUBLIC CA CERTIFICATE PEM")
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caPath, ca, 0o644); err != nil {
		t.Fatal(err)
	}
	input := provision.LocalAgentRegistration{ServerID: uuid.New(), Host: "192.0.2.25", HTTPPort: 7419, SecurePort: 7443}
	var gotToken string
	var gotCA []byte
	var output strings.Builder
	err := registerLocalAgentFromFiles(context.Background(), input, tokenPath, caPath,
		func() error { return nil }, func(in provision.LocalAgentRegistration, secret string) error {
			gotToken, gotCA, input = secret, in.CAPEM, in
			return nil
		}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if gotToken != token || string(gotCA) != string(ca) {
		t.Fatal("registration callback did not receive the selected files")
	}
	if strings.Contains(output.String(), token) || strings.Contains(output.String(), string(ca)) {
		t.Fatal("registration summary exposed token or CA contents")
	}
	if !strings.Contains(output.String(), input.ServerID.String()) || !strings.Contains(output.String(), input.Host) {
		t.Fatalf("safe summary omitted registration identity: %q", output.String())
	}
}

func TestLocalAgentTokenFileRequiresPrivateRegularFile(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "valid.token")
	if err := os.WriteFile(valid, []byte(strings.Repeat("b", 64)), 0o600); err != nil {
		t.Fatal(err)
	}
	weak := filepath.Join(root, "weak.token")
	if err := os.WriteFile(weak, []byte(strings.Repeat("c", 64)), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.token")
	if err := os.Symlink(valid, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, path := range []string{weak, link} {
		if _, err := readLocalAgentTokenFile(path); err == nil {
			t.Errorf("unsafe token path %q accepted", filepath.Base(path))
		}
	}
	got, err := readLocalAgentTokenFile(valid)
	if err != nil || got != strings.Repeat("b", 64) {
		t.Fatalf("private regular token read = %q, %v", got, err)
	}
}
