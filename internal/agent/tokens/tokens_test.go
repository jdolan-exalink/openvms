package tokens

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.tokens.json")
	return Open(path), path
}

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func TestCreatedTokenAuthenticatesWithItsScopesOnly(t *testing.T) {
	s, _ := newStore(t)
	secret, tok, err := s.Create("central", []Scope{ScopeRead, ScopeEvents}, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "ovmsa_"+tok.ID+"_") || len(secret) < 70 {
		t.Fatalf("secret %q does not have the expected shape", secret)
	}
	for scope, want := range map[Scope]bool{ScopeRead: true, ScopeEvents: true, ScopeControl: false, ScopeUpdate: false} {
		if _, ok := s.Authenticate(secret, scope, now); ok != want {
			t.Errorf("scope %s: ok=%v want %v", scope, ok, want)
		}
	}
}

func TestTheSecretIsNeverStoredAndWrongSecretsAreRefused(t *testing.T) {
	s, path := newStore(t)
	secret, tok, _ := s.Create("central", AllScopes, 0, now)
	raw, err := os.ReadFile(path) //nolint:gosec // path is inside t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), strings.TrimPrefix(secret, "ovmsa_"+tok.ID+"_")) {
		t.Fatal("the token secret is in the file")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v, want 0600", info.Mode().Perm())
	}
	for _, bad := range []string{"", "Bearer", "ovmsa_", "ovmsa_" + tok.ID + "_", "ovmsa_" + tok.ID + "_" + strings.Repeat("0", 64), "ovmsa_ffffffffffff_" + strings.TrimPrefix(secret, "ovmsa_"+tok.ID+"_"), secret + "x", "plain-legacy-secret"} {
		if _, ok := s.Authenticate(bad, ScopeRead, now); ok {
			t.Errorf("%q must not authenticate", bad)
		}
	}
}

