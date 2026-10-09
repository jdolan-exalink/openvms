package provision

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadTrustedAgentArtifactReturnsIdentityForValidatedBytes(t *testing.T) {
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "edge-agent")
	if err := os.WriteFile(path, binary, 0o700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(binary)
	if err := os.WriteFile(path+".sha256", []byte(hex.EncodeToString(hash[:])+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".goarch", []byte(runtime.GOARCH+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".version", []byte("0.1.1"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, err := LoadTrustedAgentArtifact(path)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Identity.SHA256 != hex.EncodeToString(hash[:]) || artifact.Identity.Architecture != runtime.GOARCH || artifact.Identity.Version != "0.1.1" {
		t.Fatalf("identity = %#v, does not identify validated binary", artifact.Identity)
	}
	if len(artifact.Binary) != len(binary) {
		t.Fatalf("returned bytes length = %d, want %d", len(artifact.Binary), len(binary))
	}
	if !bytes.Equal(artifact.Binary, binary) {
		t.Fatal("identity resolver returned bytes different from the manifest-verified artifact")
	}
}

func TestLoadTrustedAgentArtifactRejectsMalformedVersionManifest(t *testing.T) {
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "edge-agent")
	if err := os.WriteFile(path, binary, 0o700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(binary)
	for suffix, contents := range map[string][]byte{
		".sha256":  []byte(hex.EncodeToString(hash[:]) + "\n"),
		".goarch":  []byte(runtime.GOARCH + "\n"),
		".version": []byte("0.1.1\nother"),
	} {
		if err := os.WriteFile(path+suffix, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadTrustedAgentArtifact(path); err == nil {
		t.Fatal("malformed version metadata accepted")
	}
}

func TestLoadTrustedAgentArtifactRejectsManifestMismatch(t *testing.T) {
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "edge-agent")
	if err := os.WriteFile(path, binary, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".sha256", []byte(hex.EncodeToString(make([]byte, sha256.Size))), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".goarch", []byte(runtime.GOARCH), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTrustedAgentArtifact(path); err == nil {
		t.Fatal("manifest mismatch accepted")
	}
}
