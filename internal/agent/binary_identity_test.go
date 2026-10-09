package agent

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"os"
	"runtime"
	"strings"
	"testing"

	platformbuildinfo "github.com/jdolan-exalink/openvms/internal/platform/buildinfo"
)

func TestRunningBinaryIdentityUsesBoundedCurrentExecutable(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(data)
	got, err := RunningBinaryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA256 != hex.EncodeToString(want[:]) {
		t.Fatalf("SHA256 = %q, want digest of current executable", got.SHA256)
	}
	if got.Architecture != runtime.GOARCH {
		t.Fatalf("architecture = %q, want %q", got.Architecture, runtime.GOARCH)
	}
}

func TestIdentityFromBinaryUsesEmbeddedBuildMetadataAndDigest(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := IdentityFromBinary(data)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(data)
	if got.SHA256 != hex.EncodeToString(want[:]) || got.Architecture != runtime.GOARCH {
		t.Fatalf("identity = %#v, digest/architecture must describe exact bytes", got)
	}
	info, err := buildinfo.Read(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	wantVersion, wantCommit := info.Main.Version, ""
	if wantVersion == "(devel)" {
		wantVersion = ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			wantCommit = setting.Value
		}
	}
	if got.Version != wantVersion || got.Commit != wantCommit {
		t.Fatalf("release metadata = (%q, %q), want exact executable metadata (%q, %q)", got.Version, got.Commit, wantVersion, wantCommit)
	}
}

func TestIdentityFromBinaryRejectsOversize(t *testing.T) {
	if _, err := IdentityFromBinary(make([]byte, MaxBinarySize+1)); err == nil {
		t.Fatal("oversized binary accepted")
	}
}

func TestIdentityFromBinaryKeepsDigestWhenReleaseMetadataIsMissing(t *testing.T) {
	binary := []byte("legacy or unrecognized binary")
	got, err := IdentityFromBinary(binary)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(binary)
	if got.SHA256 != hex.EncodeToString(want[:]) || got.Version != "" || got.Commit != "" {
		t.Fatalf("unknown-build identity = %#v, want digest with unknown release labels", got)
	}
}

func TestRunningBinaryIdentityUsesAgentBuildReleaseLabel(t *testing.T) {
	original := platformbuildinfo.Version
	platformbuildinfo.Version = "0.1.1"
	t.Cleanup(func() { platformbuildinfo.Version = original })

	identity, err := RunningBinaryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if identity.Version != "0.1.1" {
		t.Fatalf("running binary version = %q, want agent build release 0.1.1", identity.Version)
	}
}

func TestValidReleaseVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		valid   bool
	}{
		{name: "semantic version", version: "0.1.1", valid: true},
		{name: "tag prefix", version: "v0.1.1", valid: true},
		{name: "prerelease", version: "0.2.0-rc.1", valid: true},
		{name: "development", version: "dev"},
		{name: "empty", version: ""},
		{name: "embedded newline", version: "0.1.1\nother"},
		{name: "path syntax", version: "../0.1.1"},
		{name: "too long", version: strings.Repeat("a", 65)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidReleaseVersion(tt.version); got != tt.valid {
				t.Fatalf("ValidReleaseVersion(%q) = %t, want %t", tt.version, got, tt.valid)
			}
		})
	}
}

func TestRunningBinaryIdentityRejectsSymlink(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := t.TempDir() + "/edge-agent"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := identityFromExecutablePath(link); err == nil {
		t.Fatal("symlink executable accepted")
	}
}

func TestRunningBinaryIdentityRejectsMissingExecutablePath(t *testing.T) {
	if _, err := identityFromExecutablePath(t.TempDir() + "/missing"); err == nil {
		t.Fatal("missing executable path accepted")
	}
}