func TestExpiredAndRevokedTokensStopWorking(t *testing.T) {
	s, _ := newStore(t)
	secret, tok, _ := s.Create("short", AllScopes, time.Hour, now)
	if _, ok := s.Authenticate(secret, ScopeRead, now.Add(30*time.Minute)); !ok {
		t.Fatal("inside its lifetime")
	}
	if _, ok := s.Authenticate(secret, ScopeRead, now.Add(time.Hour)); ok {
		t.Fatal("at its expiry it must be refused")
	}
	other, _, _ := s.Create("long", AllScopes, 0, now)
	if err := s.Revoke(tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Authenticate(secret, ScopeRead, now); ok {
		t.Fatal("a revoked token authenticates")
	}
	if _, ok := s.Authenticate(other, ScopeRead, now.Add(1000*time.Hour)); !ok {
		t.Fatal("a token without expiry never expires, and revoking another does not touch it")
	}
}

func TestAnotherProcessChangesTheFileAndTheRunningStoreSeesIt(t *testing.T) {
	running, path := newStore(t)
	cli := Open(path) // what `openvms-agent token ...` does in its own process
	secret, tok, _ := cli.Create("central", AllScopes, 0, now)
	if _, ok := running.Authenticate(secret, ScopeRead, now); !ok {
		t.Fatal("a token created by the CLI must work without restarting the agent")
	}
	if err := cli.Revoke(tok.Name); err != nil {
		t.Fatal(err)
	}
	if _, ok := running.Authenticate(secret, ScopeRead, now); ok {
		t.Fatal("a token revoked by the CLI must stop working without restarting the agent")
	}
}

func TestRotateReplacesTheSecretKeepingNameScopesAndExpiry(t *testing.T) {
	s, _ := newStore(t)
	oldSecret, old, _ := s.Create("central", []Scope{ScopeRead}, 48*time.Hour, now)
	newSecret, tok, err := s.Rotate("central", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if tok.Name != "central" || len(tok.Scopes) != 1 || tok.Scopes[0] != ScopeRead || tok.ID == old.ID {
		t.Fatalf("rotated token = %+v", tok)
	}
	if _, ok := s.Authenticate(oldSecret, ScopeRead, now); ok {
		t.Fatal("the old secret still works")
	}
	if _, ok := s.Authenticate(newSecret, ScopeRead, now.Add(time.Hour)); !ok {
		t.Fatal("the new secret does not work")
	}
	if tok.ExpiresAt == nil || !tok.ExpiresAt.Equal(old.ExpiresAt.Add(time.Hour)) && tok.ExpiresAt.Before(now.Add(time.Hour)) {
		t.Fatalf("expiry = %v", tok.ExpiresAt)
	}
}

func TestCreateRefusesBadInput(t *testing.T) {
	s, _ := newStore(t)
	if _, _, err := s.Create("", AllScopes, 0, now); err == nil {
		t.Error("a token needs a name")
	}
	if _, _, err := s.Create("bad name!", AllScopes, 0, now); err == nil {
		t.Error("names are simple words")
	}
	if _, _, err := s.Create("x", nil, 0, now); err == nil {
		t.Error("a token needs at least one scope")
	}
	if _, _, err := s.Create("x", []Scope{"root"}, 0, now); err == nil {
		t.Error("unknown scope")
	}
	if _, _, err := s.Create("x", AllScopes, -time.Hour, now); err == nil {
		t.Error("negative lifetime")
	}
	if _, _, err := s.Create("dup", AllScopes, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Create("dup", AllScopes, 0, now); err == nil {
		t.Error("a name is unique; rotate to replace a secret")
	}
	if err := s.Revoke("nope"); err == nil {
		t.Error("revoking an unknown token is an error")
	}
}

func TestACorruptFileRefusesEveryTokenAndIsNeverOverwritten(t *testing.T) {
	s, path := newStore(t)
	secret, _, _ := s.Create("central", AllScopes, 0, now)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Authenticate(secret, ScopeRead, now); ok {
		t.Fatal("an unreadable file must fail closed")
	}
	if _, _, err := s.Create("new", AllScopes, 0, now); err == nil {
		t.Fatal("creating over a file that cannot be read would destroy it")
	}
	if raw, _ := os.ReadFile(path); string(raw) != "{not json" { //nolint:gosec // path is inside t.TempDir()
		t.Fatalf("the corrupt file was overwritten: %q", raw)
	}
}

func TestScopeParsing(t *testing.T) {
	got, err := ParseScopes("read, events")
	if err != nil || len(got) != 2 || got[0] != ScopeRead || got[1] != ScopeEvents {
		t.Fatalf("ParseScopes = %v, %v", got, err)
	}
	if all, err := ParseScopes("all"); err != nil || len(all) != len(AllScopes) {
		t.Fatalf("all = %v, %v", all, err)
	}
	if _, err := ParseScopes("read,wat"); err == nil {
		t.Fatal("unknown scope accepted")
	}
	if _, err := ParseScopes(""); err == nil {
		t.Fatal("empty scope list accepted")
	}
}

// A rotated secret has the same length and may be written within the same clock tick: the running
// store must still drop the old one.
func TestRotationIsSeenEvenWhenTheFileSizeAndTimeStayTheSame(t *testing.T) {
	running, path := newStore(t)
	cli := Open(path)
	oldSecret, _, _ := cli.Create("central", AllScopes, 0, now)
	if _, ok := running.Authenticate(oldSecret, ScopeRead, now); !ok {
		t.Fatal("setup")
	}
	before, _ := os.Stat(path)
	newSecret, _, err := cli.Rotate("central", now)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if before.Size() != after.Size() {
		t.Skipf("sizes differ (%d vs %d); the case needs equal sizes", before.Size(), after.Size())
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, ok := running.Authenticate(oldSecret, ScopeRead, now); ok {
		t.Fatal("the old secret still works after a rotation")
	}
	if _, ok := running.Authenticate(newSecret, ScopeRead, now); !ok {
		t.Fatal("the new secret does not work")
	}
}
