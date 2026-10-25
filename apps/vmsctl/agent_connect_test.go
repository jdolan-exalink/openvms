package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/provision"
)

const connectTestToken = "ovmsa_0123456789ab_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func writePrivate(t *testing.T, name, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), mode); err != nil { //nolint:gosec // test files inside t.TempDir()
		t.Fatal(err)
	}
	return path
}

func TestConnectAgentFromFilesPassesEverythingToTheServiceAndReportsWhatItFound(t *testing.T) {
	server := uuid.New()
	tokenPath := writePrivate(t, "token", connectTestToken+"\n", 0o600)
	headersPath := writePrivate(t, "headers.json", `{"CF-Access-Client-Id":"id","CF-Access-Client-Secret":"s"}`, 0o600)
	caPath := writePrivate(t, "ca.pem", "-----BEGIN CERTIFICATE-----\n", 0o644)
	var got provision.ConnectAgentInput
	var out bytes.Buffer
	err := connectAgentFromFiles(context.Background(), server, "https://agent.example.com", tokenPath, headersPath, caPath, true,
		func(in provision.ConnectAgentInput) (provision.ConnectAgentResult, error) {
			got = in
			return provision.ConnectAgentResult{BaseURL: "https://agent.example.com:443", Version: "0.4.0", Scopes: provision.AgentScopes{Read: true, Events: true}}, nil
		}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://agent.example.com" || got.Token != connectTestToken || !got.Replace || got.Headers["CF-Access-Client-Secret"] != "s" || !strings.HasPrefix(got.CAPEM, "-----BEGIN") {
		t.Fatalf("input = %+v", got)
	}
	text := out.String()
	if !strings.Contains(text, "0.4.0") || !strings.Contains(text, "read") || !strings.Contains(text, "events") || !strings.Contains(text, "NOT control") {
		t.Fatalf("output = %q", text)
	}
	if strings.Contains(text, connectTestToken) || strings.Contains(text, "s3cr3t") {
		t.Fatalf("the output repeats a secret: %q", text)
	}
}

func TestConnectAgentFromFilesRefusesUnsafeOrBrokenFilesBeforeCallingAnything(t *testing.T) {
	server := uuid.New()
	called := false
	connect := func(provision.ConnectAgentInput) (provision.ConnectAgentResult, error) {
		called = true
		return provision.ConnectAgentResult{}, nil
	}
	goodToken := writePrivate(t, "token", connectTestToken, 0o600)
	for name, tc := range map[string]struct{ token, headers string }{
		"token readable by others": {writePrivate(t, "t", connectTestToken, 0o644), ""},
		"token missing":            {filepath.Join(t.TempDir(), "nope"), ""},
		"token too short":          {writePrivate(t, "t", "short", 0o600), ""},
		"headers readable":         {goodToken, writePrivate(t, "h", `{"A":"b"}`, 0o644)},
		"headers not json":         {goodToken, writePrivate(t, "h", `not json`, 0o600)},
		"headers not strings":      {goodToken, writePrivate(t, "h", `{"A":1}`, 0o600)},
	} {
		called = false
		err := connectAgentFromFiles(context.Background(), server, "https://agent.example.com", tc.token, tc.headers, "", false, connect, &bytes.Buffer{})
		if err == nil || called {
			t.Errorf("%s: err=%v called=%v; want a refusal before the service is called", name, err, called)
		}
	}
}

func TestConnectAgentFromFilesTurnsServiceFailuresIntoPlainMessages(t *testing.T) {
	tokenPath := writePrivate(t, "token", connectTestToken, 0o600)
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"unreachable":  {provision.ErrAgentConnectUnreachable, "could not reach"},
		"bad token":    {provision.ErrAgentConnectUnauthorized, "refused the token"},
		"missing read": {provision.ErrAgentConnectForbidden, "read scope"},
		"not an agent": {provision.ErrAgentConnectNotAnAgent, "not an OpenVMS agent"},
		"exists":       {provision.ErrAgentAlreadyProvisioned, "-replace"},
	} {
		err := connectAgentFromFiles(context.Background(), uuid.New(), "https://a.example.com", tokenPath, "", "", false,
			func(provision.ConnectAgentInput) (provision.ConnectAgentResult, error) {
				return provision.ConnectAgentResult{}, tc.err
			}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, tc.want)
		}
	}
	other := errors.New("db down: password=hunter2")
	err := connectAgentFromFiles(context.Background(), uuid.New(), "https://a.example.com", tokenPath, "", "", false,
		func(provision.ConnectAgentInput) (provision.ConnectAgentResult, error) {
			return provision.ConnectAgentResult{}, other
		}, &bytes.Buffer{})
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("an unexpected error must not leak its detail: %v", err)
	}
}
