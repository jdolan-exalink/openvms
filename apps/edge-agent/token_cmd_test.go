package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent/tokens"
)

var cmdNow = func() time.Time { return time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC) }

func runCmd(t *testing.T, file string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	head := 1 // flags follow the command: `token create --file F ...`, `connection --file F ...`
	if args[0] == "token" && len(args) > 1 {
		head = 2
	}
	full := append(append(append([]string{}, args[:head]...), "--file", file), args[head:]...)
	code = runConsoleCommand(full, &out, &errb, cmdNow)
	return code, out.String(), errb.String()
}

func TestTokenCreatePrintsTheSecretAloneOnStdoutAndWorks(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.tokens.json")
	code, out, errOut := runCmd(t, file, "token", "create", "--name", "central", "--scopes", "read,events", "--ttl", "720h")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	secret := strings.TrimSpace(out)
	if strings.Contains(secret, "\n") || !strings.HasPrefix(secret, "ovmsa_") {
		t.Fatalf("stdout must be the token and nothing else (it is piped into scripts), got %q", out)
	}
	if !strings.Contains(errOut, "central") || !strings.Contains(errOut, "will not be shown again") {
		t.Fatalf("stderr should explain, got %q", errOut)
	}
	if _, ok := tokens.Open(file).Authenticate(secret, tokens.ScopeEvents, cmdNow()); !ok {
		t.Fatal("the printed token does not authenticate")
	}
	if _, ok := tokens.Open(file).Authenticate(secret, tokens.ScopeUpdate, cmdNow()); ok {
		t.Fatal("the token got a scope it was not given")
	}
}

func TestTokenListShowsWhatIsNeededAndNeverTheSecret(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.tokens.json")
	_, secretOut, _ := runCmd(t, file, "token", "create", "--name", "central", "--scopes", "all")
	code, out, _ := runCmd(t, file, "token", "list")
	if code != 0 || !strings.Contains(out, "central") || !strings.Contains(out, "read,events,control,update") || !strings.Contains(out, "never") {
		t.Fatalf("list = %q (exit %d)", out, code)
	}
	if strings.Contains(out, strings.TrimSpace(secretOut)) || strings.Contains(out, "hash") {
		t.Fatalf("list leaks credentials: %q", out)
	}
	emptyCode, emptyOut, _ := runCmd(t, filepath.Join(t.TempDir(), "none.json"), "token", "list")
	if emptyCode != 0 || !strings.Contains(emptyOut, "No tokens") {
		t.Fatalf("empty list = %q (exit %d)", emptyOut, emptyCode)
	}
}

func TestTokenRevokeAndRotate(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.tokens.json")
	_, first, _ := runCmd(t, file, "token", "create", "--name", "central", "--scopes", "read")
	code, rotated, errOut := runCmd(t, file, "token", "rotate", "central")
	if code != 0 || strings.TrimSpace(rotated) == strings.TrimSpace(first) {
		t.Fatalf("rotate: exit %d out %q err %q", code, rotated, errOut)
	}
	store := tokens.Open(file)
	if _, ok := store.Authenticate(strings.TrimSpace(first), tokens.ScopeRead, cmdNow()); ok {
		t.Fatal("the old secret still works after rotate")
	}
	if _, ok := store.Authenticate(strings.TrimSpace(rotated), tokens.ScopeRead, cmdNow()); !ok {
		t.Fatal("the new secret does not work")
	}
	if code, _, errOut := runCmd(t, file, "token", "revoke", "central"); code != 0 {
		t.Fatalf("revoke: %s", errOut)
	}
	if _, ok := store.Authenticate(strings.TrimSpace(rotated), tokens.ScopeRead, cmdNow()); ok {
		t.Fatal("a revoked token works")
	}
	if code, _, errOut := runCmd(t, file, "token", "revoke", "central"); code == 0 || errOut == "" {
		t.Fatal("revoking a token that is gone must fail")
	}
}

func TestTokenCommandRejectsBadInput(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.tokens.json")
	for name, args := range map[string][]string{
		"no name":     {"token", "create", "--scopes", "read"},
		"bad scope":   {"token", "create", "--name", "x", "--scopes", "root"},
		"no scopes":   {"token", "create", "--name", "x"},
		"bad ttl":     {"token", "create", "--name", "x", "--scopes", "read", "--ttl", "soon"},
		"unknown sub": {"token", "explode"},
		"no sub":      {"token"},
	} {
		code, out, errOut := runCmd(t, file, args...)
		if code == 0 || out != "" || errOut == "" {
			t.Errorf("%s: exit %d stdout %q stderr %q; want a failure that prints nothing on stdout", name, code, out, errOut)
		}
	}
}

func TestConnectionCreatesTheOpenVMSTokenOnceAndPrintsWhatToEnter(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.tokens.json")
	code, out, errOut := runCmd(t, file, "connection", "--port", "7419")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "ovmsa_") || !strings.Contains(out, ":7419") || !strings.Contains(out, "Token") || !strings.Contains(out, "Address") {
		t.Fatalf("connection output = %q", out)
	}
	list, _ := tokens.Open(file).List()
	if len(list) != 1 || list[0].Name != "openvms" || len(list[0].Scopes) != len(tokens.AllScopes) {
		t.Fatalf("tokens = %+v", list)
	}
	// A second run must not silently mint another credential or hide that one exists.
	code, out, errOut = runCmd(t, file, "connection")
	if code == 0 || strings.Contains(out, "ovmsa_") || !strings.Contains(errOut, "token rotate openvms") {
		t.Fatalf("second run: exit %d out %q err %q", code, out, errOut)
	}
}

func TestTokenFlagsMayComeBeforeOrAfterTheName(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.tokens.json")
	runCmd(t, file, "token", "create", "--name", "a", "--scopes", "read")
	runCmd(t, file, "token", "create", "--name", "b", "--scopes", "read")
	var out, errb bytes.Buffer
	if code := runConsoleCommand([]string{"token", "rotate", "a", "--file", file}, &out, &errb, cmdNow); code != 0 {
		t.Fatalf("flags after the name: exit %d %s", code, errb.String())
	}
	out.Reset()
	errb.Reset()
	if code := runConsoleCommand([]string{"token", "revoke", "--file", file, "b"}, &out, &errb, cmdNow); code != 0 {
		t.Fatalf("flags before the name: exit %d %s", code, errb.String())
	}
	if list, _ := tokens.Open(file).List(); len(list) != 1 || list[0].Name != "a" {
		t.Fatalf("tokens = %+v", list)
	}
}
