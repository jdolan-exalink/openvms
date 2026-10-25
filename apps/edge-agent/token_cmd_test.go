package main

import (
	"bytes"
	"os"
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

func keyFileOf(tokensFile string) string {
	return filepath.Join(filepath.Dir(tokensFile), "openvms.apikey")
}

func TestConnectionKeepsTheKeyInAPrivateFileNextToTheTokens(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.tokens.json")
	listLANAddresses = func() []string { return []string{"10.8.0.5", "192.168.1.20"} }
	t.Cleanup(func() { listLANAddresses = lanAddresses })

	code, out, errOut := runCmd(t, file, "connection", "--port", "7419")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	saved, err := os.ReadFile(keyFileOf(file))
	if err != nil {
		t.Fatalf("the key was not saved: %v", err)
	}
	key := strings.TrimSpace(string(saved))
	if !strings.HasPrefix(key, "ovmsa_") || !strings.Contains(out, key) {
		t.Fatalf("saved %q, printed %q: they must be the same key", key, out)
	}
	if info, _ := os.Stat(keyFileOf(file)); info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v, want 0600", info.Mode().Perm())
	}
	if _, ok := tokens.Open(file).Authenticate(key, tokens.ScopeRead, cmdNow()); !ok {
		t.Fatal("the saved key does not authenticate")
	}
	for _, want := range []string{"http://10.8.0.5:7419", "http://192.168.1.20:7419", keyFileOf(file), "Conectar agente", "openvms-agent token rotate openvms"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestConnectionStillPrintsTheKeyWhenItCannotBeSaved(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "agent.tokens.json")
	if err := os.Mkdir(keyFileOf(file), 0o700); err != nil { // a directory where the file should go
		t.Fatal(err)
	}
	code, out, errOut := runCmd(t, file, "connection")
	if code != 0 || !strings.Contains(out, "ovmsa_") {
		t.Fatalf("exit %d, out %q: the token exists, so the key must still reach the operator", code, out)
	}
	if !strings.Contains(errOut, "could not save") {
		t.Fatalf("stderr = %q, want a warning that the key was not saved", errOut)
	}
}

func TestRotatingTheOpenVMSTokenRefreshesItsSavedKey(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.tokens.json")
	runCmd(t, file, "connection")
	before, _ := os.ReadFile(keyFileOf(file))
	code, out, errOut := runCmd(t, file, "token", "rotate", "openvms")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	after, _ := os.ReadFile(keyFileOf(file))
	if string(after) == string(before) || strings.TrimSpace(string(after)) != strings.TrimSpace(out) {
		t.Fatalf("saved key not refreshed: before %q after %q printed %q", before, after, out)
	}
}

func TestRotatingAnotherTokenNeverCreatesTheKeyFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.tokens.json")
	runCmd(t, file, "token", "create", "--name", "central", "--scopes", "read")
	runCmd(t, file, "token", "rotate", "central")
	if _, err := os.Stat(keyFileOf(file)); err == nil {
		t.Fatal("a key file appeared for a token that is not the OpenVMS one")
	}
}

func TestRevokingTheOpenVMSTokenRemovesItsSavedKey(t *testing.T) {
	for _, ref := range []string{"openvms", ""} { // by name, then by ID
		file := filepath.Join(t.TempDir(), "agent.tokens.json")
		runCmd(t, file, "connection")
		if ref == "" {
			list, _ := tokens.Open(file).List()
			ref = list[0].ID
		}
		if code, _, errOut := runCmd(t, file, "token", "revoke", ref); code != 0 {
			t.Fatalf("revoke %s: %s", ref, errOut)
		}
		if _, err := os.Stat(keyFileOf(file)); err == nil {
			t.Fatalf("revoking %q left a saved key that no longer works", ref)
		}
	}
}

func TestRevokingAnotherTokenKeepsTheSavedKey(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.tokens.json")
	runCmd(t, file, "connection")
	runCmd(t, file, "token", "create", "--name", "central", "--scopes", "read")
	if code, _, errOut := runCmd(t, file, "token", "revoke", "central"); code != 0 {
		t.Fatalf("revoke: %s", errOut)
	}
	if _, err := os.Stat(keyFileOf(file)); err != nil {
		t.Fatalf("the saved key went away with an unrelated token: %v", err)
	}
}

func TestCreatingTheOpenVMSTokenByHandSavesItsKey(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.tokens.json")
	runCmd(t, file, "connection")
	runCmd(t, file, "token", "revoke", "openvms")
	code, out, errOut := runCmd(t, file, "token", "create", "--name", "openvms", "--scopes", "all")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	saved, err := os.ReadFile(keyFileOf(file))
	if err != nil || strings.TrimSpace(string(saved)) != strings.TrimSpace(out) {
		t.Fatalf("saved %q (%v), printed %q: the file must hold the key that works", saved, err, out)
	}
}
